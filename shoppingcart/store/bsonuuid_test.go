package store

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsontype"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TestUUIDCodecEncodesSubtype4 verifies that a uuid.UUID marshals to a BSON
// Binary with subtype 0x04 holding the 16 UUID bytes in canonical order. This
// is the single most fidelity-critical behavior: if the subtype or byte order
// is wrong, every {"_id": someUUID} filter silently matches nothing against the
// documents written by this service and the original Rust service.
func TestUUIDCodecEncodesSubtype4(t *testing.T) {
	reg := newRegistry()
	id := uuid.MustParse("3c2a4b1e-0000-4000-8000-000000000abc")

	type doc struct {
		ID uuid.UUID `bson:"_id"`
	}
	raw, err := bson.MarshalWithRegistry(reg, doc{ID: id})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Decode generically into a primitive.Binary to inspect subtype + bytes.
	var back struct {
		ID primitive.Binary `bson:"_id"`
	}
	if err := bson.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal to Binary: %v", err)
	}
	if back.ID.Subtype != bsontype.BinaryUUID {
		t.Fatalf("subtype = %#x, want %#x (BinaryUUID)", back.ID.Subtype, bsontype.BinaryUUID)
	}
	if !bytes.Equal(back.ID.Data, id[:]) {
		t.Fatalf("bytes = %x, want %x", back.ID.Data, id[:])
	}
}

// TestUUIDCodecRoundTrip verifies encode→decode returns the same UUID.
func TestUUIDCodecRoundTrip(t *testing.T) {
	reg := newRegistry()
	id := uuid.New()

	type doc struct {
		ID uuid.UUID `bson:"_id"`
	}
	raw, err := bson.MarshalWithRegistry(reg, doc{ID: id})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back doc
	if err := bson.UnmarshalWithRegistry(reg, raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.ID != id {
		t.Fatalf("round-trip = %s, want %s", back.ID, id)
	}
}

// TestUUIDCodecDecodesLegacySubtypes verifies the decoder also accepts subtype
// 3 (legacy) and subtype 0 (generic) binaries, so any historical documents
// still decode.
func TestUUIDCodecDecodesLegacySubtypes(t *testing.T) {
	reg := newRegistry()
	id := uuid.New()

	for _, subtype := range []byte{bsontype.BinaryUUIDOld, bsontype.BinaryGeneric} {
		raw, err := bson.Marshal(struct {
			ID primitive.Binary `bson:"_id"`
		}{ID: primitive.Binary{Subtype: subtype, Data: id[:]}})
		if err != nil {
			t.Fatalf("marshal subtype %#x: %v", subtype, err)
		}
		var back struct {
			ID uuid.UUID `bson:"_id"`
		}
		if err := bson.UnmarshalWithRegistry(reg, raw, &back); err != nil {
			t.Fatalf("unmarshal subtype %#x: %v", subtype, err)
		}
		if back.ID != id {
			t.Fatalf("subtype %#x round-trip = %s, want %s", subtype, back.ID, id)
		}
	}
}
