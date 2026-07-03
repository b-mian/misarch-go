package graph

import (
	"misarch/inventory/store"
)

// This file maps store documents to GraphQL models and GraphQL inputs to store
// filters/order. ProductItem.ProductVariant is intentionally left nil here: it
// is populated lazily by its field resolver from the ProductVariantID
// extraField. Ids are plain strings end-to-end (the UUID scalar is a string —
// see scalars.go), so they pass through verbatim with no case normalization.

// toProductItem maps a store document to the GraphQL model, carrying the
// variant id in the internal ProductVariantID extraField for the lazy
// productVariant field resolver. The orderId pointer is copied through as-is
// (nil → GraphQL null).
func toProductItem(it store.ProductItem) *ProductItem {
	m := &ProductItem{
		ID:               it.ID,
		InventoryStatus:  ProductItemStatus(it.InventoryStatus),
		ProductVariantID: it.ProductVariant,
	}
	if it.OrderID != nil {
		oid := *it.OrderID
		m.OrderID = &oid
	}
	return m
}

// toProductItems maps a slice of store documents to model values.
func toProductItems(items []store.ProductItem) []ProductItem {
	out := make([]ProductItem, len(items))
	for i, it := range items {
		out[i] = *toProductItem(it)
	}
	return out
}

// storeFilter translates a GraphQL ProductItemFilter to a store.Filter. Only
// present keys are carried; the store applies the correct-vs-buggy field-name
// translation for count vs nodes (spec §3.3 [BUG-COMPAT nodes-filter]).
func storeFilter(f *ProductItemFilter) *store.Filter {
	if f == nil {
		return nil
	}
	sf := &store.Filter{}
	if f.ProductVariant != nil {
		s := *f.ProductVariant
		sf.ProductVariant = &s
	}
	if f.InventoryStatus != nil {
		s := string(*f.InventoryStatus)
		sf.InventoryStatus = &s
	}
	return sf
}

// orderAscending resolves an OrderDirection to ascending?; default (nil order
// or nil direction) is ascending. The only order field (ID → _id) is applied
// by the store. Defaults for field and direction are applied independently
// (spec §3.0), and since ID is the sole field, only the direction matters here.
func orderAscending(o *ProductItemOrder) bool {
	if o == nil || o.Direction == nil {
		return true
	}
	return *o.Direction != OrderDirectionDesc
}
