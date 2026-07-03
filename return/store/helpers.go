package store

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// formatUUIDList renders a slice of UUIDs the way Kotlin renders a
// Collection<UUID>.toString(): "[uuid1, uuid2]" (comma-space separated, square
// brackets, lowercase hyphenated UUIDs). Reproduced byte-for-byte so the
// "... do not exist" / "... have already been returned" error strings match.
func formatUUIDList(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// missingIDs returns the requested ids that were not loaded, preserving the
// request order — matching Kotlin's `input.orderItemIds.toSet() - loaded.toSet()`
// where the left LinkedHashSet keeps insertion (request) order.
func missingIDs(requested []uuid.UUID, loaded []OrderItem) []uuid.UUID {
	present := make(map[uuid.UUID]struct{}, len(loaded))
	for _, oi := range loaded {
		present[oi.ID] = struct{}{}
	}
	var missing []uuid.UUID
	seen := make(map[uuid.UUID]struct{}, len(requested))
	for _, id := range requested {
		if _, ok := present[id]; ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue // a Set contains each id once
		}
		seen[id] = struct{}{}
		missing = append(missing, id)
	}
	return missing
}

// returnedItemIDs returns the ids of loaded order items that already carry a
// returnedWithId, in load order (Kotlin: orderItems.filter { ... }.map { it.id }).
func returnedItemIDs(loaded []OrderItem) []uuid.UUID {
	var out []uuid.UUID
	for _, oi := range loaded {
		if oi.ReturnedWithID != nil {
			out = append(out, oi.ID)
		}
	}
	return out
}

// distinctPtrIDs collects the distinct non-nil ids produced by sel over items,
// preserving first-seen order (Kotlin `.map { ... }.toSet()`). Callers only
// invoke it after validating the selected pointer is non-nil for every item, so
// nil is defensively skipped.
func distinctPtrIDs(sel func(OrderItem) *uuid.UUID, items []OrderItem) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(items))
	var out []uuid.UUID
	for _, it := range items {
		p := sel(it)
		if p == nil {
			continue
		}
		if _, ok := seen[*p]; ok {
			continue
		}
		seen[*p] = struct{}{}
		out = append(out, *p)
	}
	return out
}

// durationDays returns the whole days between from and to, truncated toward
// zero — matching java.time.Duration.between(from, to).toDays().
func durationDays(from, to time.Time) int64 {
	return int64(to.Sub(from) / (24 * time.Hour))
}
