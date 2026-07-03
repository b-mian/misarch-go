// Package store is the shipment service persistence layer. It owns the
// embedded Postgres migrations and exposes typed, parameterized
// query/mutation methods over a pgx pool. Rows are returned as store-level
// structs; the graph layer maps them to GraphQL models (including the
// relationship extraFields the field resolvers need).
package store

import (
	"embed"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrations holds the embedded SQL migrations applied by db.Migrate at
// startup. They live under store/migrations/ (rather than a service-root
// migrations/ dir) so a single //go:embed in this package can reach them —
// go:embed cannot cross into a parent or sibling directory.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// Store provides persistence operations for the shipment service.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Store backed by the given pgx pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Pool exposes the underlying pool for callers that need to run their own
// transactions spanning several store methods (the payment/return event
// handlers do not — each shipment is created independently — so this is
// currently unused, but kept minimal-surface for parity with the reference).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// ShipmentMethod is a row of the shipmentmethodentity table.
type ShipmentMethod struct {
	ID                uuid.UUID
	Name              string
	Description       string
	ExternalReference string
	BaseFees          int
	FeesPerItem       int
	FeesPerKg         int
	ArchivedAt        *time.Time // null = not archived
}

// Shipment is a row of the shipmententity table.
type Shipment struct {
	ID                uuid.UUID
	Status            string // ShipmentStatus enum NAME string
	ShipmentMethodID  uuid.UUID
	ShipmentAddressID uuid.UUID
	OrderID           *uuid.UUID
	ReturnID          *uuid.UUID
}

// OrderItem is a row of the orderitementity table.
type OrderItem struct {
	ID                      uuid.UUID
	SentWithID              uuid.UUID
	ProductVariantVersionID uuid.UUID
	Quantity                int
}

// Address is a row of the addressentity table. UserID is null for vendor
// addresses; the discriminator for the Address GraphQL interface.
type Address struct {
	ID          uuid.UUID
	UserID      *uuid.UUID
	Street1     string
	Street2     string
	City        string
	PostalCode  string
	Country     string
	CompanyName *string
}

// ProductVariantVersion is a row of the productvariantversionentity table.
type ProductVariantVersion struct {
	ID     uuid.UUID
	Weight float64
}

// Connection is a generic pagination result mirroring the GraphQL *Connection
// types: the page of nodes plus the total count and next-page flag.
type Connection[T any] struct {
	Nodes       []T
	TotalCount  int
	HasNextPage bool
}
