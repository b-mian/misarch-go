package graph

import (
	"misarch/catalog/store"
)

// This file maps store rows to GraphQL models and GraphQL order inputs to store
// order columns. Relationship fields (Category, Characteristic, Values,
// Product, DefaultVariant, Variants, CurrentVersion, Versions, ProductVariant,
// TaxRate, CharacteristicValues, Medias, Categories, Characteristics) are left
// nil/zero here: they are populated lazily by their own field resolvers. The
// extraFields (CategoryID, CharacteristicID, DefaultVariantID, ProductID,
// CurrentVersionID, ProductVariantID, TaxRateID) carry the FKs those resolvers
// need.

// toProduct maps a store.Product to the GraphQL model.
func toProduct(p store.Product) *Product {
	return &Product{
		ID:                p.ID,
		InternalName:      p.InternalName,
		IsPubliclyVisible: p.IsPubliclyVisible,
		DefaultVariantID:  p.DefaultVariantID,
	}
}

// toProductVariant maps a store.ProductVariant to the GraphQL model.
func toProductVariant(v store.ProductVariant) *ProductVariant {
	return &ProductVariant{
		ID:                v.ID,
		IsPubliclyVisible: v.IsPubliclyVisible,
		ProductID:         v.ProductID,
		CurrentVersionID:  v.CurrentVersion,
	}
}

// toProductVariantVersion maps a store.ProductVariantVersion to the GraphQL model.
func toProductVariantVersion(v store.ProductVariantVersion) *ProductVariantVersion {
	return &ProductVariantVersion{
		ID:                   v.ID,
		Name:                 v.Name,
		Description:          v.Description,
		Version:              v.Version,
		RetailPrice:          v.RetailPrice,
		CreatedAt:            v.CreatedAt,
		CanBeReturnedForDays: v.CanBeReturnedForDays,
		Weight:               v.Weight,
		ProductVariantID:     v.ProductVariantID,
		TaxRateID:            v.TaxRateID,
	}
}

// toCategory maps a store.Category to the GraphQL model.
func toCategory(c store.Category) *Category {
	return &Category{
		ID:          c.ID,
		Name:        c.Name,
		Description: c.Description,
	}
}

// toTaxRate maps a store.TaxRate id to the GraphQL stub model.
func toTaxRate(id store.TaxRate) *TaxRate {
	return &TaxRate{ID: id.ID}
}

// toCharacteristic resolves the concrete GraphQL type of a characteristic row
// from its discriminator column: CATEGORICAL → *CategoricalCategoryCharacteristic,
// NUMERICAL → *NumericalCategoryCharacteristic. Both implement the
// CategoryCharacteristic interface.
func toCharacteristic(c store.CategoryCharacteristic) CategoryCharacteristic {
	switch c.Discriminator {
	case store.DiscriminatorNumerical:
		unit := ""
		if c.Unit != nil {
			unit = *c.Unit
		}
		return &NumericalCategoryCharacteristic{
			ID:          c.ID,
			Name:        c.Name,
			Description: c.Description,
			Unit:        unit,
			CategoryID:  c.CategoryID,
		}
	default: // CATEGORICAL
		return &CategoricalCategoryCharacteristic{
			ID:          c.ID,
			Name:        c.Name,
			Description: c.Description,
			CategoryID:  c.CategoryID,
		}
	}
}

// toCharacteristicValue resolves the concrete GraphQL type of a characteristic
// value row from its discriminator: CATEGORICAL → *CategoricalCategoryCharacteristicValue
// (stringvalue), NUMERICAL → *NumericalCategoryCharacteristicValue (doublevalue).
func toCharacteristicValue(v store.CategoryCharacteristicValue) CategoryCharacteristicValue {
	switch v.Discriminator {
	case store.DiscriminatorNumerical:
		value := 0.0
		if v.DoubleValue != nil {
			value = *v.DoubleValue
		}
		return &NumericalCategoryCharacteristicValue{
			Value:            value,
			CharacteristicID: v.CategoryCharacteristicID,
		}
	default: // CATEGORICAL
		value := ""
		if v.StringValue != nil {
			value = *v.StringValue
		}
		return &CategoricalCategoryCharacteristicValue{
			Value:            value,
			CharacteristicID: v.CategoryCharacteristicID,
		}
	}
}

// Connection mappers convert paginated store results to the GraphQL connections
// (each node mapped through the corresponding to* function).

func toProductConnection(c store.Connection[store.Product]) *ProductConnection {
	nodes := make([]Product, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toProduct(n)
	}
	return &ProductConnection{Nodes: nodes, TotalCount: c.TotalCount, HasNextPage: c.HasNextPage}
}

func toProductVariantConnection(c store.Connection[store.ProductVariant]) *ProductVariantConnection {
	nodes := make([]ProductVariant, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toProductVariant(n)
	}
	return &ProductVariantConnection{Nodes: nodes, TotalCount: c.TotalCount, HasNextPage: c.HasNextPage}
}

func toProductVariantVersionConnection(c store.Connection[store.ProductVariantVersion]) *ProductVariantVersionConnection {
	nodes := make([]ProductVariantVersion, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toProductVariantVersion(n)
	}
	return &ProductVariantVersionConnection{Nodes: nodes, TotalCount: c.TotalCount, HasNextPage: c.HasNextPage}
}

func toCategoryConnection(c store.Connection[store.Category]) *CategoryConnection {
	nodes := make([]Category, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toCategory(n)
	}
	return &CategoryConnection{Nodes: nodes, TotalCount: c.TotalCount, HasNextPage: c.HasNextPage}
}

func toCharacteristicConnection(c store.Connection[store.CategoryCharacteristic]) *CategoryCharacteristicConnection {
	nodes := make([]CategoryCharacteristic, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = toCharacteristic(n)
	}
	return &CategoryCharacteristicConnection{Nodes: nodes, TotalCount: c.TotalCount, HasNextPage: c.HasNextPage}
}

func toCharacteristicValueConnection(c store.Connection[store.CategoryCharacteristicValue]) *CategoryCharacteristicValueConnection {
	nodes := make([]CategoryCharacteristicValue, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = toCharacteristicValue(n)
	}
	return &CategoryCharacteristicValueConnection{Nodes: nodes, TotalCount: c.TotalCount, HasNextPage: c.HasNextPage}
}

func toMediaConnection(c store.Connection[store.Media]) *MediaConnection {
	nodes := make([]Media, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = Media{ID: n.ID}
	}
	return &MediaConnection{Nodes: nodes, TotalCount: c.TotalCount, HasNextPage: c.HasNextPage}
}

func toCategoricalValueConnection(c store.Connection[store.CategoricalValue]) *CategoricalCategoryCharacteristicValueConnection {
	nodes := make([]CategoricalCategoryCharacteristicValue, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = CategoricalCategoryCharacteristicValue{
			Value:            n.Value,
			CharacteristicID: n.CharacteristicID,
		}
	}
	return &CategoricalCategoryCharacteristicValueConnection{Nodes: nodes, TotalCount: c.TotalCount, HasNextPage: c.HasNextPage}
}

// ascending reports whether an OrderDirection means ascending. The default
// (nil) is ASC, matching the original OrderDirection default.
func ascending(d *OrderDirection) bool {
	return d == nil || *d != OrderDirectionDesc
}

// Order-input mappers resolve each GraphQL *OrderInput to a store column set and
// direction. Defaults follow the Kotlin *Order.DEFAULT (ASC + the ID-ish field;
// ASC + VALUE for categorical values). Every SDL enum value is handled.

func productOrder(in *ProductOrderInput) (store.OrderColumns, bool) {
	if in == nil {
		return store.ProductOrderByID, true
	}
	col := store.ProductOrderByID
	if in.Field != nil && *in.Field == ProductOrderFieldInternalName {
		col = store.ProductOrderByInternalName
	}
	return col, ascending(in.Direction)
}

func categoryOrder(in *CategoryOrderInput) (store.OrderColumns, bool) {
	if in == nil {
		return store.CategoryOrderByID, true
	}
	col := store.CategoryOrderByID
	if in.Field != nil && *in.Field == CategoryOrderFieldName {
		col = store.CategoryOrderByName
	}
	return col, ascending(in.Direction)
}

func productVariantOrder(in *ProductVariantOrderInput) (store.OrderColumns, bool) {
	if in == nil {
		return store.ProductVariantOrderByID, true
	}
	// Only ID is defined for ProductVariant.
	return store.ProductVariantOrderByID, ascending(in.Direction)
}

func productVariantVersionOrder(in *ProductVariantVersionOrderInput) (store.OrderColumns, bool) {
	if in == nil {
		return store.ProductVariantVersionOrderByID, true
	}
	col := store.ProductVariantVersionOrderByID
	if in.Field != nil {
		switch *in.Field {
		case ProductVariantVersionOrderFieldVersion:
			col = store.ProductVariantVersionOrderByVersion
		case ProductVariantVersionOrderFieldCreatedAt:
			col = store.ProductVariantVersionOrderByCreatedAt
		}
	}
	return col, ascending(in.Direction)
}

func characteristicOrder(in *CategoryCharacteristicOrderInput) (store.OrderColumns, bool) {
	if in == nil {
		return store.CategoryCharacteristicOrderByID, true
	}
	// Only ID is defined for CategoryCharacteristic.
	return store.CategoryCharacteristicOrderByID, ascending(in.Direction)
}

func characteristicValueOrder(in *CategoryCharacteristicValueOrderInput) (store.OrderColumns, bool) {
	if in == nil {
		return store.CategoryCharacteristicValueOrderByID, true
	}
	// Only ID is defined for CategoryCharacteristicValue.
	return store.CategoryCharacteristicValueOrderByID, ascending(in.Direction)
}

func categoricalValueOrder(in *CategoricalCategoryCharacteristicValueOrderInput) (store.OrderColumns, bool) {
	if in == nil {
		return store.CategoricalValueOrderByValue, true
	}
	// Only VALUE is defined.
	return store.CategoricalValueOrderByValue, ascending(in.Direction)
}

func commonOrder(in *CommonOrderInput) (store.OrderColumns, bool) {
	if in == nil {
		return store.MediaOrderByID, true
	}
	// Only ID is defined (CommonOrderField).
	return store.MediaOrderByID, ascending(in.Direction)
}
