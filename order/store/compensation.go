package store

import (
	"context"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

// ValidateObjectOrder reports whether an order with the given id exists (used by
// compensate_order's initial validate_object). Missing → mongo.ErrNoDocuments.
func (s *Store) ValidateObjectOrder(ctx context.Context, id uuid.UUID) error {
	return s.orders.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Err()
}

// CountUncompensatedProbe runs the original verify_items_uncompensated query
// VERBATIM and returns the number of matching order_compensations documents:
//
//	{ order_item_ids: { $not: { $elemMatch: { $in: orderItemIDs } } } }
//
// The caller treats a non-zero count as "could not verify" → error (blocking
// compensation). This query is (apparently) inverted — it matches compensation
// docs whose order_item_ids do NOT contain any incoming id, so as soon as ANY
// compensation for another order exists the count is ≥1 and compensation is
// blocked. Reproduced EXACTLY; do not "fix" the logic.
func (s *Store) CountUncompensatedProbe(ctx context.Context, orderItemIDs []uuid.UUID) (int64, error) {
	query := bson.D{{Key: "order_item_ids", Value: bson.D{
		{Key: "$not", Value: bson.D{
			{Key: "$elemMatch", Value: bson.D{
				{Key: "$in", Value: orderItemIDs},
			}},
		}},
	}}}
	cur, err := s.orderCompensations.Find(ctx, query)
	if err != nil {
		return 0, err
	}
	defer cur.Close(ctx)
	var docs []OrderCompensation
	if err := cur.All(ctx, &docs); err != nil {
		return 0, err
	}
	return int64(len(docs)), nil
}

// InsertOrderCompensation inserts a compensation log document. Mirrors
// insert_order_compensation_in_mongodb (a driver error surfaces "Adding order
// compensation failed in MongoDB.").
func (s *Store) InsertOrderCompensation(ctx context.Context, c OrderCompensation) error {
	_, err := s.orderCompensations.InsertOne(ctx, c)
	return err
}
