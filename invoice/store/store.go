// Package store is the invoice service persistence layer. It owns the MongoDB
// collections (invoices, vendor_address, user) and exposes typed query/mutation
// methods used by the GraphQL resolvers and the Dapr event handlers.
//
// Every document type mirrors the exact BSON shape written by the original Rust
// service (bson::Uuid = Binary subtype 4; snake_case field names). The buggy
// field-name mismatches of the original (addresses vs user_addresses, the
// $set-only-_id vendor upsert, the invalid $pull) are reproduced faithfully in
// the store methods so the external behavior is identical — see the spec's
// "defect ledger".
package store

import (
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Collection names in the invoice-database, verbatim from the Rust service.
const (
	collInvoices      = "invoices"
	collVendorAddress = "vendor_address"
	collUser          = "user"
)

// Store provides persistence operations backed by MongoDB.
type Store struct {
	invoices      *mongo.Collection
	vendorAddress *mongo.Collection
	user          *mongo.Collection
}

// New builds a Store over the given database. Each collection is bound with the
// UUID-as-Binary-subtype-4 registry so ids serialize exactly like the Rust
// service's bson::Uuid.
func New(db *mongo.Database) *Store {
	reg := buildRegistry()
	opt := options.Collection().SetRegistry(reg)
	return &Store{
		invoices:      db.Collection(collInvoices, opt),
		vendorAddress: db.Collection(collVendorAddress, opt),
		user:          db.Collection(collUser, opt),
	}
}

// UserAddress is the embedded address subdocument, matching the Rust
// foreign_types::UserAddress BSON shape. Only id is exposed via GraphQL; the
// rest are used to render the invoice content.
type UserAddress struct {
	ID          uuid.UUID `bson:"_id"`
	Street1     string    `bson:"street1"`
	Street2     string    `bson:"street2"`
	City        string    `bson:"city"`
	PostalCode  string    `bson:"postal_code"`
	Country     string    `bson:"country"`
	CompanyName string    `bson:"company_name"`
	UserID      uuid.UUID `bson:"user_id"`
}

// VendorAddress is the embedded vendor-address subdocument, matching the Rust
// foreign_types::VendorAddress BSON shape. Only id is exposed via GraphQL.
type VendorAddress struct {
	ID          uuid.UUID `bson:"_id"`
	Street1     string    `bson:"street1"`
	Street2     string    `bson:"street2"`
	City        string    `bson:"city"`
	PostalCode  string    `bson:"postal_code"`
	Country     string    `bson:"country"`
	CompanyName string    `bson:"company_name"`
}

// Invoice is a document of the invoices collection. Field names/tags are the
// exact BSON names written by the Rust service.
type Invoice struct {
	ID            uuid.UUID     `bson:"_id"`
	OrderID       uuid.UUID     `bson:"order_id"`
	IssuedAt      time.Time     `bson:"issued_at"`
	Content       string        `bson:"content"`
	UserAddress   UserAddress   `bson:"user_address"`
	VendorAddress VendorAddress `bson:"vendor_address"`
	VatNumber     *string       `bson:"vat_number"`
}

// User is a document of the user collection. Mirrors the Rust
// foreign_types::User: deserialization REQUIRES first_name/last_name to be
// present (there are no serde defaults), which — combined with the projection
// bug in the address lookup — is part of the reproduced failure behavior.
//
// Note the two array fields: `addresses` is written as [] at user insert and is
// the field every READ path uses; `user_addresses` is where the address-created
// handler actually PUSHES (the original's field-name bug). Both are modeled so
// the persisted documents match byte-for-byte.
type User struct {
	ID            uuid.UUID     `bson:"_id"`
	FirstName     string        `bson:"first_name"`
	LastName      string        `bson:"last_name"`
	Addresses     []UserAddress `bson:"addresses"`
	UserAddresses []UserAddress `bson:"user_addresses,omitempty"`
}
