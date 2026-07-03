package graph

import (
	"fmt"
	"sort"

	"github.com/google/uuid"
)

// directionAscending reports whether an OrderDirection means ascending. The
// default (nil) is ASC, matching OrderDirection::default() == Asc.
func directionAscending(d *OrderDirection) bool {
	return d == nil || *d != OrderDirectionDesc
}

// checkPagination validates first/skip the way the original's usize/u32/u64
// deserialization does: a negative value is a coercion error. Returns an error
// mirroring an input coercion failure; nil when both are absent or non-negative.
func checkPagination(first, skip *int) error {
	if first != nil && *first < 0 {
		return fmt.Errorf("`first` cannot be negative")
	}
	if skip != nil && *skip < 0 {
		return fmt.Errorf("`skip` cannot be negative")
	}
	return nil
}

// paginateInMemory reproduces the in-memory connection algorithm used by
// Order.orderItems and OrderItem.discounts: sort by _id per direction (default
// ASC), then skip/first with hasNextPage = totalCount > len(nodes)+skip.
//
// ids extracts the sort key (_id) of an element. The returned nodes slice is a
// fresh copy of the selected window.
func paginateInMemory[T any](items []T, first, skip *int, orderBy *CommonOrderInput, id func(T) uuid.UUID) (nodes []T, hasNextPage bool, totalCount int) {
	// Copy so we do not mutate the caller's slice.
	sorted := make([]T, len(items))
	copy(sorted, items)

	asc := true
	if orderBy != nil {
		asc = directionAscending(orderBy.Direction)
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		less := uuidLess(id(sorted[i]), id(sorted[j]))
		if asc {
			return less
		}
		return uuidLess(id(sorted[j]), id(sorted[i]))
	})

	total := len(sorted)
	sk := 0
	if skip != nil {
		sk = *skip
	}

	// window = sorted[skip:][:first]
	start := sk
	if start > total {
		start = total
	}
	window := sorted[start:]
	if first != nil && *first < len(window) {
		window = window[:*first]
	}

	page := make([]T, len(window))
	copy(page, window)

	hasNext := total > len(page)+sk
	return page, hasNext, total
}

// orderOrderField maps an OrderOrderField to its Mongo sort field string. The
// NAME and LAST_UPDATED_AT fields map to columns that do not exist on the order
// document (a no-op sort), preserved verbatim from OrderOrderField::as_str.
func orderOrderField(f *OrderOrderField) string {
	if f == nil {
		return "_id"
	}
	switch *f {
	case OrderOrderFieldUserID:
		return "user._id"
	case OrderOrderFieldName:
		return "name"
	case OrderOrderFieldCreatedAt:
		return "created_at"
	case OrderOrderFieldLastUpdatedAt:
		return "last_updated_at"
	default: // OrderOrderFieldID
		return "_id"
	}
}

// orderDirectionInt maps an OrderDirection to the Mongo sort int (ASC=1,
// DESC=-1); default ASC.
func orderDirectionInt(d *OrderDirection) int {
	if d != nil && *d == OrderDirectionDesc {
		return -1
	}
	return 1
}
