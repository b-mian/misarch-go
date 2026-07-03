// Package store is the tax service persistence layer. It owns the embedded
// Postgres migrations and exposes typed, parameterized query/mutation methods
// over a pgx pool. Rows are returned as store-level structs; the graph layer
// maps them to GraphQL models (including the relationship extraFields).
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

// Store provides persistence operations for tax rates and their versions.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Store backed by the given pgx pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// TaxRate is a row of the taxrateentity table. CurrentVersionID is nullable
// only during the brief window between the two INSERTs of createTaxRate; every
// tax rate observable through the API always has a current version, matching
// the original service's non-null TaxRate.currentVersion field.
type TaxRate struct {
	ID               uuid.UUID
	Name             string
	Description      string
	CurrentVersionID uuid.UUID
}

// TaxRateVersion is a row of the taxrateversionentity table.
type TaxRateVersion struct {
	ID        uuid.UUID
	Rate      float64
	Version   int
	CreatedAt time.Time
	TaxRateID uuid.UUID
}

// Connection is a generic pagination result mirroring the GraphQL *Connection
// types: the page of nodes plus the total count and next-page flag.
type Connection[T any] struct {
	Nodes       []T
	TotalCount  int
	HasNextPage bool
}
