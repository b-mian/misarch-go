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

// uuidType is the reflect.Type of google/uuid.UUID, used to register the
// custom BSON codec below.
var uuidType = reflect.TypeOf(uuid.UUID{})

// encodeUUID writes a uuid.UUID as a BSON Binary with subtype 0x04 (standard
// UUID). This matches the Rust service's bson::Uuid representation exactly: the
// original stores every id (invoice _id, order_id, embedded address _id/user_id,
// user _id) as Binary subtype 4, NOT the generic subtype 0x00 that the driver's
// default [16]byte encoder would produce. Existing data written by the Rust
// service only round-trips with subtype 4.
func encodeUUID(_ bsoncodec.EncodeContext, vw bsonrw.ValueWriter, val reflect.Value) error {
	if !val.IsValid() || val.Type() != uuidType {
		return bsoncodec.ValueEncoderError{Name: "encodeUUID", Types: []reflect.Type{uuidType}, Received: val}
	}
	u := val.Interface().(uuid.UUID)
	return vw.WriteBinaryWithSubtype(u[:], bsontype.BinaryUUID)
}

// decodeUUID reads a BSON Binary into a uuid.UUID, accepting the modern UUID
// subtype 0x04, the legacy 0x03 (BinaryUUIDOld), and generic 0x00, matching how
// bson::Uuid deserializes in the Rust driver.
func decodeUUID(_ bsoncodec.DecodeContext, vr bsonrw.ValueReader, val reflect.Value) error {
	if !val.CanSet() || val.Type() != uuidType {
		return bsoncodec.ValueDecoderError{Name: "decodeUUID", Types: []reflect.Type{uuidType}, Received: val}
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

// buildRegistry returns a BSON registry that encodes/decodes uuid.UUID as
// Binary subtype 4. Everything else falls back to the driver defaults.
func buildRegistry() *bsoncodec.Registry {
	reg := bson.NewRegistry()
	reg.RegisterTypeEncoder(uuidType, bsoncodec.ValueEncoderFunc(encodeUUID))
	reg.RegisterTypeDecoder(uuidType, bsoncodec.ValueDecoderFunc(decodeUUID))
	return reg
}
