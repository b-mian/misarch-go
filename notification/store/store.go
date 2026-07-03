// Package store is the notification service persistence layer. It owns the
// embedded Postgres migrations and exposes typed, parameterized query/mutation
// methods over a pgx pool. Rows are returned as store-level structs; the graph
// layer maps them to GraphQL models (including the relationship extraFields).
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

// Store provides persistence operations for notifications and the local user
// id replica.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Store backed by the given pgx pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Notification is a row of the notificationentity table. DateRead is nil when
// the notification is unread; the GraphQL isRead field is derived from it.
type Notification struct {
	ID       uuid.UUID
	Title    string
	Body     string
	DateSent time.Time
	DateRead *time.Time
	UserID   uuid.UUID
}

// Connection is a generic pagination result mirroring the GraphQL *Connection
// type: the page of nodes plus the total count and next-page flag. The
// notification connection resolves each field lazily, so this is assembled per
// selected field rather than eagerly.
type Connection[T any] struct {
	Nodes       []T
	TotalCount  int
	HasNextPage bool
}
