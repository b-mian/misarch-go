// Package scalars provides gqlgen marshalers for the custom scalars used by
// the MiSArch subgraph schemas (UUID, DateTime, Date, JSONObject).
package scalars

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/google/uuid"
)

// Aliases bound in each service's gqlgen.yml (gqlgen resolves the
// Marshal<Name>/Unmarshal<Name> functions in this package via these names).
type (
	UUID       = uuid.UUID
	DateTime   = time.Time
	JSONObject = map[string]any
)

// MarshalUUID serializes a UUID as its canonical string form.
func MarshalUUID(u uuid.UUID) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		fmt.Fprintf(w, "%q", u.String())
	})
}

// UnmarshalUUID parses a UUID scalar value.
func UnmarshalUUID(v any) (uuid.UUID, error) {
	s, ok := v.(string)
	if !ok {
		return uuid.Nil, fmt.Errorf("UUID must be a string, got %T", v)
	}
	return uuid.Parse(s)
}

// MarshalDateTime serializes a timestamp as RFC 3339 with UTC offset,
// matching the OffsetDateTime / chrono::DateTime<Utc> encoding of the
// original services.
func MarshalDateTime(t time.Time) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		fmt.Fprintf(w, "%q", t.UTC().Format(time.RFC3339Nano))
	})
}

// UnmarshalDateTime parses an RFC 3339 timestamp.
func UnmarshalDateTime(v any) (time.Time, error) {
	s, ok := v.(string)
	if !ok {
		return time.Time{}, fmt.Errorf("DateTime must be a string, got %T", v)
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid DateTime %q: %w", s, err)
	}
	return t.UTC(), nil
}

// Date is a calendar date (no time component), serialized as yyyy-mm-dd.
type Date struct{ time.Time }

// MarshalDate serializes a Date as yyyy-mm-dd.
func MarshalDate(d Date) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		fmt.Fprintf(w, "%q", d.Format(time.DateOnly))
	})
}

// UnmarshalDate parses a yyyy-mm-dd date.
func UnmarshalDate(v any) (Date, error) {
	s, ok := v.(string)
	if !ok {
		return Date{}, fmt.Errorf("Date must be a string, got %T", v)
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Date{}, fmt.Errorf("invalid Date %q: %w", s, err)
	}
	return Date{t}, nil
}

// MarshalJSONObject serializes an arbitrary JSON object scalar.
func MarshalJSONObject(m map[string]any) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		_ = json.NewEncoder(w).Encode(m)
	})
}

// UnmarshalJSONObject parses an arbitrary JSON object scalar.
func UnmarshalJSONObject(v any) (map[string]any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("JSONObject must be an object, got %T", v)
	}
	return m, nil
}
