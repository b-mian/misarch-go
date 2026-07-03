// Package store is the wishlist service persistence layer. It owns the MongoDB
// collections (wishlists, users, product_variants), the BSON document shapes,
// and the UUID<->Binary(subtype 4) codec that keeps ids interoperable with the
// sibling services and the Dapr event payloads. Documents are returned as
// store-level structs; the graph layer maps them to GraphQL models.
package store

import (
	"fmt"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsontype"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// Collection names (identical to the original Rust service).
const (
	wishlistsCollection       = "wishlists"
	usersCollection           = "users"
	productVariantsCollection = "product_variants"
)

// Store provides persistence operations for wishlists and the user /
// product-variant shadow collections.
type Store struct {
	db        *mongo.Database
	wishlists *mongo.Collection
	users     *mongo.Collection
	variants  *mongo.Collection
}

// New builds a Store backed by the given Mongo database.
func New(db *mongo.Database) *Store {
	return &Store{
		db:        db,
		wishlists: db.Collection(wishlistsCollection),
		users:     db.Collection(usersCollection),
		variants:  db.Collection(productVariantsCollection),
	}
}

// binUUID is a uuid.UUID that always serializes to / from a BSON Binary of
// subtype 4 (the canonical UUID subtype). Every `_id` in this service is stored
// this way so documents match those written by sibling services and by the
// Dapr event handlers; a wrong subtype would silently fail to match on reads.
type binUUID uuid.UUID

// UUID returns the wrapped uuid.UUID.
func (b binUUID) UUID() uuid.UUID { return uuid.UUID(b) }

// MarshalBSONValue encodes the UUID as a BSON Binary subtype 4.
func (b binUUID) MarshalBSONValue() (bsontype.Type, []byte, error) {
	bin := primitive.Binary{Subtype: bsontype.BinaryUUID, Data: append([]byte(nil), b[:]...)}
	return bson.MarshalValue(bin)
}

// UnmarshalBSONValue decodes a BSON Binary (any UUID subtype) back into the
// UUID. It accepts subtype 0x04 and the legacy 0x03 form; anything else errors.
func (b *binUUID) UnmarshalBSONValue(t bsontype.Type, data []byte) error {
	if t != bsontype.Binary {
		return fmt.Errorf("cannot decode BSON %v into a UUID; expected Binary", t)
	}
	subtype, raw, ok := bson.RawValue{Type: t, Value: data}.BinaryOK()
	if !ok {
		return fmt.Errorf("invalid BSON Binary value for UUID")
	}
	if subtype != bsontype.BinaryUUID && subtype != bsontype.BinaryUUIDOld {
		return fmt.Errorf("BSON Binary subtype %#x is not a UUID", subtype)
	}
	if len(raw) != 16 {
		return fmt.Errorf("UUID Binary must be 16 bytes, got %d", len(raw))
	}
	copy(b[:], raw)
	return nil
}

// wishlistDoc is the BSON shape of a `wishlists` document. Field names/casing
// are copied verbatim from the Rust service and are load-bearing: the
// User.wishlists filter ("user._id") and the USER_ID sort key both depend on
// the embedded `user` subdocument, and the stored variant array key must be
// `internal_product_variants` (NOT productVariants / product_variant_ids).
type wishlistDoc struct {
	ID                      binUUID             `bson:"_id"`
	User                    userDoc             `bson:"user"`
	Name                    string              `bson:"name"`
	CreatedAt               primitive.DateTime  `bson:"created_at"`
	LastUpdatedAt           primitive.DateTime  `bson:"last_updated_at"`
	InternalProductVariants []productVariantDoc `bson:"internal_product_variants"`
}

// userDoc is the embedded user stub and the `users` shadow document: only _id.
type userDoc struct {
	ID binUUID `bson:"_id"`
}

// productVariantDoc is an embedded variant stub and the `product_variants`
// shadow document: only _id.
type productVariantDoc struct {
	ID binUUID `bson:"_id"`
}

// Wishlist is a store-level wishlist row with plain uuid.UUID / time fields.
type Wishlist struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	Name              string
	CreatedAt         primitive.DateTime
	LastUpdatedAt     primitive.DateTime
	ProductVariantIDs []uuid.UUID
}

// toWishlist converts a BSON document to a store-level Wishlist.
func toWishlist(d wishlistDoc) Wishlist {
	ids := make([]uuid.UUID, len(d.InternalProductVariants))
	for i, pv := range d.InternalProductVariants {
		ids[i] = pv.ID.UUID()
	}
	return Wishlist{
		ID:                d.ID.UUID(),
		UserID:            d.User.ID.UUID(),
		Name:              d.Name,
		CreatedAt:         d.CreatedAt,
		LastUpdatedAt:     d.LastUpdatedAt,
		ProductVariantIDs: ids,
	}
}

// Connection is a generic pagination result mirroring the GraphQL *Connection
// types: the page of nodes plus the total count and next-page flag.
type Connection[T any] struct {
	Nodes       []T
	TotalCount  int
	HasNextPage bool
}
