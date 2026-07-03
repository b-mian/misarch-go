package store

import "time"

// DateTime is the store-level timestamp type. It aliases time.Time; the mongo
// driver's default time.Time codec stores it as a BSON Date (millisecond
// precision) and reads it back in UTC, matching the original service's
// bson::DateTime on-disk form and read-back semantics.
type DateTime = time.Time

// Now returns the current server time in UTC, mirroring bson::DateTime::now().
// The mongo driver truncates to millisecond precision on write, so the stored
// value has the same resolution as the Rust original.
func Now() DateTime {
	return time.Now().UTC()
}
