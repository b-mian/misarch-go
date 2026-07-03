package service

import (
	"context"
	"log/slog"
	"time"
)

// cronInterval is the 15-minute cadence of both overdue-payment schedulers
// (`*/15 * * * *` in the original). Using a ticker rather than a cron
// expression: the original fires every 15 minutes of wall-clock, the ticker
// fires every 15 minutes from start; both externally set overdue PENDING
// payments to FAILED at ~15-minute granularity.
const cronInterval = 15 * time.Minute

// invoiceOverdueDays / prepaymentOverdueDays are the age thresholds: invoices
// expire after 30 days PENDING, prepayments after 7.
const (
	invoiceOverdueDays    = 30
	prepaymentOverdueDays = 7
)

// StartCron launches the two overdue-payment schedulers as background
// goroutines, stopping when ctx is cancelled. Mirrors InvoiceService and
// PrepaymentService.checkOpenPayments (both @Cron('*/15 * * * *')).
func (s *Service) StartCron(ctx context.Context) {
	go s.runScheduler(ctx, methodInvoice, invoiceOverdueDays)
	go s.runScheduler(ctx, methodPrepayment, prepaymentOverdueDays)
}

// runScheduler ticks every cronInterval and fails overdue PENDING payments of
// the given method.
func (s *Service) runScheduler(ctx context.Context, method string, days int) {
	ticker := time.NewTicker(cronInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkOpenPayments(ctx, method, days)
		}
	}
}

// checkOpenPayments finds PENDING payments of the method whose createdAt is at
// least `days` old, sets each to FAILED, and publishes payment-failed —
// reproducing checkOpenPayments for both invoice and prepayment.
func (s *Service) checkOpenPayments(ctx context.Context, method string, days int) {
	slog.Info("{checkOpenPayments} Checking open payments", "method", method)
	// build the upper bound on createdAt: now minus `days` days.
	to := xDaysBackFromNow(days)

	openPayments, err := s.store.FindPaymentsByStatusMethod(ctx, statusPending, method, to)
	if err != nil {
		slog.Error("{checkOpenPayments} find failed", "method", method, "error", err)
		return
	}

	for _, payment := range openPayments {
		slog.Info("Setting payment to failed since it is overdue", "id", payment.ID)
		if _, err := s.store.UpdatePaymentStatus(ctx, payment.ID, statusFailed); err != nil {
			slog.Error("{checkOpenPayments} update failed", "id", payment.ID, "error", err)
		}
		// emit failed event.
		s.publishFailedForPayment(ctx, payment.ID)
	}
}

// xDaysBackFromNow returns now minus x days, matching the original util.
func xDaysBackFromNow(x int) time.Time {
	return time.Now().AddDate(0, 0, -x)
}
