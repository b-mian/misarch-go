package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readconcern"
)

// ErrNotEnoughItems signals an insufficient-stock reservation. The message is
// formatted by the caller so it can inject the productVariantId.
var ErrNotEnoughItems = fmt.Errorf("not enough product items")

// FindByID returns the item with the given _id, or (nil, nil) if it does not
// exist (the caller decides whether that is an error).
func (s *Store) FindByID(ctx context.Context, id string) (*ProductItem, error) {
	var item ProductItem
	err := s.productItems.FindOne(ctx, bson.M{"_id": id}).Decode(&item)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find product item: %w", err)
	}
	return &item, nil
}

// nodesFilter builds the Mongo filter used for the `nodes` query. It reproduces
// the original buildQuery [BUG-COMPAT nodes-filter]: `productVariant` is mapped
// to the nonexistent key `status` and `inventoryStatus` to `createdAt`, so any
// non-empty filter yields no documents while an empty filter matches all.
func nodesFilter(f *Filter) bson.M {
	q := bson.M{}
	if f == nil {
		return q
	}
	if f.ProductVariant != nil {
		q["status"] = *f.ProductVariant
	}
	if f.InventoryStatus != nil {
		q["createdAt"] = *f.InventoryStatus
	}
	return q
}

// countFilter builds the Mongo filter used for totalCount. It uses the CORRECT
// document field names, so counts honor the filter even where nodes do not
// (spec §3.3).
func countFilter(f *Filter) bson.M {
	q := bson.M{}
	if f == nil {
		return q
	}
	if f.ProductVariant != nil {
		q["productVariant"] = *f.ProductVariant
	}
	if f.InventoryStatus != nil {
		q["inventoryStatus"] = *f.InventoryStatus
	}
	return q
}

// FindNodes returns the paginated, sorted page of items for the connection's
// `nodes` field. sortAsc chooses direction on the `_id` sort key.
func (s *Store) FindNodes(ctx context.Context, f *Filter, skip, first int, sortAsc bool) ([]ProductItem, error) {
	dir := 1
	if !sortAsc {
		dir = -1
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: dir}}).
		SetLimit(int64(first)).
		SetSkip(int64(skip))

	cur, err := s.productItems.Find(ctx, nodesFilter(f), opts)
	if err != nil {
		return nil, fmt.Errorf("find product items: %w", err)
	}
	defer cur.Close(ctx)

	items := []ProductItem{}
	if err := cur.All(ctx, &items); err != nil {
		return nil, fmt.Errorf("decode product items: %w", err)
	}
	return items, nil
}

// Count returns countDocuments for the correctly-translated filter.
func (s *Store) Count(ctx context.Context, f *Filter) (int64, error) {
	n, err := s.productItems.CountDocuments(ctx, countFilter(f))
	if err != nil {
		return 0, fmt.Errorf("count product items: %w", err)
	}
	return n, nil
}

// CreateBatch inserts `number` new product items for the given variant in a
// single transaction and returns them in insertion order. number <= 0 inserts
// nothing and returns an empty slice (no validation, matching the original).
func (s *Store) CreateBatch(ctx context.Context, productVariantID string, number int) ([]ProductItem, error) {
	items := []ProductItem{}
	if number <= 0 {
		return items, nil
	}
	for i := 0; i < number; i++ {
		items = append(items, ProductItem{
			ID:              uuid.NewString(),
			ProductVariant:  productVariantID,
			InventoryStatus: StatusInStorage,
		})
	}

	err := s.withTransaction(ctx, nil, func(sc mongo.SessionContext) error {
		docs := make([]any, len(items))
		for i := range items {
			docs[i] = items[i]
		}
		_, err := s.productItems.InsertMany(sc, docs)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create product item batch: %w", err)
	}
	return items, nil
}

// Update sets productVariant and inventoryStatus on the item and returns the
// post-update document. Missing item → (nil, nil). Per [BUG-COMPAT
// update-not-overwrite] only these two fields are $set; orderId is preserved.
func (s *Store) Update(ctx context.Context, id, productVariantID, inventoryStatus string) (*ProductItem, error) {
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	update := bson.M{"$set": bson.M{
		"productVariant":  productVariantID,
		"inventoryStatus": inventoryStatus,
	}}
	var item ProductItem
	err := s.productItems.FindOneAndUpdate(ctx, bson.M{"_id": id}, update, opts).Decode(&item)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update product item: %w", err)
	}
	return &item, nil
}

// Delete removes the item by _id and returns the deleted document, or
// (nil, nil) if it did not exist.
func (s *Store) Delete(ctx context.Context, id string) (*ProductItem, error) {
	var item ProductItem
	err := s.productItems.FindOneAndDelete(ctx, bson.M{"_id": id}).Decode(&item)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("delete product item: %w", err)
	}
	return &item, nil
}

// ReserveBatch reserves up to `number` IN_STORAGE items of the given variant
// for orderId, in a snapshot-isolation transaction. Fewer than `number`
// available → ErrNotEnoughItems (transaction aborted).
//
// Per [BUG-COMPAT reserve-returns-empty] the original re-read the reserved
// items with a malformed query that matched nothing, so a successful
// reservation returns an EMPTY slice while the reservation itself is persisted.
// This reproduces that: it commits the reservation and returns no items.
func (s *Store) ReserveBatch(ctx context.Context, productVariantID string, number int, orderID string) ([]ProductItem, error) {
	txnOpts := options.Transaction().SetReadConcern(readconcern.Snapshot())
	err := s.withTransaction(ctx, txnOpts, func(sc mongo.SessionContext) error {
		findOpts := options.Find().SetLimit(int64(number))
		cur, err := s.productItems.Find(sc, bson.M{
			"productVariant":  productVariantID,
			"inventoryStatus": StatusInStorage,
		}, findOpts)
		if err != nil {
			return err
		}
		var toReserve []ProductItem
		if err := cur.All(sc, &toReserve); err != nil {
			return err
		}
		if len(toReserve) < number {
			return ErrNotEnoughItems
		}
		ids := make([]string, len(toReserve))
		for i, it := range toReserve {
			ids[i] = it.ID
		}
		_, err = s.productItems.UpdateMany(sc,
			bson.M{"_id": bson.M{"$in": ids}},
			bson.M{"$set": bson.M{"inventoryStatus": StatusReserved, "orderId": orderID}},
		)
		return err
	})
	if err != nil {
		return nil, err
	}
	// Empty return reproduces the original's malformed re-read.
	return []ProductItem{}, nil
}

// UpdateOrderStatus sets inventoryStatus on every item carrying orderId,
// unconditionally (0 matches is fine). Mirrors updateOrderProductItemsStatus.
func (s *Store) UpdateOrderStatus(ctx context.Context, orderID, status string) error {
	_, err := s.productItems.UpdateMany(ctx,
		bson.M{"orderId": orderID},
		bson.M{"$set": bson.M{"inventoryStatus": status}},
	)
	if err != nil {
		return fmt.Errorf("update order product items status: %w", err)
	}
	return nil
}

// ReleaseBatch releases every item reserved for orderId back to IN_STORAGE with
// orderId cleared to null, and returns the released items.
//
// Per [BUG-COMPAT release-upsert] (FIXED here): the original passed
// {upsert:true}, so when no items matched Mongo inserted a garbage orphan
// document with no productVariant field. This port skips the update entirely
// when nothing matches, avoiding the orphan. Recorded as an intentional
// deviation.
func (s *Store) ReleaseBatch(ctx context.Context, orderID string) ([]ProductItem, error) {
	cur, err := s.productItems.Find(ctx, bson.M{"orderId": orderID})
	if err != nil {
		return nil, fmt.Errorf("find reserved product items: %w", err)
	}
	var items []ProductItem
	if err := cur.All(ctx, &items); err != nil {
		return nil, fmt.Errorf("decode reserved product items: %w", err)
	}

	if len(items) == 0 {
		// No items reserved for this order: skip the update (no upsert orphan).
		return []ProductItem{}, nil
	}

	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	_, err = s.productItems.UpdateMany(ctx,
		bson.M{"_id": bson.M{"$in": ids}},
		bson.M{"$set": bson.M{"inventoryStatus": StatusInStorage, "orderId": nil}},
	)
	if err != nil {
		return nil, fmt.Errorf("release product items: %w", err)
	}

	// Re-read the released items to return their post-update state.
	relCur, err := s.productItems.Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return nil, fmt.Errorf("re-read released product items: %w", err)
	}
	released := []ProductItem{}
	if err := relCur.All(ctx, &released); err != nil {
		return nil, fmt.Errorf("decode released product items: %w", err)
	}
	return released, nil
}

// withTransaction runs fn inside a Mongo transaction on a fresh session,
// committing on success and aborting on error (matching the original's
// startTransaction/commit/abort structure).
func (s *Store) withTransaction(ctx context.Context, txnOpts *options.TransactionOptions, fn func(mongo.SessionContext) error) error {
	sess, err := s.client.StartSession()
	if err != nil {
		return fmt.Errorf("start session: %w", err)
	}
	defer sess.EndSession(ctx)

	_, err = sess.WithTransaction(ctx, func(sc mongo.SessionContext) (any, error) {
		return nil, fn(sc)
	}, txnOpts)
	return err
}
