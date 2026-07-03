// Package queue is the heart of the simulation: the RabbitMQ producer and
// consumer that turn a registration into a delayed success/failure callback.
//
// It ports the reference EventProcessorService faithfully, preserving the
// observable broker behavior and its quirks:
//
//   - Two durable queues (payments-queue, shipments-queue). Messages are the
//     {pattern,data} envelope produced by @nestjs/microservices RMQ emit,
//     published persistent (deliveryMode=2). The consumer only reads .data.
//   - Per message, fresh reads of the per-minute limit and processing delay
//     from ECS config. Rate-limit gate BEFORE ack: if count >= max, mark the
//     queue paused and return WITHOUT acking (the message stays unacked for
//     redelivery). Then ack, then the blocked check (read-and-delete the
//     in-memory record; suppress the automatic callback if it was blocked),
//     then count++ — so blocked messages do NOT count toward the limit but do
//     pass the gate. This exact ordering is part of the contract.
//   - The delay is captured at consume time; the callback fires after
//     delay seconds and rolls rand < successRate for SUCCEEDED/DELIVERED else
//     FAILED. Ack happens before the delay (at-most-once callback).
//   - A minute-boundary reset (cron '0 * * * * *') zeroes the counters and,
//     if any queue was paused while its max > 0, reconnects the whole
//     connection to force the broker to redeliver the unacked (rate-limited)
//     messages.
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	// PaymentsQueue and ShipmentsQueue are the two durable queue names.
	PaymentsQueue  = "payments-queue"
	ShipmentsQueue = "shipments-queue"

	// registerPaymentPattern / registerShipmentPattern are the emit patterns
	// used in the {pattern,data} envelope. The consumer ignores them; they are
	// reproduced for wire fidelity with the reference producer.
	registerPaymentPattern  = "register-payment"
	registerShipmentPattern = "register-shipment"

	// connectRetryDelay is the fixed reconnect retry interval on startup
	// (reference: setTimeout(5000) between connect attempts).
	connectRetryDelay = 5 * time.Second
)

// Config is the ECS tunable resolver the processor reads per message.
type Config interface {
	GetCurrentVariableValueNumber(name string, fallback float64) float64
}

// Blocker checks (and consumes) whether an id was manually blocked. The store
// repositories implement this: IsBlocked is a read-and-delete.
type Blocker interface {
	IsBlocked(id string) bool
}

// Callback delivers the rolled status to the originating service.
type Callback interface {
	SendUpdateToPayment(ctx context.Context, paymentID, status string)
	SendUpdateToShipment(ctx context.Context, shipmentID, status string)
}

// envelope is the message body: {"pattern": "...", "data": {...}}. Only data
// is read on consume.
type envelope struct {
	Pattern string          `json:"pattern"`
	Data    json.RawMessage `json:"data"`
}

// idData extracts the id from a message's data. The reference reads
// data.paymentId || data.shipmentId.
type idData struct {
	PaymentID  string `json:"paymentId"`
	ShipmentID string `json:"shipmentId"`
}

// Processor owns the RabbitMQ connection, the per-queue rate-limit state, and
// the consumer/reset lifecycle.
type Processor struct {
	url       string
	cfg       Config
	payments  Blocker
	shipments Blocker
	callback  Callback
	rng       *rand.Rand

	// mu guards the connection/channel handles and the rate-limit maps, all of
	// which are touched by both the consumer goroutines and the reset ticker.
	mu                sync.Mutex
	conn              *amqp.Connection
	channel           *amqp.Channel
	messageCounts     map[string]float64
	processingAllowed map[string]bool

	// consumerGen increments on every (re)connect so stale consumer goroutines
	// from a previous connection exit instead of touching the new channel.
	consumerGen int

	// ctx is the service lifetime context; cancellation stops the reset ticker
	// and consumer loops.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// queues is the fixed queue set, iterated in this order everywhere (matches the
// reference's `queues` array).
var queues = []string{PaymentsQueue, ShipmentsQueue}

// NewProcessor builds a processor. url is RABBITMQ_URL (already validated
// non-"NOT_SET" by the caller). rng may be nil to use a time-seeded default.
func NewProcessor(cfg Config, payments, shipments Blocker, cb Callback, url string, rng *rand.Rand) *Processor {
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return &Processor{
		url:               url,
		cfg:               cfg,
		payments:          payments,
		shipments:         shipments,
		callback:          cb,
		rng:               rng,
		messageCounts:     map[string]float64{},
		processingAllowed: map[string]bool{},
	}
}

// Start connects to RabbitMQ (retrying every 5s until reachable, as the
// reference does), initializes the per-queue state and consumers, and launches
// the minute-boundary reset loop. It blocks until the initial connection
// succeeds or ctx is cancelled.
func (p *Processor) Start(ctx context.Context) error {
	p.ctx, p.cancel = context.WithCancel(ctx)

	for {
		if err := p.connect(); err != nil {
			slog.Error("Failed to connect to RabbitMQ", "error", err)
			slog.Debug("Retrying in 5 seconds.")
			select {
			case <-time.After(connectRetryDelay):
			case <-p.ctx.Done():
				return p.ctx.Err()
			}
			continue
		}
		break
	}

	p.mu.Lock()
	for _, q := range queues {
		p.messageCounts[q] = 0
		p.processingAllowed[q] = true
	}
	gen := p.consumerGen
	for _, q := range queues {
		if err := p.startConsumer(q, gen); err != nil {
			p.mu.Unlock()
			return fmt.Errorf("start consumer %s: %w", q, err)
		}
	}
	p.mu.Unlock()

	p.wg.Add(1)
	go p.resetLoop()
	return nil
}

// connect establishes the connection and channel and asserts both queues
// durable. Caller must not hold p.mu (it locks internally to swap handles).
func (p *Processor) connect() error {
	conn, err := amqp.Dial(p.url)
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return err
	}
	for _, q := range queues {
		if _, err := ch.QueueDeclare(q, true /*durable*/, false, false, false, nil); err != nil {
			_ = ch.Close()
			_ = conn.Close()
			return err
		}
	}
	p.mu.Lock()
	p.conn = conn
	p.channel = ch
	p.consumerGen++
	p.mu.Unlock()
	slog.Info("Connected to RabbitMQ.")
	return nil
}

// Publish emits a registration to the given queue as the {pattern,data}
// envelope, persistent (deliveryMode=2) so it survives on the durable queue.
// pattern is register-payment or register-shipment; data is the raw
// registration body (the full DTO, incl. amount for payments).
func (p *Processor) Publish(ctx context.Context, queue string, data any) error {
	var pattern string
	switch queue {
	case PaymentsQueue:
		pattern = registerPaymentPattern
	case ShipmentsQueue:
		pattern = registerShipmentPattern
	default:
		return fmt.Errorf("unknown queue %s", queue)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	body, err := json.Marshal(envelope{Pattern: pattern, Data: raw})
	if err != nil {
		return err
	}

	p.mu.Lock()
	ch := p.channel
	p.mu.Unlock()
	if ch == nil {
		return fmt.Errorf("not connected to RabbitMQ")
	}
	return ch.PublishWithContext(ctx, "" /*default exchange*/, queue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent, // deliveryMode=2
		Body:         body,
	})
}

// startConsumer begins consuming queue with manual ack and dispatches each
// delivery to consumeMessage in a single goroutine, preserving the reference's
// sequential per-message processing (which the counter accounting depends on).
// gen ties the goroutine to the current connection so it exits after a
// reconnect. Caller holds p.mu.
func (p *Processor) startConsumer(queue string, gen int) error {
	deliveries, err := p.channel.Consume(queue, "" /*consumer tag*/, false /*autoAck*/, false, false, false, nil)
	if err != nil {
		return err
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			select {
			case <-p.ctx.Done():
				return
			case d, ok := <-deliveries:
				if !ok {
					// Channel closed (reconnect or shutdown); this consumer is
					// done. A new one was/will be started for the new channel.
					return
				}
				p.consumeMessage(queue, d, gen)
			}
		}
	}()
	return nil
}

// consumeMessage processes one delivery, reproducing the reference ordering
// exactly:
//
//  1. read fresh max = *_PER_MINUTE, delay = *_PROCESSING_TIME.
//  2. gate: if count >= max → log, pause queue, return WITHOUT acking.
//  3. parse data; ack.
//  4. blocked check (read-and-delete): if blocked, return (suppress callback).
//  5. count++.
//  6. schedule the callback after delay seconds.
func (p *Processor) consumeMessage(queue string, d amqp.Delivery, gen int) {
	maxMessages := p.getMaxPerMinute(queue)
	delay := p.getProcessingTime(queue)

	// The reference runs this entire body atomically on the single-threaded
	// event loop (the minute-reset cron cannot interleave mid-message). Hold
	// p.mu across the gate → ack → blocked check → count++ so the reset ticker
	// (the only other writer of these fields) cannot reset the counter between
	// the gate and the increment. isBlocked locks the store, not p.mu, and no
	// path takes p.mu while holding the store lock, so there is no cycle.
	p.mu.Lock()
	defer p.mu.Unlock()

	// Ignore deliveries from a superseded connection (defensive; the closed
	// channel normally ends the goroutine first).
	if gen != p.consumerGen {
		return
	}
	count := p.messageCounts[queue]
	// Gate: JS `count >= max`. With max == NaN this is false (limit disabled).
	if count >= maxMessages {
		slog.Debug(fmt.Sprintf("[%s] Maximum message count reached. Pausing processing until reset.", queue))
		p.processingAllowed[queue] = false
		return // no ack: message stays unacked for redelivery at reset.
	}

	data, ok := parseData(d.Body)
	if !ok {
		// Reference JSON.parse would throw and crash the consumer; we log and
		// ack (drop) the poison message instead of stalling the queue.
		slog.Error(fmt.Sprintf("[%s] Failed to parse message", queue))
		_ = d.Ack(false)
		return
	}
	slog.Debug(fmt.Sprintf("[%s] Processing message: %s [%d/%s]", queue, string(data), int(count)+1, formatMax(maxMessages)))

	if err := d.Ack(false); err != nil {
		slog.Error(fmt.Sprintf("[%s] Failed to ack message", queue), "error", err)
	}

	// Blocked check: read-and-delete the in-memory record. Suppress the
	// automatic callback if it was manually blocked.
	if p.isBlocked(queue, data) {
		return
	}

	p.messageCounts[queue]++

	slog.Info(fmt.Sprintf("[%s] Sending Event Update after [%s]s", queue, formatDelay(delay)))
	p.scheduleCallback(queue, data, delay)
}

// scheduleCallback fires the callback after delay seconds. delay is captured
// here (a mid-flight config change does not affect it). A NaN/negative delay
// fires immediately (JS setTimeout treats those as 0). The id is
// data.paymentId || data.shipmentId; a missing id is logged (the reference
// threw inside the timer, which we guard).
func (p *Processor) scheduleCallback(queue string, data []byte, delay float64) {
	var d time.Duration
	if math.IsNaN(delay) || delay <= 0 {
		d = 0
	} else {
		d = time.Duration(delay * float64(time.Second))
	}
	p.wg.Add(1)
	time.AfterFunc(d, func() {
		defer p.wg.Done()
		id := extractID(data)
		if id == "" {
			slog.Error("No ID found in message")
			return
		}
		p.buildEventUpdate(queue, id)
	})
}

// buildEventUpdate rolls the outcome and sends the callback. successful =
// rand.Float64() < successRate (rate 1.0 always succeeds, 0.0 always fails).
func (p *Processor) buildEventUpdate(queue, id string) {
	successRate := p.getSuccessRate(queue)
	successful := p.rng.Float64() < successRate
	switch queue {
	case PaymentsQueue:
		status := "FAILED"
		if successful {
			status = "SUCCEEDED"
		}
		p.callback.SendUpdateToPayment(p.ctx, id, status)
	case ShipmentsQueue:
		status := "FAILED"
		if successful {
			status = "DELIVERED"
		}
		p.callback.SendUpdateToShipment(p.ctx, id, status)
	}
}

// isBlocked dispatches the read-and-delete to the right repository.
func (p *Processor) isBlocked(queue string, data []byte) bool {
	id := extractID(data)
	switch queue {
	case PaymentsQueue:
		return p.payments.IsBlocked(id)
	case ShipmentsQueue:
		return p.shipments.IsBlocked(id)
	default:
		// Reference throws Error('Unknown queue'); unreachable for our two
		// queues, but log rather than panic.
		slog.Error("Unknown queue", "queue", queue)
		return false
	}
}

// resetLoop implements the '0 * * * * *' cron: it wakes at each minute boundary
// and calls resetMessageCounts. It aligns to the wall-clock minute so the reset
// batch redelivery lands at the top of the minute, as the reference cron does.
func (p *Processor) resetLoop() {
	defer p.wg.Done()
	for {
		next := time.Now().Truncate(time.Minute).Add(time.Minute)
		wait := time.Until(next)
		timer := time.NewTimer(wait)
		select {
		case <-p.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			p.resetMessageCounts()
		}
	}
}

// resetMessageCounts zeroes every queue's counter and clears its paused flag;
// if any queue was paused while its max > 0, it reconnects to force the broker
// to redeliver that queue's unacked (rate-limited) messages.
func (p *Processor) resetMessageCounts() {
	slog.Debug("Resetting message counts.")
	reconnect := false
	p.mu.Lock()
	for _, q := range queues {
		maxMessages := p.getMaxPerMinuteLocked(q)
		if !p.processingAllowed[q] && maxMessages > 0 {
			reconnect = true
		}
		p.messageCounts[q] = 0
		p.processingAllowed[q] = true
	}
	p.mu.Unlock()

	if reconnect {
		p.reconnect()
	}
}

// reconnect closes the current channel/connection and re-establishes them and
// the consumers, causing the broker to redeliver all unacked messages. Errors
// are logged (the reference swallows them).
func (p *Processor) reconnect() {
	slog.Debug("Reconnecting to RabbitMQ...")
	p.mu.Lock()
	ch, conn := p.channel, p.conn
	p.channel, p.conn = nil, nil
	p.mu.Unlock()

	if ch != nil {
		_ = ch.Close()
	}
	if conn != nil {
		_ = conn.Close()
	}

	if err := p.connect(); err != nil {
		slog.Error("Failed to reconnect to RabbitMQ", "error", err)
		return
	}
	p.mu.Lock()
	gen := p.consumerGen
	for _, q := range queues {
		if err := p.startConsumer(q, gen); err != nil {
			slog.Error("Failed to reconnect to RabbitMQ", "error", err)
		}
	}
	p.mu.Unlock()
}

// Close stops the reset loop and consumers and closes the channel/connection.
// Safe to call once on shutdown.
func (p *Processor) Close() {
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Lock()
	ch, conn := p.channel, p.conn
	p.channel, p.conn = nil, nil
	p.mu.Unlock()
	if ch != nil {
		_ = ch.Close()
	}
	if conn != nil {
		_ = conn.Close()
	}
	p.wg.Wait()
}

// --- ECS tunable helpers (fresh read per use, matching the reference) ---

// getProcessingTime returns *_PROCESSING_TIME (PAYMENT/SHIPMENT), fallback 5.
func (p *Processor) getProcessingTime(queue string) float64 {
	key := "SHIPMENT"
	if queue == PaymentsQueue {
		key = "PAYMENT"
	}
	return p.cfg.GetCurrentVariableValueNumber(key+"_PROCESSING_TIME", 5)
}

// getMaxPerMinute returns *_PER_MINUTE (PAYMENTS/SHIPMENTS — note the plural
// split from PROCESSING_TIME/SUCCESS_RATE), fallback 1_000_000.
func (p *Processor) getMaxPerMinute(queue string) float64 {
	key := "SHIPMENTS"
	if queue == PaymentsQueue {
		key = "PAYMENTS"
	}
	return p.cfg.GetCurrentVariableValueNumber(key+"_PER_MINUTE", 1000000)
}

// getMaxPerMinuteLocked is getMaxPerMinute for callers already holding p.mu
// (the config service has its own lock, so this is only about not re-locking
// p.mu; the call itself is lock-independent).
func (p *Processor) getMaxPerMinuteLocked(queue string) float64 {
	return p.getMaxPerMinute(queue)
}

// getSuccessRate returns *_SUCCESS_RATE (PAYMENT/SHIPMENT), fallback 0.95.
func (p *Processor) getSuccessRate(queue string) float64 {
	key := "SHIPMENT"
	if queue == PaymentsQueue {
		key = "PAYMENT"
	}
	return p.cfg.GetCurrentVariableValueNumber(key+"_SUCCESS_RATE", 0.95)
}

// --- helpers ---

// parseData extracts the .data raw bytes from a message envelope.
func parseData(body []byte) ([]byte, bool) {
	var e envelope
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, false
	}
	return e.Data, true
}

// extractID returns paymentId || shipmentId from the data bytes.
func extractID(data []byte) string {
	var d idData
	if err := json.Unmarshal(data, &d); err != nil {
		return ""
	}
	if d.PaymentID != "" {
		return d.PaymentID
	}
	return d.ShipmentID
}

// formatMax renders the max for the "[count/max]" log line (NaN → "NaN",
// integer-valued → no decimal point).
func formatMax(max float64) string {
	if math.IsNaN(max) {
		return "NaN"
	}
	if max == math.Trunc(max) {
		return fmt.Sprintf("%d", int64(max))
	}
	return fmt.Sprintf("%g", max)
}

// formatDelay renders the delay for the "after [delay]s" log line.
func formatDelay(delay float64) string {
	if math.IsNaN(delay) {
		return "NaN"
	}
	if delay == math.Trunc(delay) {
		return fmt.Sprintf("%d", int64(delay))
	}
	return fmt.Sprintf("%g", delay)
}
