package graph

import (
	"bytes"
	"sort"

	"github.com/google/uuid"

	"misarch/wishlist/store"
)

// This file maps store domain structs to GraphQL models, implements the
// in-memory product-variant pagination (Wishlist.productVariants), and maps the
// WishlistOrderInput to a Mongo sort key + direction.
//
// Relationship fields (Wishlist.User, Wishlist.ProductVariants) are left nil
// here: they are populated lazily by their own field resolvers. The mapper only
// carries the FK extraFields (UserID, ProductVariantIDs) those resolvers need.

// toWishlist maps a store.Wishlist to the GraphQL model. User and
// ProductVariants are left nil (resolved lazily); UserID and ProductVariantIDs
// carry the data those field resolvers need. Timestamps are normalized to UTC.
func toWishlist(w store.Wishlist) *Wishlist {
	return &Wishlist{
		ID:                w.ID,
		Name:              w.Name,
		CreatedAt:         w.CreatedAt.Time().UTC(),
		LastUpdatedAt:     w.LastUpdatedAt.Time().UTC(),
		UserID:            w.UserID,
		ProductVariantIDs: w.ProductVariantIDs,
	}
}

// toWishlistConnection maps a paginated store result (User.wishlists) to the
// GraphQL connection. Nodes is never nil (empty slice on no matches, matching
// the SDL [Wishlist!]!).
func toWishlistConnection(c store.Connection[store.Wishlist]) *WishlistConnection {
	nodes := make([]Wishlist, 0, len(c.Nodes))
	for _, n := range c.Nodes {
		nodes = append(nodes, *toWishlist(n))
	}
	return &WishlistConnection{
		Nodes:       nodes,
		HasNextPage: c.HasNextPage,
		TotalCount:  c.TotalCount,
	}
}

// paginateProductVariants replicates the original in-memory pagination
// (Wishlist.productVariants, spec §3.3/§3.6): clone the wishlist's embedded
// variant-id set, sort by variant UUID bytes (only the direction from orderBy
// is honored; the field is ignored — the sole orderable field is ID and the
// comparator always compares the UUID), then skip/take. totalCount is the
// pre-pagination length; first omitted means "all remaining"; hasNextPage =
// totalCount > len(nodes) + skip. No database access.
func paginateProductVariants(ids []uuid.UUID, first *int, skip *int, orderBy *CommonOrderInput) *ProductVariantConnection {
	sorted := make([]uuid.UUID, len(ids))
	copy(sorted, ids)

	asc := commonAscending(orderBy)
	sort.Slice(sorted, func(i, j int) bool {
		cmp := bytes.Compare(sorted[i][:], sorted[j][:])
		if asc {
			return cmp < 0
		}
		return cmp > 0
	})

	total := len(sorted)

	skipN := 0
	if skip != nil {
		skipN = *skip
	}
	if skipN < 0 {
		skipN = 0
	}

	// skip then take.
	var page []uuid.UUID
	if skipN < total {
		page = sorted[skipN:]
	}
	if first != nil && *first >= 0 && *first < len(page) {
		page = page[:*first]
	}

	nodes := make([]ProductVariant, len(page))
	for i, id := range page {
		nodes[i] = ProductVariant{ID: id}
	}

	return &ProductVariantConnection{
		Nodes:       nodes,
		HasNextPage: total > len(nodes)+skipN,
		TotalCount:  total,
	}
}

// commonAscending reports whether a CommonOrderInput means ascending. Defaults
// (nil orderBy or nil direction) are ASC, matching the OrderDirection default.
// Only the direction is used; the field is ignored (the sole orderable field is
// ID and the sort is always by variant UUID).
func commonAscending(orderBy *CommonOrderInput) bool {
	if orderBy == nil || orderBy.Direction == nil {
		return true
	}
	return *orderBy.Direction != OrderDirectionDesc
}

// wishlistSort resolves a WishlistOrderInput (User.wishlists) to a Mongo sort
// key and direction, replicating the original defaults (direction ASC, field
// ID) and the exact field→Mongo-key mapping (spec §3.5). Both the input and its
// sub-fields independently default.
func wishlistSort(in *WishlistOrderInput) (sortKey string, asc bool) {
	asc = true // OrderDirection default = ASC
	field := WishlistOrderFieldID
	if in != nil {
		if in.Direction != nil && *in.Direction == OrderDirectionDesc {
			asc = false
		}
		if in.Field != nil {
			field = *in.Field
		}
	}
	return wishlistOrderFieldKey(field), asc
}

// wishlistOrderFieldKey maps a WishlistOrderField enum value to its Mongo sort
// key, implementing every enum value exactly per the spec's mapping table
// (§3.5). Note USER_ID → "user._id" (the embedded subdocument key), not a
// top-level field.
func wishlistOrderFieldKey(f WishlistOrderField) string {
	switch f {
	case WishlistOrderFieldID:
		return "_id"
	case WishlistOrderFieldUserID:
		return "user._id"
	case WishlistOrderFieldName:
		return "name"
	case WishlistOrderFieldCreatedAt:
		return "created_at"
	case WishlistOrderFieldLastUpdatedAt:
		return "last_updated_at"
	default:
		return "_id"
	}
}
