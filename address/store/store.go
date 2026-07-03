// Package store is the address service persistence layer. It owns the embedded
// Postgres migrations and exposes typed, parameterized query/mutation methods
// over a pgx pool. Rows are returned as store-level structs; the graph layer
// maps them to GraphQL models (UserAddress vs VendorAddress by the userid
// discriminator).
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

// Store provides persistence operations for user and vendor addresses and the
// replicated user id set.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Store backed by the given pgx pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Address is a row of the addressentity table. A single table backs both user
// and vendor addresses: UserID distinguishes them (non-nil => user address,
// nil => vendor address). FirstName/LastName are constrained to be either both
// null or both non-null by a DB trigger; the graph layer folds them into the
// Name value object. ArchivedAt is the soft-delete timestamp (always nil for
// vendor rows). Version is a shared BIGSERIAL used only to pick the current
// vendor address (max version among userid-null rows).
type Address struct {
	ID          uuid.UUID
	FirstName   *string
	LastName    *string
	Street1     string
	Street2     string
	City        string
	PostalCode  string
	Country     string
	CompanyName *string
	UserID      *uuid.UUID
	ArchivedAt  *time.Time
	Version     int64
}

// IsVendor reports whether the row is a vendor address (userid IS NULL).
func (a Address) IsVendor() bool { return a.UserID == nil }

// Connection is a generic pagination result mirroring the GraphQL *Connection
// types: the page of nodes plus the total count and next-page flag.
type Connection[T any] struct {
	Nodes       []T
	TotalCount  int
	HasNextPage bool
}
