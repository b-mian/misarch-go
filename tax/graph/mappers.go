package graph

import (
	"misarch/tax/store"
)

// This file maps store rows to GraphQL models and GraphQL order inputs to
// store order columns. Relationship fields (CurrentVersion, Versions, TaxRate)
// are intentionally left nil here: they are populated lazily by their own
// field resolvers. The extraFields (CurrentVersionID, TaxRateID) carry the FKs
// those resolvers need.

// toTaxRate maps a store.TaxRate to the GraphQL model.
func toTaxRate(t store.TaxRate) *TaxRate {
	return &TaxRate{
		ID:               t.ID,
		Name:             t.Name,
		Description:      t.Description,
		CurrentVersionID: t.CurrentVersionID,
	}
}

// toTaxRateVersion maps a store.TaxRateVersion to the GraphQL model.
func toTaxRateVersion(v store.TaxRateVersion) *TaxRateVersion {
	return &TaxRateVersion{
		ID:        v.ID,
		Rate:      v.Rate,
		Version:   v.Version,
		CreatedAt: v.CreatedAt,
		TaxRateID: v.TaxRateID,
	}
}

// toTaxRateConnection maps a paginated store result to the GraphQL connection.
func toTaxRateConnection(c store.Connection[store.TaxRate]) *TaxRateConnection {
	nodes := make([]TaxRate, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toTaxRate(n)
	}
	return &TaxRateConnection{
		Nodes:       nodes,
		TotalCount:  c.TotalCount,
		HasNextPage: c.HasNextPage,
	}
}

// toTaxRateVersionConnection maps a paginated store result to the GraphQL
// connection.
func toTaxRateVersionConnection(c store.Connection[store.TaxRateVersion]) *TaxRateVersionConnection {
	nodes := make([]TaxRateVersion, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toTaxRateVersion(n)
	}
	return &TaxRateVersionConnection{
		Nodes:       nodes,
		TotalCount:  c.TotalCount,
		HasNextPage: c.HasNextPage,
	}
}

// ascending reports whether an OrderDirection means ascending. The default
// (nil) is ASC, matching the original OrderDirection default.
func ascending(d *OrderDirection) bool {
	return d == nil || *d != OrderDirectionDesc
}

// taxRateOrder resolves a TaxRateOrderInput to a store order column set and a
// direction. Defaults: field ID, direction ASC (TaxRateOrder.DEFAULT).
func taxRateOrder(in *TaxRateOrderInput) (store.TaxRateOrderColumn, bool) {
	if in == nil {
		return store.TaxRateOrderByID, true
	}
	col := store.TaxRateOrderByID
	if in.Field != nil && *in.Field == TaxRateOrderFieldName {
		col = store.TaxRateOrderByName
	}
	return col, ascending(in.Direction)
}

// taxRateVersionOrder resolves a TaxRateVersionOrderInput to a store order
// column set and a direction. Defaults: field ID, direction ASC
// (TaxRateVersionOrder.DEFAULT).
func taxRateVersionOrder(in *TaxRateVersionOrderInput) (store.TaxRateVersionOrderColumn, bool) {
	if in == nil {
		return store.TaxRateVersionOrderByID, true
	}
	col := store.TaxRateVersionOrderByID
	if in.Field != nil {
		switch *in.Field {
		case TaxRateVersionOrderFieldCreatedAt:
			col = store.TaxRateVersionOrderByCreatedAt
		case TaxRateVersionOrderFieldVersion:
			col = store.TaxRateVersionOrderByVersion
		}
	}
	return col, ascending(in.Direction)
}
