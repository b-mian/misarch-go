package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrNotFound is returned by the finder methods when no matching document
// exists (as opposed to a database/driver error). The resolver layer turns it
// into the appropriate GraphQL not-found error string.
var ErrNotFound = errors.New("not found")

// FindUser looks up a user by id (Rust query_object on the users collection).
// Returns ErrNotFound if the user does not exist.
func (s *Store) FindUser(ctx context.Context, id uuid.UUID) (userDoc, error) {
	var doc userDoc
	err := s.users.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return userDoc{}, ErrNotFound
	}
	if err != nil {
		return userDoc{}, err
	}
	return doc, nil
}

// FindUserCart looks up a user by id and returns their cart. Used by
// updateShoppingcart's final re-query (Rust query_shoppingcart).
func (s *Store) FindUserCart(ctx context.Context, id uuid.UUID) (Cart, error) {
	doc, err := s.FindUser(ctx, id)
	if err != nil {
		return Cart{}, err
	}
	return doc.toCart(), nil
}

// itemProjection matches the Rust query_shoppingcart_item_user projection: the
// positional-matched item plus last_updated_at and _id.
var itemProjection = bson.D{
	{Key: "shoppingcart.internal_shoppingcart_items.$", Value: 1},
	{Key: "shoppingcart.last_updated_at", Value: 1},
	{Key: "_id", Value: 1},
}

// FindItemOwner finds the user owning the cart item with the given id, using an
// $elemMatch on the item _id with a positional ($) projection so only the
// matched item is returned. It returns the owner's user id and the single
// projected item. Mirrors Rust query_shoppingcart_item_user +
// project_user_to_shopping_cart_item.
//
//   - No user matches      → ErrNotFound (caller: "ShoppingCartItem of UUID ... not found.")
//   - Matched but no item  → ErrProjectionFailed (should not happen with $)
func (s *Store) FindItemOwner(ctx context.Context, itemID uuid.UUID) (ownerID uuid.UUID, item CartItem, err error) {
	filter := bson.D{{Key: "shoppingcart.internal_shoppingcart_items", Value: bson.D{
		{Key: "$elemMatch", Value: bson.D{{Key: "_id", Value: itemID}}},
	}}}
	var doc userDoc
	e := s.users.FindOne(ctx, filter, options.FindOne().SetProjection(itemProjection)).Decode(&doc)
	if errors.Is(e, mongo.ErrNoDocuments) {
		return uuid.Nil, CartItem{}, ErrNotFound
	}
	if e != nil {
		return uuid.Nil, CartItem{}, e
	}
	if len(doc.Cart.Items) == 0 {
		return uuid.Nil, CartItem{}, ErrProjectionFailed
	}
	return doc.ID, doc.Cart.Items[0].toItem(), nil
}

// FindItem re-queries a single cart item by its id and returns it (Rust
// query_shoppingcart_item). Returns ErrNotFound / ErrProjectionFailed like
// FindItemOwner.
func (s *Store) FindItem(ctx context.Context, itemID uuid.UUID) (CartItem, error) {
	_, item, err := s.FindItemOwner(ctx, itemID)
	return item, err
}

// ErrProjectionFailed mirrors the Rust projection error where a matched user
// unexpectedly contains no cart item.
var ErrProjectionFailed = errors.New("projection failed")

// FindItemByVariant looks up an existing cart item in the given user's cart
// that references the given product variant, via $elemMatch on
// product_variant._id with a positional projection. Mirrors Rust
// query_shoppingcart_item_user_by_product_variant_id_and_user_id. Returns
// (item, true, nil) when found, (_, false, nil) when not found, and
// (_, false, err) on a driver error.
func (s *Store) FindItemByVariant(ctx context.Context, userID, variantID uuid.UUID) (CartItem, bool, error) {
	filter := bson.D{
		{Key: "_id", Value: userID},
		{Key: "shoppingcart.internal_shoppingcart_items", Value: bson.D{
			{Key: "$elemMatch", Value: bson.D{{Key: "product_variant._id", Value: variantID}}},
		}},
	}
	projection := bson.D{
		{Key: "shoppingcart.internal_shoppingcart_items.$", Value: 1},
		{Key: "_id", Value: 0},
	}
	var doc userDoc
	err := s.users.FindOne(ctx, filter, options.FindOne().SetProjection(projection)).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return CartItem{}, false, nil
	}
	if err != nil {
		return CartItem{}, false, err
	}
	if len(doc.Cart.Items) == 0 {
		// Matched the user but projection yielded no item: treat as not found,
		// matching the Rust path where the projection error leads to the
		// "create new item" branch.
		return CartItem{}, false, nil
	}
	return doc.Cart.Items[0].toItem(), true, nil
}

// ProductVariantsExist checks that every id in ids is present in the
// product_variants collection (Rust validate_shopping_cart_items). It returns
// the first requested id (in the given slice order) that is missing along with
// ok=false; ok=true when all are present. A driver error is returned as err.
func (s *Store) ProductVariantsExist(ctx context.Context, ids []uuid.UUID) (missing uuid.UUID, ok bool, err error) {
	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}}
	cur, err := s.productVariants.Find(ctx, filter)
	if err != nil {
		return uuid.Nil, false, err
	}
	var found []productVariantDoc
	if err := cur.All(ctx, &found); err != nil {
		return uuid.Nil, false, err
	}
	present := make(map[uuid.UUID]struct{}, len(found))
	for _, pv := range found {
		present[pv.ID] = struct{}{}
	}
	for _, id := range ids {
		if _, isPresent := present[id]; !isPresent {
			return id, false, nil
		}
	}
	return uuid.Nil, true, nil
}

// ProductVariantExists reports whether a single product variant id exists (Rust
// validate_shopping_cart_item). A driver error maps to (false, nil) so the
// caller emits the same "not present in the system" message the Rust service
// used for both the not-found and error cases.
func (s *Store) ProductVariantExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var doc productVariantDoc
	err := s.productVariants.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	if err != nil {
		// Rust treats a driver error identically to not-found here.
		return false, nil
	}
	return true, nil
}

// ReplaceCartItems overwrites the entire item set of the user's cart and bumps
// last_updated_at to ts (Rust update_shopping_cart_items $set). This is the
// only operation that touches last_updated_at.
func (s *Store) ReplaceCartItems(ctx context.Context, userID uuid.UUID, items []CartItem, ts time.Time) error {
	docs := make([]itemDoc, len(items))
	for i, it := range items {
		docs[i] = toItemDoc(it)
	}
	bts := primitive.NewDateTimeFromTime(ts.UTC())
	update := bson.D{{Key: "$set", Value: bson.D{
		{Key: "shoppingcart.internal_shoppingcart_items", Value: docs},
		{Key: "shoppingcart.last_updated_at", Value: bts},
	}}}
	_, err := s.users.UpdateOne(ctx, bson.D{{Key: "_id", Value: userID}}, update)
	return err
}

// PushItem appends a single item to the user's cart via $push. It does NOT
// bump last_updated_at (Rust add_shoppingcart_item_to_monogdb).
func (s *Store) PushItem(ctx context.Context, userID uuid.UUID, item CartItem) error {
	update := bson.D{{Key: "$push", Value: bson.D{
		{Key: "shoppingcart.internal_shoppingcart_items", Value: toItemDoc(item)},
	}}}
	_, err := s.users.UpdateOne(ctx, bson.D{{Key: "_id", Value: userID}}, update)
	return err
}

// UpdateItemCount sets the count of the cart item with the given id via the
// positional $ operator (Rust update_shoppingcart_item). It does not touch
// added_at or last_updated_at.
func (s *Store) UpdateItemCount(ctx context.Context, itemID uuid.UUID, count int) error {
	filter := bson.D{{Key: "shoppingcart.internal_shoppingcart_items._id", Value: itemID}}
	update := bson.D{{Key: "$set", Value: bson.D{
		{Key: "shoppingcart.internal_shoppingcart_items.$.count", Value: int32(count)},
	}}}
	_, err := s.users.UpdateOne(ctx, filter, update)
	return err
}

// PullItem removes the cart item with the given id via $pull (Rust
// delete_shoppingcart_item). It does not bump last_updated_at.
func (s *Store) PullItem(ctx context.Context, itemID uuid.UUID) error {
	filter := bson.D{{Key: "shoppingcart.internal_shoppingcart_items._id", Value: itemID}}
	update := bson.D{{Key: "$pull", Value: bson.D{
		{Key: "shoppingcart.internal_shoppingcart_items", Value: bson.D{{Key: "_id", Value: itemID}}},
	}}}
	_, err := s.users.UpdateOne(ctx, filter, update)
	return err
}

// PullItems removes all cart items whose id is in itemIDs from the given user's
// cart via $pull/$in (Rust delete_ordered_shoppingcart_items_in_mongodb, the
// order/order/created handler). Idempotent: a second delivery removes nothing
// and still succeeds. Does not bump last_updated_at.
func (s *Store) PullItems(ctx context.Context, userID uuid.UUID, itemIDs []uuid.UUID) error {
	filter := bson.D{{Key: "_id", Value: userID}}
	update := bson.D{{Key: "$pull", Value: bson.D{
		{Key: "shoppingcart.internal_shoppingcart_items", Value: bson.D{
			{Key: "_id", Value: bson.D{{Key: "$in", Value: itemIDs}}},
		}},
	}}}
	_, err := s.users.UpdateOne(ctx, filter, update)
	return err
}

// InsertUser creates a new user document with an empty cart (Rust
// add_user_to_mongodb, the user/user/created handler). Not idempotent: a
// duplicate _id returns a driver error (duplicate key) which the caller
// surfaces so Dapr redelivers — matching the original.
func (s *Store) InsertUser(ctx context.Context, id uuid.UUID, now time.Time) error {
	doc := userDoc{
		ID: id,
		Cart: cartDoc{
			LastUpdatedAt: now.UTC(),
			Items:         []itemDoc{},
		},
	}
	_, err := s.users.InsertOne(ctx, doc)
	return err
}

// InsertProductVariant creates a product variant existence document (Rust
// add_product_variant_to_mongodb, the catalog/product-variant/created handler).
// Not idempotent, same as InsertUser.
func (s *Store) InsertProductVariant(ctx context.Context, id uuid.UUID) error {
	_, err := s.productVariants.InsertOne(ctx, productVariantDoc{ID: id})
	return err
}

// toItemDoc maps a domain CartItem to its BSON persistence form.
func toItemDoc(it CartItem) itemDoc {
	return itemDoc{
		ID:             it.ID,
		Count:          int32(it.Count),
		AddedAt:        it.AddedAt.UTC(),
		ProductVariant: productVariantRef{ID: it.ProductVariantID},
	}
}
