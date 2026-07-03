package store

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// dedupe returns the ids with duplicates removed, preserving first-occurrence
// order. This mirrors Kotlin's `List.toSet()` (a LinkedHashSet keeps insertion
// order), which the error messages and join inserts depend on.
func dedupe(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// formatUUIDList renders a slice of UUIDs the way Kotlin's List<UUID>.toString
// does: "[a, b, c]" (canonical lowercase uuids, comma-space separated). Empty
// list renders "[]". Used verbatim in the missing-entity error messages.
func formatUUIDList(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// missingIDs returns the subset of ids (order preserved) that do NOT exist in
// the given table's id column, using a single ANY query. ids should already be
// de-duplicated.
func missingIDs(ctx context.Context, q querier, table string, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx,
		"SELECT id FROM "+table+" WHERE id = ANY($1)", ids)
	if err != nil {
		return nil, err
	}
	present := map[uuid.UUID]struct{}{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		present[id] = struct{}{}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var missing []uuid.UUID
	for _, id := range ids {
		if _, ok := present[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// existsByID reports whether an id exists in the table's id column.
func existsByID(ctx context.Context, q querier, table string, id uuid.UUID) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM "+table+" WHERE id = $1)", id).Scan(&exists)
	return exists, err
}
