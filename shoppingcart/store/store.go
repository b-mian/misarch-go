// Package store is the shoppingcart service persistence layer. It owns the
// MongoDB collections and exposes typed query/mutation methods that mirror the
// original Rust service's filters and updates exactly (same BSON keys, same
// UUID binary subtype). Documents are returned as store-level domain structs;
// the graph layer maps them to GraphQL models.
//
// The shopping cart is not a standalone entity: it is embedded in the user
// document under the "shoppingcart" key, and the user's UUID is the cart's
// identity. The two collections are:
//
//	users             — one doc per user; the embedded cart lives here.
//	product_variants  — one doc per known product variant id (existence mirror).
//
// Both are created solely from Dapr events; mutations never create users.
package store

import (
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Collection names (Rust: db.collection::<T>("users") / ("product_variants")).
const (
	usersCollection           = "users"
	productVariantsCollection = "product_variants"
)

// Store provides persistence operations over the users and product_variants
// collections.
type Store struct {
	users           *mongo.Collection
	productVariants *mongo.Collection
}

// New builds a Store backed by the given Mongo database. It applies the
// subtype-4 UUID codec registry so uuid.UUID fields (every "_id") marshal and
// match the documents written by this service and the original Rust service.
func New(db *mongo.Database) *Store {
	reg := newRegistry()
	collOpts := options.Collection().SetRegistry(reg)
	return &Store{
		users:           db.Collection(usersCollection, collOpts),
		productVariants: db.Collection(productVariantsCollection, collOpts),
	}
}

// Cart is a user's shopping cart: the last-updated timestamp plus its items.
// It has no id of its own — the owning user's id identifies it.
type Cart struct {
	LastUpdatedAt time.Time
	Items         []CartItem
}

// CartItem is a single line item in a cart.
type CartItem struct {
	ID               uuid.UUID
	Count            int
	AddedAt          time.Time
	ProductVariantID uuid.UUID
}

// -- BSON persistence structs (exact on-disk field names; see spec §5). --

// userDoc is a document in the users collection. The embedded key is
// "shoppingcart" (one lowercase word).
type userDoc struct {
	ID   uuid.UUID `bson:"_id"`
	Cart cartDoc   `bson:"shoppingcart"`
}

// cartDoc is the embedded cart. Item array key is "internal_shoppingcart_items"
// (exposed to GraphQL as shoppingcartItems, but stored under the internal name).
type cartDoc struct {
	LastUpdatedAt time.Time `bson:"last_updated_at"`
	Items         []itemDoc `bson:"internal_shoppingcart_items"`
}

// itemDoc is one embedded cart item. Timestamps are last_updated_at / added_at;
// the product variant is a nested doc keyed product_variant with its own _id.
type itemDoc struct {
	ID             uuid.UUID         `bson:"_id"`
	Count          int32             `bson:"count"`
	AddedAt        time.Time         `bson:"added_at"`
	ProductVariant productVariantRef `bson:"product_variant"`
}

// productVariantRef is the nested { "_id": <uuid> } reference on a cart item.
type productVariantRef struct {
	ID uuid.UUID `bson:"_id"`
}

// productVariantDoc is a document in the product_variants collection: only the
// id, used purely for existence validation.
type productVariantDoc struct {
	ID uuid.UUID `bson:"_id"`
}

// toCart maps a persistence userDoc's embedded cart to the domain Cart,
// converting timestamps to UTC.
func (u userDoc) toCart() Cart {
	items := make([]CartItem, len(u.Cart.Items))
	for i, it := range u.Cart.Items {
		items[i] = it.toItem()
	}
	return Cart{
		LastUpdatedAt: u.Cart.LastUpdatedAt.UTC(),
		Items:         items,
	}
}

// toItem maps a persistence itemDoc to the domain CartItem, converting the
// timestamp to UTC.
func (it itemDoc) toItem() CartItem {
	return CartItem{
		ID:               it.ID,
		Count:            int(it.Count),
		AddedAt:          it.AddedAt.UTC(),
		ProductVariantID: it.ProductVariant.ID,
	}
}
