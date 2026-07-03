package store

import (
	"testing"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsontype"
)

// TestUUIDBinarySubtype4 verifies that a uuid.UUID marshals to a BSON Binary
// with subtype 0x04 (standard UUID) under the store registry, matching the Rust
// bson::Uuid on-disk representation — critical for $in/equality interop with
// event-supplied ids and cross-service matching.
func TestUUIDBinarySubtype4(t *testing.T) {
	reg := buildRegistry()
	id := uuid.MustParse("6f9619ff-8b86-4d01-b42d-00cf4fc964ff")

	type doc struct {
		ID uuid.UUID `bson:"_id"`
	}
	b, err := bson.MarshalWithRegistry(reg, doc{ID: id})
	if err != nil {
		t.Fatal(err)
	}

	raw := bson.Raw(b)
	val := raw.Lookup("_id")
	if val.Type != bsontype.Binary {
		t.Fatalf("_id type = %v, want Binary", val.Type)
	}
	subtype, data := val.Binary()
	if subtype != 0x04 {
		t.Errorf("_id binary subtype = %#x, want 0x04", subtype)
	}
	if len(data) != 16 {
		t.Errorf("_id binary length = %d, want 16", len(data))
	}

	// Round-trip decode.
	var back doc
	if err := bson.UnmarshalWithRegistry(reg, b, &back); err != nil {
		t.Fatal(err)
	}
	if back.ID != id {
		t.Errorf("round-trip id = %s, want %s", back.ID, id)
	}
}

// TestOrderEnumStoredAsString verifies order_status is stored as a plain string
// (SCREAMING_SNAKE), not an enum object, and that a nil placed_at/rejection are
// encoded as BSON null.
func TestOrderEnumStoredAsString(t *testing.T) {
	reg := buildRegistry()
	o := Order{
		ID:                 uuid.New(),
		OrderStatus:        "PENDING",
		InternalOrderItems: []OrderItem{},
	}
	b, err := bson.MarshalWithRegistry(reg, o)
	if err != nil {
		t.Fatal(err)
	}
	raw := bson.Raw(b)
	if v := raw.Lookup("order_status"); v.Type != bsontype.String || v.StringValue() != "PENDING" {
		t.Errorf("order_status = %v/%q, want String/PENDING", v.Type, v.StringValue())
	}
	if v := raw.Lookup("placed_at"); v.Type != bsontype.Null {
		t.Errorf("placed_at type = %v, want Null", v.Type)
	}
	if v := raw.Lookup("rejection_reason"); v.Type != bsontype.Null {
		t.Errorf("rejection_reason type = %v, want Null", v.Type)
	}
}

// TestIsVisible verifies the create-vs-update visibility semantics: a bool true
// is visible; the string "true" (as written by the update event) is NOT.
func TestIsVisible(t *testing.T) {
	if !(ProductVariant{IsPubliclyVisible: true}).IsVisible() {
		t.Error("bool true must be visible")
	}
	if (ProductVariant{IsPubliclyVisible: false}).IsVisible() {
		t.Error("bool false must not be visible")
	}
	if (ProductVariant{IsPubliclyVisible: "true"}).IsVisible() {
		t.Error(`string "true" must NOT be visible (update-event bug reproduced)`)
	}
}
