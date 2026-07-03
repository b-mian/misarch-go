package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"misarch/payment/events"
	"misarch/payment/store"
)

// PaymentStatus / PaymentMethod string constants, matching the values stored in
// Mongo and used on the wire.
const (
	statusOpen      = "OPEN"
	statusPending   = "PENDING"
	statusSucceeded = "SUCCEEDED"
	statusFailed    = "FAILED"

	methodCreditCard = "CREDIT_CARD"
	methodPrepayment = "PREPAYMENT"
	methodInvoice    = "INVOICE"
)

// Service is the payment saga orchestrator. It owns the persistence store, the
// event publisher, and the simulation connector, and drives the payment-method
// state machines on inbound events and the simulation callback.
type Service struct {
	store *store.Store
	pub   events.Publisher
	sim   *Simulation
}

// New builds a Service.
func New(st *store.Store, pub events.Publisher, sim *Simulation) *Service {
	return &Service{store: st, pub: pub, sim: sim}
}

// orderHeader is the subset of the order JSON the saga reads. The full order is
// round-tripped as raw JSON elsewhere; only these fields drive control flow.
type orderHeader struct {
	ID                       string `json:"id"`
	PaymentInformationID     string `json:"paymentInformationId"`
	CompensatableOrderAmount int64  `json:"compensatableOrderAmount"`
}

// StartPaymentProcess reproduces EventService.startPaymentProcess:
//  1. paymentService.create(order): look up payment information (not-found →
//     compensation), create a Payment with _id == order.id.
//  2. openOrdersService.create(payment.id, order): persist order context.
//  3. dispatch to the payment-method processor create flow.
//
// On any error in the above, delete the open order and publish payment-failed
// (order compensation). Errors are logged, never returned to the caller — the
// Dapr handler always returns 200. The whole method is meant to run in the
// background (the original invoked it fire-and-forget).
func (s *Service) StartPaymentProcess(ctx context.Context, order json.RawMessage) {
	var hdr orderHeader
	if err := json.Unmarshal(order, &hdr); err != nil {
		slog.Error("{startPaymentProcess} invalid order JSON", "error", err)
		return
	}
	slog.Info("Starting payment process for order", "id", hdr.ID)

	if err := s.startPaymentProcess(ctx, hdr, order); err != nil {
		slog.Error("{startPaymentProcess} Fatal error", "error", err)
		// remove the open order (best-effort) and publish payment-failed.
		_ = s.store.DeleteOpenOrder(ctx, hdr.ID)
		if perr := events.PublishPaymentFailed(ctx, s.pub, order); perr != nil {
			slog.Error("{startPaymentProcess} publish payment-failed", "error", perr)
		}
	}
}

// startPaymentProcess performs the create + dispatch, returning an error so the
// caller can run the compensation branch — matching the original try/catch.
func (s *Service) startPaymentProcess(ctx context.Context, hdr orderHeader, order json.RawMessage) error {
	// paymentService.create(order): find the payment information first.
	info, err := s.store.GetPaymentInformation(ctx, hdr.PaymentInformationID)
	if err != nil {
		// fatal error that requires complete order compensation.
		return fmt.Errorf("payment information %s not found: %w", hdr.PaymentInformationID, err)
	}

	// create the payment (id == order id; ref == payment information id).
	if _, err := s.store.CreatePayment(ctx, hdr.ID, info.ID, hdr.CompensatableOrderAmount); err != nil {
		return fmt.Errorf("create payment: %w", err)
	}

	// temporarily store the order context for later events.
	if err := s.store.CreateOpenOrder(ctx, hdr.ID, order); err != nil {
		return fmt.Errorf("create open order: %w", err)
	}

	// dispatch to the payment-method processor create flow.
	return s.startProcessor(ctx, info.PaymentMethod, hdr.ID, hdr.CompensatableOrderAmount)
}

// startProcessor dispatches the create flow by payment method, reproducing
// PaymentProviderConnectionService.startPaymentProcess + each processor.create.
func (s *Service) startProcessor(ctx context.Context, method, id string, amount int64) error {
	switch method {
	case methodCreditCard:
		// emit enabled event since everything necessary is in place.
		s.publishEnabledForPayment(ctx, id)
		s.sim.Register(ctx, id, amount, "credit-card")
		_, err := s.store.UpdatePaymentStatus(ctx, id, statusPending)
		return err
	case methodPrepayment:
		// prepayment does NOT emit payment-enabled at create.
		s.sim.Register(ctx, id, amount, "prepayment")
		_, err := s.store.UpdatePaymentStatus(ctx, id, statusPending)
		return err
	case methodInvoice:
		s.publishEnabledForPayment(ctx, id)
		s.sim.Register(ctx, id, amount, "invoice")
		_, err := s.store.UpdatePaymentStatus(ctx, id, statusPending)
		return err
	default:
		// Unreachable given the current PaymentMethod enum (mirrors the
		// original NotImplementedException).
		return fmt.Errorf("Controller for Payment Method not implemented")
	}
}

// UpdatePaymentStatus reproduces PaymentProviderConnectionService.updatePaymentStatus:
// load the payment (with its payment information), then dispatch on the method.
// Returns an error (surfaced as HTTP 404 for not-found by the REST caller).
func (s *Service) UpdatePaymentStatus(ctx context.Context, paymentID, status string) error {
	payment, err := s.store.GetPayment(ctx, paymentID)
	if err != nil {
		return err
	}
	info, err := s.store.GetPaymentInformation(ctx, payment.PaymentInformation)
	if err != nil {
		// The original throws NotFoundException('Payment Information not found')
		// when the ref cannot be populated.
		return fmt.Errorf("Payment Information not found")
	}
	slog.Info("{updatePaymentStatus} Updating payment", "id", paymentID, "method", info.PaymentMethod, "status", status)

	switch info.PaymentMethod {
	case methodCreditCard:
		return s.updateCreditCard(ctx, paymentID, status)
	case methodPrepayment:
		return s.updatePrepayment(ctx, paymentID, status)
	case methodInvoice:
		return s.updateInvoice(ctx, paymentID, status)
	default:
		return fmt.Errorf("Controller for Payment Method not implemented")
	}
}

// updateCreditCard reproduces CreditCardService.update:
//   - status != FAILED → persist status.
//   - status == FAILED → read numberOfRetries (always 0); if >= 3 publish
//     payment-failed + persist FAILED; else retry (POST register {paymentId,
//     type}). Because retries never increments, FAILED is retried indefinitely
//     and never persisted. [PRESERVE]
func (s *Service) updateCreditCard(ctx context.Context, paymentID, status string) error {
	if status != statusFailed {
		_, err := s.store.UpdatePaymentStatus(ctx, paymentID, status)
		return err
	}
	payment, err := s.store.GetPayment(ctx, paymentID)
	if err != nil {
		return err
	}
	if payment.NumberOfRetries >= 3 {
		s.publishFailedForPayment(ctx, paymentID)
		_, err := s.store.UpdatePaymentStatus(ctx, paymentID, status)
		return err
	}
	// otherwise retry the payment (never persists FAILED, since retries == 0).
	s.sim.Retry(ctx, paymentID, "credit-card")
	return nil
}

// updatePrepayment reproduces PrepaymentService.update:
//   - status != SUCCEEDED → persist status.
//   - status == SUCCEEDED → publish payment-enabled and return WITHOUT
//     persisting SUCCEEDED (the payment stays at its prior status). [PRESERVE]
func (s *Service) updatePrepayment(ctx context.Context, paymentID, status string) error {
	if status != statusSucceeded {
		_, err := s.store.UpdatePaymentStatus(ctx, paymentID, status)
		return err
	}
	s.publishEnabledForPayment(ctx, paymentID)
	return nil
}

// updateInvoice reproduces InvoiceService.update: just persist the status
// (SUCCEEDED sets payedAt via the store). No events.
func (s *Service) updateInvoice(ctx context.Context, paymentID, status string) error {
	_, err := s.store.UpdatePaymentStatus(ctx, paymentID, status)
	return err
}

// AddDefaultPaymentInformations reproduces
// PaymentInformationService.addDefaultPaymentInformations: create a PREPAYMENT
// and an INVOICE payment information for the user (no public/secret details),
// each with a fresh string-UUID _id and user stored as { id }. Not idempotent,
// matching the original (redelivery duplicates). Errors are logged, not
// returned (the Dapr handler always returns 200).
func (s *Service) AddDefaultPaymentInformations(ctx context.Context, userID string) {
	slog.Info("{addDefaultPaymentInformations} for user", "id", userID)
	for _, method := range []string{methodPrepayment, methodInvoice} {
		doc := store.PaymentInformationDoc{
			ID:            uuid.NewString(),
			PaymentMethod: method,
			User:          store.UserRef{ID: userID},
		}
		if _, err := s.store.CreatePaymentInformation(ctx, doc); err != nil {
			slog.Error("{addDefaultPaymentInformations} create failed", "method", method, "user", userID, "error", err)
		}
	}
}

// publishEnabledForPayment reproduces EventService.buildPaymentEnabledEvent:
// look up the open order for the payment id and publish payment-enabled with
// its stored order. Missing open order is logged (the original threw, but the
// throw was swallowed by the un-awaited fire-and-forget caller).
func (s *Service) publishEnabledForPayment(ctx context.Context, paymentID string) {
	order, err := s.store.FindOpenOrder(ctx, paymentID)
	if err != nil {
		slog.Error("{buildPaymentEnabledEvent} open order not found", "paymentId", paymentID, "error", err)
		return
	}
	if err := events.PublishPaymentEnabled(ctx, s.pub, order); err != nil {
		slog.Error("{buildPaymentEnabledEvent} publish failed", "paymentId", paymentID, "error", err)
	}
}

// publishFailedForPayment reproduces EventService.buildPaymentFailedEvent: look
// up the open order for the payment id and publish payment-failed with its
// stored order.
func (s *Service) publishFailedForPayment(ctx context.Context, paymentID string) {
	order, err := s.store.FindOpenOrder(ctx, paymentID)
	if err != nil {
		slog.Error("{buildPaymentFailedEvent} open order not found", "paymentId", paymentID, "error", err)
		return
	}
	if err := events.PublishPaymentFailed(ctx, s.pub, order); err != nil {
		slog.Error("{buildPaymentFailedEvent} publish failed", "paymentId", paymentID, "error", err)
	}
}
