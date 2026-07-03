// Package store holds the simulation's two in-memory repositories (payments
// and shipments). They exist alongside the RabbitMQ queues purely to let a
// manual /update "block" an entity so the automatic queue result is discarded.
// State is lost on restart (the reference used plain in-process arrays);
// because both HTTP handlers and the AMQP consumer goroutine touch them, every
// operation is mutex-guarded here (Node relied on its single-threaded event
// loop instead).
//
// Insertion order is preserved (findAll returns entries in push order) to
// match the reference's array-backed repositories; findAll is debug-only.
package store

import "sync"

// Payment is the in-memory payment record. [QUIRK] amount is intentionally NOT
// stored (the reference drops it from the record though it is still sent on the
// queue). JSON tags match the reference entity shape for POST /payment/findAll.
type Payment struct {
	ID          string `json:"id"`
	PaymentType string `json:"paymentType"`
	Blocked     bool   `json:"blocked"`
}

// Shipment is the in-memory shipment record.
type Shipment struct {
	ID      string `json:"id"`
	Blocked bool   `json:"blocked"`
}

// PaymentRepository stores payments in memory for manual-update tracking.
type PaymentRepository struct {
	mu       sync.Mutex
	payments []Payment
}

// NewPaymentRepository returns an empty payment repository.
func NewPaymentRepository() *PaymentRepository { return &PaymentRepository{} }

// Create appends a payment (create in the reference just pushes).
func (r *PaymentRepository) Create(p Payment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.payments = append(r.payments, p)
}

// FindByID reports whether a payment with id exists.
func (r *PaymentRepository) FindByID(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.indexOf(id) >= 0
}

// FindAll returns a copy of all payments in insertion order.
func (r *PaymentRepository) FindAll() []Payment {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Payment, len(r.payments))
	copy(out, r.payments)
	return out
}

// Block marks the payment with id as blocked (the reference's
// update(id,{blocked:true})). No-op if absent.
func (r *PaymentRepository) Block(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i := r.indexOf(id); i >= 0 {
		r.payments[i].Blocked = true
	}
}

// IsBlocked is the atomic read-and-delete the consumer relies on: if the
// payment is absent it returns false (restart case — automatic processing
// proceeds); otherwise it removes the record and returns its blocked flag. This
// deletes at consume time (before the callback delay), so a later manual update
// for the same id will 404 — a [QUIRK/RACE] that must be preserved.
func (r *PaymentRepository) IsBlocked(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	i := r.indexOf(id)
	if i < 0 {
		return false
	}
	blocked := r.payments[i].Blocked
	r.payments = append(r.payments[:i], r.payments[i+1:]...)
	return blocked
}

// indexOf returns the index of the first payment with id, or -1. Caller holds
// the lock.
func (r *PaymentRepository) indexOf(id string) int {
	for i := range r.payments {
		if r.payments[i].ID == id {
			return i
		}
	}
	return -1
}

// ShipmentRepository stores shipments in memory for manual-update tracking.
type ShipmentRepository struct {
	mu        sync.Mutex
	shipments []Shipment
}

// NewShipmentRepository returns an empty shipment repository.
func NewShipmentRepository() *ShipmentRepository { return &ShipmentRepository{} }

// Create appends a shipment.
func (r *ShipmentRepository) Create(s Shipment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shipments = append(r.shipments, s)
}

// FindByID reports whether a shipment with id exists.
func (r *ShipmentRepository) FindByID(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.indexOf(id) >= 0
}

// FindAll returns a copy of all shipments in insertion order.
func (r *ShipmentRepository) FindAll() []Shipment {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Shipment, len(r.shipments))
	copy(out, r.shipments)
	return out
}

// Block marks the shipment with id as blocked. No-op if absent.
func (r *ShipmentRepository) Block(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i := r.indexOf(id); i >= 0 {
		r.shipments[i].Blocked = true
	}
}

// IsBlocked is the atomic read-and-delete for shipments (see the payment
// variant). Absent → false; present → delete and return blocked.
func (r *ShipmentRepository) IsBlocked(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	i := r.indexOf(id)
	if i < 0 {
		return false
	}
	blocked := r.shipments[i].Blocked
	r.shipments = append(r.shipments[:i], r.shipments[i+1:]...)
	return blocked
}

// indexOf returns the index of the first shipment with id, or -1. Caller holds
// the lock.
func (r *ShipmentRepository) indexOf(id string) int {
	for i := range r.shipments {
		if r.shipments[i].ID == id {
			return i
		}
	}
	return -1
}
