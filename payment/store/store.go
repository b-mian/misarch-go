// Package store is the payment service persistence layer. It wraps the three
// MongoDB collections the original NestJS service used — payments,
// paymentinformations, openorders — behind typed query/mutation methods, and
// returns store-level structs that the graph layer maps to GraphQL models.
//
// Fidelity notes carried over from the TypeScript service:
//   - Payment._id and PaymentInformation._id are string UUIDs (Mongoose
//     overrides the default ObjectId), not bson.ObjectID.
//   - A Payment's paymentInformation ref is annotated ObjectId in the original
//     but is in practice a string UUID; we model it as a plain string FK and
//     join manually.
//   - PaymentInformation.user is stored as a subdocument { id: "<uuid>" }.
//   - Payment has timestamps (createdAt/updatedAt); PaymentInformation does not.
//   - Enum values (paymentMethod, status) are stored as their string names.
package store

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
)

// Collection names — must match the Mongoose model collection names exactly so
// data written by either implementation is mutually readable.
const (
	collPayments            = "payments"
	collPaymentInformations = "paymentinformations"
	collOpenOrders          = "openorders"
)

// Store provides persistence operations for payments, payment informations, and
// the short-lived open-order saga context.
type Store struct {
	db                 *mongo.Database
	payments           *mongo.Collection
	paymentInformation *mongo.Collection
	openOrders         *mongo.Collection
}

// New builds a Store backed by the given Mongo database and ensures the indexes
// the service relies on exist.
func New(ctx context.Context, db *mongo.Database) (*Store, error) {
	s := &Store{
		db:                 db,
		payments:           db.Collection(collPayments),
		paymentInformation: db.Collection(collPaymentInformations),
		openOrders:         db.Collection(collOpenOrders),
	}
	if err := s.ensureIndexes(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// PaymentDoc is the BSON document of the payments collection. bson tags match
// the original Mongoose schema field names exactly.
type PaymentDoc struct {
	ID                 string     `bson:"_id"`
	TotalAmount        int64      `bson:"totalAmount"`
	Status             string     `bson:"status"`
	PaymentInformation string     `bson:"paymentInformation"` // string FK to paymentinformations._id
	PayedAt            *time.Time `bson:"payedAt,omitempty"`
	NumberOfRetries    int        `bson:"numberOfRetries"`
	CreatedAt          time.Time  `bson:"createdAt"`
	UpdatedAt          time.Time  `bson:"updatedAt"`
}

// UserRef mirrors the { id } subdocument the original stores under
// PaymentInformation.user.
type UserRef struct {
	ID string `bson:"id" json:"id"`
}

// PaymentInformationDoc is the BSON document of the paymentinformations
// collection. secretMethodDetails is persisted but never surfaced through
// GraphQL (the original used @HideField()).
type PaymentInformationDoc struct {
	ID                  string         `bson:"_id"`
	PaymentMethod       string         `bson:"paymentMethod"`
	PublicMethodDetails map[string]any `bson:"publicMethodDetails,omitempty"`
	SecretMethodDetails map[string]any `bson:"secretMethodDetails,omitempty"`
	User                UserRef        `bson:"user"`
}

// OpenOrderDoc is the BSON document of the openorders collection. The order is
// stored verbatim (as it was received) so later saga events can re-publish the
// exact same JSON without re-querying the order service.
type OpenOrderDoc struct {
	PaymentID string `bson:"paymentId"`
	Order     any    `bson:"order"`
}

// Connection is a generic pagination result mirroring the GraphQL *Connection
// types: the page of nodes plus the total count and next-page flag.
type Connection[T any] struct {
	Nodes       []T
	TotalCount  int
	HasNextPage bool
}
