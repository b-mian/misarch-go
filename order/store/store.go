// Package store is the order service persistence layer. It owns the MongoDB
// collections and exposes typed query/mutation methods used by the GraphQL
// resolvers and the Dapr event handlers.
//
// Every document type mirrors the exact BSON shape written by the original Rust
// service (bson::Uuid = Binary subtype 4; snake_case field names; enums stored
// as SCREAMING_SNAKE strings; embedded order-item snapshots). Known
// idiosyncrasies of the original are reproduced faithfully so external
// behavior is identical:
//
//   - order items live ONLY in orders.internal_order_items; the order_items
//     collection is declared but never written;
//   - product_variants.is_publicly_visible is a bool on create but a string
//     after an update event (IsPubliclyVisible is modeled as `any`);
//   - the embedded user in an order always carries an empty user_address_ids;
//   - no indexes are created (only the implicit _id index).
package store

import (
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Collection names in the order-database, verbatim from the Rust service.
const (
	collOrders             = "orders"
	collOrderItems         = "order_items" // declared, never written
	collUsers              = "users"
	collProductVariants    = "product_variants"
	collTaxRates           = "tax_rates"
	collCoupons            = "coupons"
	collShipmentMethods    = "shipment_methods"
	collOrderCompensations = "order_compensations"
)

// Store provides persistence operations backed by MongoDB. Each collection is
// bound with the UUID-as-Binary-subtype-4 registry so ids serialize exactly
// like the Rust service's bson::Uuid.
type Store struct {
	orders             *mongo.Collection
	orderItems         *mongo.Collection
	users              *mongo.Collection
	productVariants    *mongo.Collection
	taxRates           *mongo.Collection
	coupons            *mongo.Collection
	shipmentMethods    *mongo.Collection
	orderCompensations *mongo.Collection
}

// New builds a Store over the given database.
func New(db *mongo.Database) *Store {
	reg := buildRegistry()
	opt := options.Collection().SetRegistry(reg)
	return &Store{
		orders:             db.Collection(collOrders, opt),
		orderItems:         db.Collection(collOrderItems, opt),
		users:              db.Collection(collUsers, opt),
		productVariants:    db.Collection(collProductVariants, opt),
		taxRates:           db.Collection(collTaxRates, opt),
		coupons:            db.Collection(collCoupons, opt),
		shipmentMethods:    db.Collection(collShipmentMethods, opt),
		orderCompensations: db.Collection(collOrderCompensations, opt),
	}
}

// ErrNoDocuments is re-exported so callers can distinguish "not found" from a
// driver/transport error without importing the mongo package directly.
var ErrNoDocuments = mongo.ErrNoDocuments

// ---------------------------------------------------------------------------
// Document shapes (exact BSON field names — see spec §5).
// ---------------------------------------------------------------------------

// User is a document of the users collection and the embedded user snapshot in
// an order. Consulted by the User entity resolver, User.orders (only _id) and
// order validation (only existence).
type User struct {
	ID             uuid.UUID   `bson:"_id"`
	UserAddressIDs []uuid.UUID `bson:"user_address_ids"`
}

// UUIDRef is the single-field {_id: UUID} subdocument used by shipment_address,
// invoice_address, shopping_cart_item, shipment_method, coupons and
// shipment_methods documents.
type UUIDRef struct {
	ID uuid.UUID `bson:"_id"`
}

// ProductVariantVersion is the embedded current_version of a product variant,
// and the point-in-time product_variant_version snapshot on an order item.
type ProductVariantVersion struct {
	ID        uuid.UUID `bson:"_id"`
	Price     uint32    `bson:"price"`
	TaxRateID uuid.UUID `bson:"tax_rate_id"`
}

// ProductVariant is a document of the product_variants collection and the
// embedded product_variant snapshot on an order item.
//
// IsPubliclyVisible is `any` because the create path stores a BOOL (true) while
// the update-event path stores a STRING ("true"/"false") — a latent bug in the
// original that we reproduce. Availability filtering in createOrder treats only
// a real bool `true` as visible (see IsVisible), matching how the Rust bool
// deserialize would accept the create-path value and fail/reject the string.
type ProductVariant struct {
	ID                uuid.UUID             `bson:"_id"`
	CurrentVersion    ProductVariantVersion `bson:"current_version"`
	IsPubliclyVisible any                   `bson:"is_publicly_visible"`
}

// IsVisible reports whether the variant counts as publicly visible for order
// creation. Only a BSON boolean true qualifies; a string ("true"/"false") — as
// written by the update event — does NOT, matching the original where the bool
// deserialize of a string-typed field breaks the query_objects call (the
// variant is effectively dropped/unusable).
func (p ProductVariant) IsVisible() bool {
	b, ok := p.IsPubliclyVisible.(bool)
	return ok && b
}

// TaxRateVersion is the embedded current_version of a tax rate, and the
// point-in-time tax_rate_version snapshot on an order item.
type TaxRateVersion struct {
	ID      uuid.UUID `bson:"_id"`
	Rate    float64   `bson:"rate"`
	Version uint32    `bson:"version"`
}

// TaxRate is a document of the tax_rates collection.
type TaxRate struct {
	ID             uuid.UUID      `bson:"_id"`
	CurrentVersion TaxRateVersion `bson:"current_version"`
}

// Discount is an element of an order item's internal_discounts array. The
// discount multiplier is stored so the (already-computed) compensatable_amount
// can be reproduced if needed; only _id is exposed publicly.
type Discount struct {
	ID       uuid.UUID `bson:"_id"`
	Discount float64   `bson:"discount"`
}

// OrderItem is an element of an order's internal_order_items array. It embeds
// point-in-time snapshots of the product variant / version / tax rate / applied
// discounts at creation time (orders are immutable snapshots).
type OrderItem struct {
	ID                    uuid.UUID             `bson:"_id"`
	CreatedAt             time.Time             `bson:"created_at"`
	ProductVariant        ProductVariant        `bson:"product_variant"`
	ProductVariantVersion ProductVariantVersion `bson:"product_variant_version"`
	TaxRateVersion        TaxRateVersion        `bson:"tax_rate_version"`
	ShoppingCartItem      UUIDRef               `bson:"shopping_cart_item"`
	Count                 uint64                `bson:"count"`
	CompensatableAmount   uint64                `bson:"compensatable_amount"`
	ShipmentMethod        UUIDRef               `bson:"shipment_method"`
	InternalDiscounts     []Discount            `bson:"internal_discounts"`
}

// Order is a document of the orders collection — the owned aggregate.
//
// OrderStatus / RejectionReason are stored as SCREAMING_SNAKE strings.
// RejectionReason is a pointer because it is null until (never, in practice)
// set. internal_order_items, shipment_address and vat_number are internal (not
// exposed via GraphQL).
type Order struct {
	ID                       uuid.UUID   `bson:"_id"`
	User                     User        `bson:"user"`
	CreatedAt                time.Time   `bson:"created_at"`
	OrderStatus              string      `bson:"order_status"`
	PlacedAt                 *time.Time  `bson:"placed_at"`
	RejectionReason          *string     `bson:"rejection_reason"`
	InternalOrderItems       []OrderItem `bson:"internal_order_items"`
	ShipmentAddress          UUIDRef     `bson:"shipment_address"`
	InvoiceAddress           UUIDRef     `bson:"invoice_address"`
	CompensatableOrderAmount uint64      `bson:"compensatable_order_amount"`
	PaymentInformationID     uuid.UUID   `bson:"payment_information_id"`
	VatNumber                *string     `bson:"vat_number"`
}

// OrderCompensation is a document of the order_compensations collection — the
// compensation log written by the shipment-creation-failed saga.
type OrderCompensation struct {
	ID                 uuid.UUID   `bson:"_id"`
	OrderID            uuid.UUID   `bson:"order_id"`
	OrderItemIDs       []uuid.UUID `bson:"order_item_ids"`
	TriggeredAt        time.Time   `bson:"triggered_at"`
	AmountToCompensate uint64      `bson:"amount_to_compensate"`
}
