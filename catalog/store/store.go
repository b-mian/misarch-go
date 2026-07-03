// Package store is the catalog service persistence layer. It owns the embedded
// Postgres migrations and exposes typed, parameterized query/mutation methods
// over a pgx pool. Rows are returned as store-level structs; the graph layer
// maps them to GraphQL models (resolving polymorphic types from the
// discriminator column and populating relationship extraFields).
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

// Discriminator values distinguishing categorical from numerical
// characteristics and characteristic values (stored verbatim in the
// discriminator VARCHAR column).
const (
	DiscriminatorCategorical = "CATEGORICAL"
	DiscriminatorNumerical   = "NUMERICAL"
)

// Store provides persistence operations for the catalog bounded contexts.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Store backed by the given pgx pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Product is a row of the productentity table. DefaultVariantID is nullable
// only during the brief window mid-creation before the variant is back-patched;
// every product observable through the API always has a default variant.
type Product struct {
	ID                uuid.UUID
	InternalName      string
	IsPubliclyVisible bool
	DefaultVariantID  uuid.UUID
}

// ProductVariant is a row of the productvariantentity table.
type ProductVariant struct {
	ID                uuid.UUID
	IsPubliclyVisible bool
	ProductID         uuid.UUID
	CurrentVersion    uuid.UUID
}

// ProductVariantVersion is a row of the productvariantversionentity table.
type ProductVariantVersion struct {
	ID                   uuid.UUID
	Name                 string
	Description          string
	Version              int
	RetailPrice          int
	CreatedAt            time.Time
	CanBeReturnedForDays *int
	ProductVariantID     uuid.UUID
	TaxRateID            uuid.UUID
	Weight               float64
}

// Category is a row of the categoryentity table.
type Category struct {
	ID          uuid.UUID
	Name        string
	Description string
}

// CategoryCharacteristic is a row of the categorycharacteristicentity table.
// Discriminator selects the concrete GraphQL type; Unit is populated only for
// numerical characteristics.
type CategoryCharacteristic struct {
	ID            uuid.UUID
	Discriminator string
	Name          string
	Description   string
	Unit          *string
	CategoryID    uuid.UUID
}

// CategoryCharacteristicValue is a row of the categorycharacteristicvalueentity
// table. Discriminator selects the concrete GraphQL type; exactly one of
// StringValue / DoubleValue is populated.
type CategoryCharacteristicValue struct {
	ID                       uuid.UUID
	Discriminator            string
	StringValue              *string
	DoubleValue              *float64
	CategoryCharacteristicID uuid.UUID
	ProductVariantVersionID  uuid.UUID
}

// TaxRate is a row of the taxrateentity table (an id-only mirror of the tax
// service's entity, populated from Dapr events).
type TaxRate struct {
	ID uuid.UUID
}

// Media is a row of the mediaentity table (an id-only mirror of the media
// service's entity, populated from Dapr events).
type Media struct {
	ID uuid.UUID
}

// CategoricalValue is one DISTINCT string value of a categorical
// characteristic (the projection backing CategoricalCategoryCharacteristic.values).
type CategoricalValue struct {
	CharacteristicID uuid.UUID
	Value            string
}

// Connection is a generic pagination result mirroring the GraphQL *Connection
// types: the page of nodes plus the total count and next-page flag.
type Connection[T any] struct {
	Nodes       []T
	TotalCount  int
	HasNextPage bool
}

// OrderColumns is a validated ORDER BY column list for a connection. The graph
// layer maps the GraphQL order enum to one of these; a secondary `id` column is
// appended to non-unique fields (NAME, INTERNAL_NAME, VERSION, CREATED_AT),
// exactly as the original *OrderField enums did, and the chosen direction
// applies to every column in the list.
type OrderColumns []string

// Order-column sets, one variable per SDL order-enum value, using the physical
// (lowercase) column names. Where a join is present the ID tiebreaker is
// qualified to the base entity table to stay unambiguous. These reproduce the
// Kotlin *OrderField.expressions arrays column-for-column.
var (
	// Product (productentity) order.
	ProductOrderByID           = OrderColumns{"productentity.id"}
	ProductOrderByInternalName = OrderColumns{"internalname", "productentity.id"}

	// Category (categoryentity) order.
	CategoryOrderByID   = OrderColumns{"categoryentity.id"}
	CategoryOrderByName = OrderColumns{"name", "categoryentity.id"}

	// ProductVariant (productvariantentity) order — id only.
	ProductVariantOrderByID = OrderColumns{"id"}

	// ProductVariantVersion (productvariantversionentity) order.
	ProductVariantVersionOrderByID        = OrderColumns{"id"}
	ProductVariantVersionOrderByVersion   = OrderColumns{"version", "id"}
	ProductVariantVersionOrderByCreatedAt = OrderColumns{"createdat", "id"}

	// CategoryCharacteristic (categorycharacteristicentity) order — id only.
	CategoryCharacteristicOrderByID = OrderColumns{"id"}

	// CategoryCharacteristicValue (categorycharacteristicvalueentity) order — id only.
	CategoryCharacteristicValueOrderByID = OrderColumns{"id"}

	// CategoricalCategoryCharacteristic.values distinct-string order — stringvalue only.
	CategoricalValueOrderByValue = OrderColumns{"stringvalue"}

	// Media (mediaentity) order — id only (CommonOrder).
	MediaOrderByID = OrderColumns{"mediaentity.id"}
)
