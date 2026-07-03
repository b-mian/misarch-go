package events

import (
	"fmt"
	"strings"
	"time"
)

// Time wraps time.Time so event payloads serialize timestamps the way the
// original Rust service's serde-default chrono::DateTime<Utc> does:
// RFC 3339 in UTC with a 'Z' suffix and "AutoSi" fractional seconds (0, 3, 6,
// or 9 digits — the fewest that represent the value losslessly).
//
// This is deliberately distinct from the GraphQL DateTime scalar (which the
// original renders via chrono to_rfc3339, i.e. a "+00:00" offset). The event
// DTO path uses the serde default; the GraphQL path uses to_rfc3339.
type Time struct{ time.Time }

// NewTime wraps a time.Time.
func NewTime(t time.Time) Time { return Time{t} }

// UnmarshalJSON parses an RFC 3339 timestamp (accepting 'Z' or numeric offset)
// into UTC.
func (t *Time) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" || s == "" {
		t.Time = time.Time{}
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return err
	}
	t.Time = parsed.UTC()
	return nil
}

// MarshalJSON emits the chrono serde-default form: UTC, 'Z' suffix, AutoSi
// fractional digits.
func (t Time) MarshalJSON() ([]byte, error) {
	return []byte(`"` + t.formatAutoSi() + `"`), nil
}

// formatAutoSi renders the timestamp in UTC as RFC 3339 with 'Z' and the
// fewest of {0, 3, 6, 9} fractional digits that represent the sub-second part
// exactly (chrono SecondsFormat::AutoSi).
func (t Time) formatAutoSi() string {
	u := t.Time.UTC()
	base := u.Format("2006-01-02T15:04:05")
	ns := u.Nanosecond()
	switch {
	case ns == 0:
		return base + "Z"
	case ns%1_000_000 == 0:
		return fmt.Sprintf("%s.%03dZ", base, ns/1_000_000)
	case ns%1_000 == 0:
		return fmt.Sprintf("%s.%06dZ", base, ns/1_000)
	default:
		return fmt.Sprintf("%s.%09dZ", base, ns)
	}
}
