package graph

import (
	"fmt"
	"io"
	"regexp"

	"github.com/99designs/gqlgen/graphql"
)

// UUID is the Go representation of the inventory subgraph's custom UUID scalar.
//
// The original service (src/shared/scalars/CustomUuidScalar.ts) validates the
// value against a case-insensitive shape regex and then passes it through
// UNCHANGED — no lowercasing, no canonicalization. It is therefore modelled as
// a plain string (NOT github.com/google/uuid.UUID, which would canonicalize
// case and reject non-canonical-but-shape-valid input differently). Stored and
// returned values are byte-for-byte what was received/stored (spec §3.0).
type UUID = string

// uuidRegex mirrors the original scalar's pattern exactly, including the
// case-insensitive `i` flag (rendered here as the `(?i)` Go prefix): hex groups
// in the 8-4-4-4-12 layout. Only the shape is checked; any version/variant
// nibble is accepted, and upper- or lower-case hex both match.
var uuidRegex = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// MarshalUUID serializes a UUID scalar. Serialization runs the SAME validation
// as parsing (the original's serialize === parseValue === validate). The
// validation happens EAGERLY (before the WriterFunc is returned) so that on a
// violation the panic fires synchronously inside gqlgen's field-resolution
// recover block — reproducing the original scalar's thrown Error('invalid
// uuid') as a GraphQL field error rather than crashing at response-write time.
// On success the value is emitted verbatim (no case normalization).
//
// In the fixed port an invalid value is effectively unreachable: stored ids are
// service-generated UUIDv4 strings, scalar inputs are validated on unmarshal,
// and the release-upsert orphan bug (the one source of missing/blank ids) is
// not replicated. The check is kept for exact behavioral fidelity.
func MarshalUUID(u UUID) graphql.Marshaler {
	if !uuidRegex.MatchString(u) {
		panic(fmt.Errorf("invalid uuid"))
	}
	return graphql.WriterFunc(func(w io.Writer) {
		graphql.MarshalString(u).MarshalGQL(w)
	})
}

// UnmarshalUUID parses a UUID scalar value. A non-string input reproduces the
// literal-branch error "UUID must be a string."; a shape violation reproduces
// "invalid uuid". The value is returned unchanged (no case normalization).
func UnmarshalUUID(v any) (UUID, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("UUID must be a string.")
	}
	if !uuidRegex.MatchString(s) {
		return "", fmt.Errorf("invalid uuid")
	}
	return s, nil
}
