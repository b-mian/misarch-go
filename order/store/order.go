package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// GetOrder loads an order by its _id. Missing → mongo.ErrNoDocuments. Mirrors
// the Rust query_object::<Order> find_one({_id: id}).
func (s *Store) GetOrder(ctx context.Context, id uuid.UUID) (Order, error) {
	var o Order
	if err := s.orders.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&o); err != nil {
		return Order{}, err
	}
	return o, nil
}

// GetUser loads a user by _id. Missing → mongo.ErrNoDocuments. Mirrors
// query_object::<User> find_one({_id: id}).
func (s *Store) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	var u User
	if err := s.users.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&u); err != nil {
		return User{}, err
	}
	return u, nil
}

// GetOrderItemFromCollection loads an order item from the (never-written)
// order_items collection. In practice this always returns mongo.ErrNoDocuments
// because no code path inserts into order_items — order items live embedded in
// orders.internal_order_items. Reproduced verbatim: the original orderItem
// query / OrderItem entity resolver read this collection and thus 404.
func (s *Store) GetOrderItemFromCollection(ctx context.Context, id uuid.UUID) (OrderItem, error) {
	var oi OrderItem
	if err := s.orderItems.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&oi); err != nil {
		return OrderItem{}, err
	}
	return oi, nil
}

// GetOrderByEmbeddedItemID finds the order whose internal_order_items array
// contains an item with the given _id, and returns it. Used by the orderItem
// query for its authorization user lookup. Missing → mongo.ErrNoDocuments.
// Mirrors query_user_from_order_item_id find_one({internal_order_items._id: id}).
func (s *Store) GetOrderByEmbeddedItemID(ctx context.Context, id uuid.UUID) (Order, error) {
	var o Order
	err := s.orders.FindOne(ctx, bson.D{{Key: "internal_order_items._id", Value: id}}).Decode(&o)
	if err != nil {
		return Order{}, err
	}
	return o, nil
}

// ValidateObjectUser reports whether a user with the given id exists. Used by
// createOrder validation (validate_object::<User>). Missing →
// mongo.ErrNoDocuments.
func (s *Store) ValidateObjectUser(ctx context.Context, id uuid.UUID) error {
	return s.users.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Err()
}

// presentIDs runs find({_id: {$in: ids}}) on the given collection and returns
// the set of _id values found. Used by validate_objects for shipment methods
// and coupons.
func presentIDs(ctx context.Context, coll *mongo.Collection, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	cur, err := coll.Find(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	found := make(map[uuid.UUID]struct{})
	for cur.Next(ctx) {
		var ref UUIDRef
		if err := cur.Decode(&ref); err != nil {
			return nil, err
		}
		found[ref.ID] = struct{}{}
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}
	return found, nil
}

// ShipmentMethodsPresent returns the subset of the given ids present in the
// shipment_methods collection.
func (s *Store) ShipmentMethodsPresent(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	return presentIDs(ctx, s.shipmentMethods, ids)
}

// CouponsPresent returns the subset of the given ids present in the coupons
// collection.
func (s *Store) CouponsPresent(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	return presentIDs(ctx, s.coupons, ids)
}

// GetProductVariants loads product variants by id ($in), returning a map keyed
// by _id. Visibility filtering is applied by the caller (createOrder drops
// non-visible variants). Mirrors query_objects::<ProductVariant>.
func (s *Store) GetProductVariants(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]ProductVariant, error) {
	cur, err := s.productVariants.Find(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make(map[uuid.UUID]ProductVariant)
	for cur.Next(ctx) {
		var pv ProductVariant
		if err := cur.Decode(&pv); err != nil {
			return nil, err
		}
		out[pv.ID] = pv
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetTaxRates loads tax rates by id ($in), returning a map keyed by _id.
// Mirrors query_objects::<TaxRate>.
func (s *Store) GetTaxRates(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]TaxRate, error) {
	cur, err := s.taxRates.Find(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make(map[uuid.UUID]TaxRate)
	for cur.Next(ctx) {
		var tr TaxRate
		if err := cur.Decode(&tr); err != nil {
			return nil, err
		}
		out[tr.ID] = tr
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// InsertOrder inserts the order document and re-reads it by the inserted id,
// returning the re-read order. Mirrors insert_order_in_mongodb. A driver error
// is returned so the resolver can surface "Adding order failed in MongoDB.".
func (s *Store) InsertOrder(ctx context.Context, o Order) (Order, error) {
	if _, err := s.orders.InsertOne(ctx, o); err != nil {
		return Order{}, err
	}
	return s.GetOrder(ctx, o.ID)
}

// SetStatusPlaced sets order_status=PLACED and placed_at=ts for the given id.
// Mirrors set_status_placed_in_mongodb.
func (s *Store) SetStatusPlaced(ctx context.Context, id uuid.UUID, ts time.Time) error {
	_, err := s.orders.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: id}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "order_status", Value: "PLACED"},
			{Key: "placed_at", Value: ts},
		}}},
	)
	return err
}

// SetStatusRejected sets order_status=REJECTED for the given id (no placed_at,
// no rejection_reason). Mirrors set_status_rejected_in_mongodb (which always
// returns an error to the caller regardless of the update result).
func (s *Store) SetStatusRejected(ctx context.Context, id uuid.UUID) error {
	_, err := s.orders.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: id}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "order_status", Value: "REJECTED"}}}},
	)
	return err
}

// OrdersPage is one page of a user's orders plus its total count.
type OrdersPage struct {
	Nodes       []Order
	TotalCount  int64
	HasNextPage bool
}

// ListOrdersByUser paginates the orders of a user (DB-backed), reproducing the
// mongodb-cursor-pagination skip/limit behavior of User.orders:
//   - totalCount = count of ALL docs matching {user._id: userID} (ignoring
//     skip/limit);
//   - nodes = docs after applying sort, skip, limit;
//   - hasNextPage = skip + len(nodes) < totalCount.
//
// sortField is the raw Mongo field name (per OrderOrderField, including the
// dead `name` / `last_updated_at` fields, which sort on a missing field →
// no-op). direction is 1 (ASC) or -1 (DESC). A driver error → the resolver
// surfaces "Retrieving orders failed in MongoDB.".
func (s *Store) ListOrdersByUser(ctx context.Context, userID uuid.UUID, sortField string, direction int, skip *int64, first *int64) (OrdersPage, error) {
	filter := bson.D{{Key: "user._id", Value: userID}}

	total, err := s.orders.CountDocuments(ctx, filter)
	if err != nil {
		return OrdersPage{}, err
	}

	findOpts := options.Find().SetSort(bson.D{{Key: sortField, Value: direction}})
	if skip != nil {
		findOpts.SetSkip(*skip)
	}
	if first != nil {
		findOpts.SetLimit(*first)
	}

	cur, err := s.orders.Find(ctx, filter, findOpts)
	if err != nil {
		return OrdersPage{}, err
	}
	defer cur.Close(ctx)

	var nodes []Order
	if err := cur.All(ctx, &nodes); err != nil {
		return OrdersPage{}, err
	}

	skipped := int64(0)
	if skip != nil {
		skipped = *skip
	}
	hasNext := skipped+int64(len(nodes)) < total

	return OrdersPage{Nodes: nodes, TotalCount: total, HasNextPage: hasNext}, nil
}
