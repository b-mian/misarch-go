// Package store is the review service persistence layer. It wraps a MongoDB
// database and exposes typed query/mutation methods over the four collections
// the service owns: reviews (the aggregate root) and the users, products and
// product_variants shadow copies materialized from Dapr events.
//
// UUIDs are stored as BSON Binary subtype 4 (the standard UUID representation),
// matching the original Rust service's bson::Uuid on-disk form so a shared
// database reads identically across the fleet. This is achieved with a custom
// BSON registry (see uuidRegistry) applied to every collection handle.
package store

import (
	"fmt"
	"reflect"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsoncodec"
	"go.mongodb.org/mongo-driver/bson/bsonrw"
	"go.mongodb.org/mongo-driver/bson/bsontype"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Collection names, snake_case exactly as the original service used them.
const (
	collReviews         = "reviews"
	collUsers           = "users"
	collProducts        = "products"
	collProductVariants = "product_variants"
)

// Store provides persistence operations for the review service.
type Store struct {
	reviews         *mongo.Collection
	users           *mongo.Collection
	products        *mongo.Collection
	productVariants *mongo.Collection
}

// New builds a Store over the given database. Every collection handle uses a
// registry that encodes/decodes google/uuid.UUID as BSON Binary subtype 4.
func New(db *mongo.Database) *Store {
	reg := uuidRegistry()
	opts := options.Collection().SetRegistry(reg)
	return &Store{
		reviews:         db.Collection(collReviews, opts),
		users:           db.Collection(collUsers, opts),
		products:        db.Collection(collProducts, opts),
		productVariants: db.Collection(collProductVariants, opts),
	}
}

// EmbeddedUser is the {_id} sub-document embedded in a review's `user` field.
type EmbeddedUser struct {
	ID uuid.UUID `bson:"_id"`
}

// EmbeddedProductVariant is the {_id, product_id} sub-document embedded in a
// review's `product_variant` field (a full local copy captured at creation).
type EmbeddedProductVariant struct {
	ID        uuid.UUID `bson:"_id"`
	ProductID uuid.UUID `bson:"product_id"`
}

// Review is a document of the reviews collection. Field names/casing mirror the
// original Rust bson serialization exactly (snake_case) so the on-disk shape is
// identical. Rating is stored as its string form (see rating.go).
type Review struct {
	ID             uuid.UUID              `bson:"_id"`
	User           EmbeddedUser           `bson:"user"`
	ProductVariant EmbeddedProductVariant `bson:"product_variant"`
	Body           string                 `bson:"body"`
	Rating         string                 `bson:"rating"`
	CreatedAt      DateTime               `bson:"created_at"`
	LastUpdatedAt  DateTime               `bson:"last_updated_at"`
	IsVisible      bool                   `bson:"is_visible"`
}

// User is a document of the users shadow collection: only an id.
type User struct {
	ID uuid.UUID `bson:"_id"`
}

// Product is a document of the products shadow collection: only an id.
type Product struct {
	ID uuid.UUID `bson:"_id"`
}

// ProductVariant is a document of the product_variants shadow collection.
type ProductVariant struct {
	ID        uuid.UUID `bson:"_id"`
	ProductID uuid.UUID `bson:"product_id"`
}

// Connection is a generic pagination result mirroring the GraphQL
// ReviewConnection: the page of nodes plus the total count and next-page flag.
type Connection[T any] struct {
	Nodes       []T
	TotalCount  int
	HasNextPage bool
}

// tUUID is the reflect.Type of google/uuid.UUID, used to register the codec.
var tUUID = reflect.TypeOf(uuid.UUID{})

// uuidRegistry returns a BSON registry whose only customization is a codec that
// stores uuid.UUID as BSON Binary subtype 4 (bson::Uuid standard form). It
// starts from bson.NewRegistry(), which already registers the default value
// codecs AND the primitive codecs (needed for bson.M/bson.D filters and
// primitive.* types), then layers the UUID codec on top. This matches the
// convention used by the other MongoDB Go services in the fleet (order,
// invoice).
func uuidRegistry() *bsoncodec.Registry {
	reg := bson.NewRegistry()
	reg.RegisterTypeEncoder(tUUID, bsoncodec.ValueEncoderFunc(encodeUUID))
	reg.RegisterTypeDecoder(tUUID, bsoncodec.ValueDecoderFunc(decodeUUID))
	return reg
}

// encodeUUID writes a uuid.UUID as a BSON Binary with subtype 0x04 (standard
// UUID). This matches the original Rust service's bson::Uuid representation
// exactly: every id (review _id, embedded user._id, product_variant._id/
// product_id, and the shadow collection _ids) is stored as Binary subtype 4,
// NOT the generic subtype 0x00 the driver's default [16]byte encoder produces.
func encodeUUID(_ bsoncodec.EncodeContext, vw bsonrw.ValueWriter, val reflect.Value) error {
	if !val.IsValid() || val.Type() != tUUID {
		return bsoncodec.ValueEncoderError{Name: "encodeUUID", Types: []reflect.Type{tUUID}, Received: val}
	}
	u := val.Interface().(uuid.UUID)
	return vw.WriteBinaryWithSubtype(u[:], bsontype.BinaryUUID)
}

// decodeUUID reads a BSON Binary into a uuid.UUID, accepting the modern UUID
// subtype 0x04, the legacy 0x03 (BinaryUUIDOld) and generic 0x00, matching how
// bson::Uuid deserializes in the Rust driver.
func decodeUUID(_ bsoncodec.DecodeContext, vr bsonrw.ValueReader, val reflect.Value) error {
	if !val.CanSet() || val.Type() != tUUID {
		return bsoncodec.ValueDecoderError{Name: "decodeUUID", Types: []reflect.Type{tUUID}, Received: val}
	}
	if vr.Type() != bsontype.Binary {
		return fmt.Errorf("cannot decode %v into a UUID", vr.Type())
	}
	data, subtype, err := vr.ReadBinary()
	if err != nil {
		return err
	}
	if subtype != bsontype.BinaryUUID && subtype != bsontype.BinaryUUIDOld && subtype != bsontype.BinaryGeneric {
		return fmt.Errorf("cannot decode binary subtype %v into a UUID", subtype)
	}
	u, err := uuid.FromBytes(data)
	if err != nil {
		return err
	}
	val.Set(reflect.ValueOf(u))
	return nil
}
