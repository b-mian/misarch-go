package graph

import (
	"bytes"
	"sort"

	"github.com/google/uuid"

	"misarch/shoppingcart/store"
)

// This file maps store domain structs to GraphQL models and implements the
// in-memory cart-item pagination. Relationship fields (ShoppingCart.
// ShoppingcartItems, ShoppingCartItem.ProductVariant) are left nil here: they
// are populated lazily by their own field resolvers. The internal Items slice
// on ShoppingCart carries the loaded item set for the connection resolver; the
// ProductVariantID extraField carries the FK the ProductVariant resolver needs.

// toCartItem maps a store.CartItem to the GraphQL model. ProductVariant is left
// nil (resolved lazily); ProductVariantID carries the id it needs.
func toCartItem(it store.CartItem) ShoppingCartItem {
	return ShoppingCartItem{
		ID:               it.ID,
		Count:            it.Count,
		AddedAt:          it.AddedAt,
		ProductVariantID: it.ProductVariantID,
	}
}

// toCartItems maps a slice of store.CartItem to GraphQL models.
func toCartItems(items []store.CartItem) []ShoppingCartItem {
	out := make([]ShoppingCartItem, len(items))
	for i, it := range items {
		out[i] = toCartItem(it)
	}
	return out
}

// toShoppingCart maps a store.Cart to the GraphQL ShoppingCart, stashing the
// loaded items on the internal Items field for the connection resolver.
// ShoppingcartItems is left nil (resolved lazily).
func toShoppingCart(c store.Cart) *ShoppingCart {
	return &ShoppingCart{
		LastUpdatedAt: c.LastUpdatedAt,
		Items:         toCartItems(c.Items),
	}
}

// paginateItems replicates the original in-memory pagination
// (ShoppingCart.shoppingcart_items): sort the whole item set by item UUID
// bytes (only the direction from orderBy is honored; the field is ignored),
// then skip/take. totalCount is the pre-pagination length; first omitted means
// "all remaining"; hasNextPage = totalCount > len(page) + skip.
func paginateItems(items []ShoppingCartItem, first *int, skip *int, orderBy *CommonOrderInput) *ShoppingCartItemConnection {
	sorted := make([]ShoppingCartItem, len(items))
	copy(sorted, items)

	asc := ascending(orderBy)
	sort.Slice(sorted, func(i, j int) bool {
		cmp := bytes.Compare(sorted[i].ID[:], sorted[j].ID[:])
		if asc {
			return cmp < 0
		}
		return cmp > 0
	})

	total := len(sorted)

	definitelySkip := 0
	if skip != nil {
		definitelySkip = *skip
	}
	if definitelySkip < 0 {
		definitelySkip = 0
	}

	// skip then take.
	var page []ShoppingCartItem
	if definitelySkip < total {
		page = sorted[definitelySkip:]
	} else {
		page = nil
	}
	if first != nil && *first >= 0 && *first < len(page) {
		page = page[:*first]
	}

	nodes := make([]ShoppingCartItem, len(page))
	copy(nodes, page)

	return &ShoppingCartItemConnection{
		Nodes:       nodes,
		HasNextPage: total > len(nodes)+definitelySkip,
		TotalCount:  total,
	}
}

// ascending reports whether an orderBy means ascending. Defaults (nil orderBy
// or nil direction) are ASC, matching the original OrderDirection default. Only
// the direction is used; the field is ignored (the sole orderable field is ID
// and the sort is always by item UUID).
func ascending(orderBy *CommonOrderInput) bool {
	if orderBy == nil || orderBy.Direction == nil {
		return true
	}
	return *orderBy.Direction != OrderDirectionDesc
}

// dedupItemInputs de-duplicates shopping cart item inputs by the (count,
// productVariantId) pair, preserving first-seen order. This matches the Rust
// HashSet<ShoppingCartItemInput> semantics where equality is over both fields:
// fully identical entries collapse to one; same-variant/different-count entries
// both persist. Item ids are freshly minted downstream, so order is not
// observable by clients.
func dedupItemInputs(inputs []ShoppingCartItemInput) []ShoppingCartItemInput {
	type key struct {
		count   int
		variant uuid.UUID
	}
	seen := make(map[key]struct{}, len(inputs))
	out := make([]ShoppingCartItemInput, 0, len(inputs))
	for _, it := range inputs {
		k := key{count: it.Count, variant: it.ProductVariantID}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, it)
	}
	return out
}
