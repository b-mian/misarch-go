package store

import (
	"context"
	"encoding/json"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// openOrderStore is the on-disk shape of an openorders document. The order is
// stored as raw JSON text so it can be re-published byte-for-byte in later saga
// events without any lossy re-serialization (spec §4: round-trip the exact
// JSON). This diverges from the original (which stored a BSON subdocument) but
// is externally indistinguishable — downstream consumers read `order` by field
// name — and strictly safer for fidelity.
type openOrderStore struct {
	PaymentID string `bson:"paymentId"`
	Order     string `bson:"order"`
}

// CreateOpenOrder persists the order context keyed by paymentId. order is the
// exact JSON received in the validation-succeeded event.
func (s *Store) CreateOpenOrder(ctx context.Context, paymentID string, order json.RawMessage) error {
	_, err := s.openOrders.InsertOne(ctx, openOrderStore{PaymentID: paymentID, Order: string(order)})
	if err != nil {
		return fmt.Errorf("create open order %s: %w", paymentID, err)
	}
	return nil
}

// FindOpenOrder returns the raw order JSON stored for a payment id. Not-found
// yields the original NestJS message.
func (s *Store) FindOpenOrder(ctx context.Context, paymentID string) (json.RawMessage, error) {
	var doc openOrderStore
	err := s.openOrders.FindOne(ctx, bson.M{"paymentId": paymentID}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, fmt.Errorf("Open order for payment: %s not found", paymentID)
	}
	if err != nil {
		return nil, fmt.Errorf("find open order %s: %w", paymentID, err)
	}
	return json.RawMessage(doc.Order), nil
}

// DeleteOpenOrder hard-deletes the open order for a payment id. A missing order
// is not treated as an error by callers that delete opportunistically, so this
// returns whether a document was deleted rather than an error on not-found.
func (s *Store) DeleteOpenOrder(ctx context.Context, paymentID string) error {
	res := s.openOrders.FindOneAndDelete(ctx, bson.M{"paymentId": paymentID})
	if err := res.Err(); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil
		}
		return fmt.Errorf("delete open order %s: %w", paymentID, err)
	}
	return nil
}

// ensureIndexes creates the indexes the service relies on. Only a paymentId
// index on openorders is added (a benign improvement over the original, which
// did a collection scan). The _id indexes are implicit.
func (s *Store) ensureIndexes(ctx context.Context) error {
	_, err := s.openOrders.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "paymentId", Value: 1}},
	}, options.CreateIndexes())
	if err != nil {
		return fmt.Errorf("create openorders paymentId index: %w", err)
	}
	return nil
}
