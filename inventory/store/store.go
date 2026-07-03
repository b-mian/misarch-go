// Package store is the inventory service persistence layer. It wraps the two
// MongoDB collections the original NestJS service used — `productitems` (one
// document per physical unit) and `productvariantpartials` (a denormalized
// replica of catalog variant IDs) — behind typed methods over the mongo-driver.
//
// Document shapes, collection names, BSON field names and the string `_id`
// UUID strategy are reproduced verbatim from the original Mongoose schemas
// (spec §5) so a Go port can share a database with historic data.
package store

import (
	"go.mongodb.org/mongo-driver/mongo"
)

// Collection names use Mongoose's default pluralization; they must not drift.
const (
	collProductItems           = "productitems"
	collProductVariantPartials = "productvariantpartials"
)

// Inventory status values, stored as these exact uppercase strings.
const (
	StatusInStorage     = "IN_STORAGE"
	StatusReserved      = "RESERVED"
	StatusInFulfillment = "IN_FULFILLMENT"
	StatusShipped       = "SHIPPED"
	StatusDelivered     = "DELIVERED"
	StatusReturned      = "RETURNED"
	StatusLost          = "LOST"
)

// Store provides persistence operations for product items and the product
// variant partial replica.
type Store struct {
	db           *mongo.Database
	client       *mongo.Client
	productItems *mongo.Collection
	variantParts *mongo.Collection
}

// New builds a Store backed by the given Mongo database.
func New(db *mongo.Database) *Store {
	return &Store{
		db:           db,
		client:       db.Client(),
		productItems: db.Collection(collProductItems),
		variantParts: db.Collection(collProductVariantPartials),
	}
}

// ProductItem is one document of the `productitems` collection. OrderID is a
// pointer so it can be absent (never reserved) or BSON null (after release);
// both decode to nil and surface as GraphQL `orderId: null`. It is tagged
// `omitempty` so freshly created items carry no `orderId` field at all,
// matching the original insert (spec §3.4 / §5).
type ProductItem struct {
	ID              string  `bson:"_id"`
	ProductVariant  string  `bson:"productVariant"`
	InventoryStatus string  `bson:"inventoryStatus"`
	OrderID         *string `bson:"orderId,omitempty"`
}

// Filter is the translated Mongo filter for a product-item query. Only the set
// keys are applied. Keys use the real document field names — this is the
// CORRECT translation used for counts (spec §3.3 totalCount).
type Filter struct {
	ProductVariant  *string
	InventoryStatus *string
}
