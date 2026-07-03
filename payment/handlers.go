package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"misarch/payment/service"
)

// validationSucceeded is the inbound `data` shape for the
// order-validation-succeeded route: { order: <OrderDTO> }. The order is kept as
// raw JSON so it is round-tripped verbatim into openorders and later saga
// events.
type validationSucceeded struct {
	Order json.RawMessage `json:"order"`
}

// userCreated is the inbound `data` shape for the user-created route. Only id is
// used.
type userCreated struct {
	ID string `json:"id"`
}

// handleOrderValidationSucceeded reacts to discount/order/validation-succeeded.
// It reads the order and kicks off the payment process in the background,
// returning nil so the Dapr route always responds 200 (the original invoked the
// process fire-and-forget and never surfaced errors to Dapr → no redelivery).
func handleOrderValidationSucceeded(_ context.Context, svc *service.Service, data json.RawMessage) error {
	var event validationSucceeded
	if err := json.Unmarshal(data, &event); err != nil {
		slog.Error("order-validation-succeeded: invalid event data", "error", err)
		return nil // always 200; a malformed event can never succeed on retry.
	}
	if len(event.Order) == 0 {
		slog.Error("order-validation-succeeded: missing order in event data")
		return nil
	}
	slog.Info("Received discount order validation success event")
	// Detach from the request context (which is cancelled once this handler
	// returns) so the background saga work runs to completion.
	go svc.StartPaymentProcess(context.Background(), event.Order)
	return nil
}

// handleUserCreated reacts to user/user/created by materializing the two
// default payment informations (PREPAYMENT + INVOICE) for the new user. Always
// returns nil (HTTP 200); errors are logged inside the service.
func handleUserCreated(_ context.Context, svc *service.Service, data json.RawMessage) error {
	var user userCreated
	if err := json.Unmarshal(data, &user); err != nil {
		slog.Error("user-created: invalid event data", "error", err)
		return nil
	}
	slog.Info("Received user creation event", "id", user.ID)
	go svc.AddDefaultPaymentInformations(context.Background(), user.ID)
	return nil
}

// updateStatusRequest is the REST callback body from the simulation provider.
type updateStatusRequest struct {
	PaymentID string `json:"paymentId"`
	Status    string `json:"status"`
}

// validPaymentStatuses is the accepted set for the callback's status field
// (matching the PaymentStatus enum; the original validated with @IsEnum).
var validPaymentStatuses = map[string]bool{
	"OPEN": true, "PENDING": true, "SUCCEEDED": true, "FAILED": true, "INKASSO": true,
}

// updatePaymentStatusHandler handles POST /payment/update-payment-status from
// the external simulation provider. Body: { paymentId, status }. It dispatches
// to the payment-method update flow. Returns 400 on a malformed body, 404 when
// the payment (or its payment information) is not found, and 200 on success —
// reproducing the original controller (which rethrew not-found as HTTP 404).
func updatePaymentStatusHandler(_ context.Context, svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req updateStatusRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.PaymentID == "" || !validPaymentStatuses[req.Status] {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Run synchronously so the outcome (success vs not-found) is reflected
		// in the HTTP status, as the original controller did.
		if err := svc.UpdatePaymentStatus(r.Context(), req.PaymentID, req.Status); err != nil {
			slog.Error("update-payment-status failed", "paymentId", req.PaymentID, "error", err)
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}
}
