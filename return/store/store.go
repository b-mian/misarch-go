// Package store is the return service persistence layer. It owns the embedded
// Postgres migrations and exposes typed, parameterized query/mutation methods
// over a pgx pool. Rows are returned as store-level structs; the graph layer
// maps them to GraphQL models (including the relationship extraFields).
//
// The local tables (OrderEntity, OrderItemEntity, ShipmentEntity,
// ProductVariantVersionEntity) are read models materialized from Dapr events;
// ReturnEntity is the only table this service writes through the GraphQL API
// (the createReturn mutation). The load-bearing return-window triggers on
// OrderItemEntity live in the embedded migration and fire on the order-item
// returnedWithId UPDATE during createReturn.
package store

import (
	"context"
	"embed"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrations holds the embedded SQL migrations applied by db.Migrate at
// startup. They live under store/migrations/ (rather than a service-root
// migrations/ dir) so a single //go:embed in this package can reach them —
// go:embed cannot cross into a parent or sibling directory.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// Store provides persistence operations for returns and the local read models.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Store backed by the given pgx pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Return is a row of the returnentity table. refundedAmount is stored as a
// 64-bit BIGINT (matching the Kotlin Long); the graph layer narrows it to a
// 32-bit Int on GraphQL output while the event payload keeps the full int64.
type Return struct {
	ID             uuid.UUID
	Reason         string
	RefundedAmount int64
	CreatedAt      time.Time
	OrderID        uuid.UUID
}

// OrderItem is a row of the orderitementity local read model. Every field but
// id/orderId is nullable because the row is materialized incrementally from two
// independent events (order-created populates compensatableAmount /
// productVariantVersionId; shipment-created populates sentWithId), and
// returnedWithId is set only when a return links the item.
type OrderItem struct {
	ID                      uuid.UUID
	ReturnedWithID          *uuid.UUID
	SentWithID              *uuid.UUID
	OrderID                 uuid.UUID
	CompensatableAmount     *int64
	ProductVariantVersionID *uuid.UUID
}

// Shipment is a row of the shipmententity local read model.
type Shipment struct {
	ID          uuid.UUID
	DeliveredAt *time.Time
}

// ProductVariantVersion is a row of the productvariantversionentity local read
// model. Because of BUG-1 (the catalog/product-variant-version/created topic is
// never subscribed) this table is empty in production.
type ProductVariantVersion struct {
	ID                   uuid.UUID
	CanBeReturnedForDays *int
}

// Connection is a generic pagination result mirroring the GraphQL *Connection
// types: the page of nodes plus the total count and next-page flag.
type Connection[T any] struct {
	Nodes       []T
	TotalCount  int
	HasNextPage bool
}

// querier is the subset of pgxpool.Pool / pgx.Tx the store methods need, so the
// same helpers work against a pool or a transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}
