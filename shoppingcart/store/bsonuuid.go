package store

import (
	"fmt"
	"reflect"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsoncodec"
	"go.mongodb.org/mongo-driver/bson/bsonrw"
	"go.mongodb.org/mongo-driver/bson/bsontype"
)

// uuidType is the reflect.Type of github.com/google/uuid.UUID ([16]byte).
var uuidType = reflect.TypeOf(uuid.UUID{})

// uuidCodec encodes/decodes uuid.UUID as a BSON Binary with subtype 0x04
// (the modern "UUID" subtype). This matches the Rust bson::Uuid on-disk
// representation exactly: subtype-4 binary holding the 16 UUID bytes in
// canonical (network) byte order. Without this codec, uuid.UUID (a [16]byte)
// marshals as a BSON array of 16 ints, so every {"_id": someUUID} filter would
// silently match nothing.
type uuidCodec struct{}

// EncodeValue writes a uuid.UUID as Binary subtype 4.
func (uuidCodec) EncodeValue(_ bsoncodec.EncodeContext, vw bsonrw.ValueWriter, val reflect.Value) error {
	if !val.IsValid() || val.Type() != uuidType {
		return bsoncodec.ValueEncoderError{Name: "uuidCodec.EncodeValue", Types: []reflect.Type{uuidType}, Received: val}
	}
	u := val.Interface().(uuid.UUID)
	return vw.WriteBinaryWithSubtype(u[:], bsontype.BinaryUUID)
}

// DecodeValue reads a BSON value into a uuid.UUID. It accepts Binary subtype 4
// (the standard, what this service and Rust write) as well as subtype 3
// (legacy) and subtype 0 (generic) so any historical documents still decode,
// plus a string form for robustness.
func (uuidCodec) DecodeValue(_ bsoncodec.DecodeContext, vr bsonrw.ValueReader, val reflect.Value) error {
	if !val.CanSet() || val.Type() != uuidType {
		return bsoncodec.ValueDecoderError{Name: "uuidCodec.DecodeValue", Types: []reflect.Type{uuidType}, Received: val}
	}
	switch vr.Type() {
	case bsontype.Binary:
		data, subtype, err := vr.ReadBinary()
		if err != nil {
			return err
		}
		if subtype != bsontype.BinaryUUID && subtype != bsontype.BinaryUUIDOld && subtype != bsontype.BinaryGeneric {
			return fmt.Errorf("cannot decode BSON binary subtype %#x into a UUID", subtype)
		}
		u, err := uuid.FromBytes(data)
		if err != nil {
			return err
		}
		val.Set(reflect.ValueOf(u))
		return nil
	case bsontype.String:
		s, err := vr.ReadString()
		if err != nil {
			return err
		}
		u, err := uuid.Parse(s)
		if err != nil {
			return err
		}
		val.Set(reflect.ValueOf(u))
		return nil
	case bsontype.Null:
		if err := vr.ReadNull(); err != nil {
			return err
		}
		val.Set(reflect.ValueOf(uuid.Nil))
		return nil
	default:
		return fmt.Errorf("cannot decode BSON %s into a UUID", vr.Type())
	}
}

// newRegistry builds a BSON registry that adds the subtype-4 UUID codec to the
// driver defaults. Applied to the database handle at connect time so all
// collections encode/decode uuid.UUID fields (notably every "_id") correctly.
func newRegistry() *bsoncodec.Registry {
	rb := bson.NewRegistryBuilder()
	rb.RegisterTypeEncoder(uuidType, uuidCodec{})
	rb.RegisterTypeDecoder(uuidType, uuidCodec{})
	return rb.Build()
}
