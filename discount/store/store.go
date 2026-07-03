// Package store is the discount service persistence layer. It owns the
// embedded Postgres migrations and exposes typed, parameterized
// query/mutation methods over a pgx pool. Rows are returned as store-level
// structs; the graph layer maps them to GraphQL models. Table and column
// names are all lowercase (Postgres folds unquoted identifiers), matching the
// original Flyway DDL as it lands on disk.
package store

import (
	"context"
	"embed"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrations holds the embedded SQL migrations applied by db.Migrate at
// startup. They live under store/migrations/ so a single //go:embed in this
// package can reach them — go:embed cannot cross into a parent or sibling
// directory.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// Store provides persistence operations for the discount bounded context.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Store backed by the given pgx pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Discount is a row of the discountentity table.
type Discount struct {
	ID               uuid.UUID
	Discount         float64
	MaxUsagesPerUser *int
	ValidFrom        time.Time
	ValidUntil       time.Time
	MinOrderAmount   *int
}

// Coupon is a row of the couponentity table.
type Coupon struct {
	ID         uuid.UUID
	Usages     int
	MaxUsages  *int
	ValidFrom  time.Time
	ValidUntil time.Time
	Code       string
	DiscountID uuid.UUID
}

// DiscountUsage is a row of the discountusageentity table. Usages is stored as
// BIGINT; the GraphQL type narrows it to Int (matching the original .toInt()).
type DiscountUsage struct {
	ID         uuid.UUID
	DiscountID uuid.UUID
	UserID     uuid.UUID
	Usages     int64
}

// Connection is a generic pagination result mirroring the GraphQL *Connection
// types: the page of nodes plus the total count and next-page flag.
type Connection[T any] struct {
	Nodes       []T
	TotalCount  int
	HasNextPage bool
}

// Column projections, qualified with the entity alias so they are unambiguous
// under the connection joins. Postgres folded the Flyway identifiers to
// lowercase, so every column is referenced lowercase.
const (
	discountColumns = "discountentity.id, discountentity.discount, discountentity.maxusagesperuser, " +
		"discountentity.validfrom, discountentity.validuntil, discountentity.minorderamount"
	couponColumns = "couponentity.id, couponentity.usages, couponentity.maxusages, " +
		"couponentity.validfrom, couponentity.validuntil, couponentity.code, couponentity.discountid"
	discountUsageColumns = "discountusageentity.id, discountusageentity.discountid, " +
		"discountusageentity.userid, discountusageentity.usages"
	categoryColumns       = "categoryentity.id"
	productColumns        = "productentity.id"
	productVariantColumns = "productvariantentity.id, productvariantentity.productid"
	userColumns           = "userentity.id"
)

// Category / Product / ProductVariant / User are id-only (plus productid for a
// variant) replicas; the connections return just the id(s) that the foreign
// federated stubs need.
type (
	// CategoryRow is a categoryentity row.
	CategoryRow struct{ ID uuid.UUID }
	// ProductRow is a productentity row.
	ProductRow struct{ ID uuid.UUID }
	// ProductVariantRow is a productvariantentity row.
	ProductVariantRow struct {
		ID        uuid.UUID
		ProductID uuid.UUID
	}
	// UserRow is a userentity row.
	UserRow struct{ ID uuid.UUID }
)

// querier is the subset of pgxpool.Pool / pgx.Tx the store methods need, so the
// same helpers work against a pool or a transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}
