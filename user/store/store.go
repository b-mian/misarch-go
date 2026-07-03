// Package store is the user service persistence layer. It owns the embedded
// Postgres migrations and exposes typed, parameterized query/mutation methods
// over a pgx pool. Rows are returned as store-level structs; the graph layer
// maps them to GraphQL models.
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

// Store provides persistence operations for users.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Store backed by the given pgx pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// User is a row of the userentity table.
//
// Gender is the enum name stored as free-form text (VARCHAR(255)); it is
// nullable, hence a pointer. Birthday is stored in a TIMESTAMPTZ column even
// though the GraphQL type is a calendar Date — the graph mapper truncates it to
// a date (and the update path writes it at UTC midnight) so it round-trips.
type User struct {
	ID         uuid.UUID
	Username   string
	FirstName  string
	LastName   string
	Gender     *string
	Birthday   *time.Time
	DateJoined time.Time
}

// Connection is a generic pagination result mirroring the GraphQL *Connection
// types: the page of nodes plus the total count and next-page flag.
type Connection[T any] struct {
	Nodes       []T
	TotalCount  int
	HasNextPage bool
}
