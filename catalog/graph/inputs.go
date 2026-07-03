package graph

import (
	"misarch/catalog/events"
	"misarch/catalog/store"
)

// This file converts GraphQL input types to store-level inputs and store
// mutation results to event payloads, keeping the resolvers terse and the store
// free of graph-package imports.

// filterVisible extracts the ProductFilterInput.isPubliclyVisible value (nil =
// no filter).
func filterVisible(f *ProductFilterInput) *bool {
	if f == nil {
		return nil
	}
	return f.IsPubliclyVisible
}

// variantFilterVisible extracts the ProductVariantFilterInput.isPubliclyVisible
// value (nil = no filter).
func variantFilterVisible(f *ProductVariantFilterInput) *bool {
	if f == nil {
		return nil
	}
	return f.IsPubliclyVisible
}

// toNamedInputs converts categorical characteristic inputs (no unit).
func toNamedInputs(in []CategoricalCategoryCharacteristicInput) []store.NamedInput {
	out := make([]store.NamedInput, len(in))
	for i, c := range in {
		out[i] = store.NamedInput{Name: c.Name, Description: c.Description}
	}
	return out
}

// toNumericalNamedInputs converts numerical characteristic inputs (with unit).
func toNumericalNamedInputs(in []NumericalCategoryCharacteristicInput) []store.NamedInput {
	out := make([]store.NamedInput, len(in))
	for i, n := range in {
		unit := n.Unit
		out[i] = store.NamedInput{Name: n.Name, Description: n.Description, Unit: &unit}
	}
	return out
}

// toVariantInput converts a ProductVariantInput (default variant of a product).
func toVariantInput(in *ProductVariantInput) store.VariantInput {
	return store.VariantInput{
		IsPubliclyVisible: in.IsPubliclyVisible,
		InitialVersion:    toVersionInput(in.InitialVersion),
	}
}

// toVersionInput converts a ProductVariantVersionInput.
func toVersionInput(in *ProductVariantVersionInput) store.VersionInput {
	return store.VersionInput{
		Name:                 in.Name,
		Description:          in.Description,
		RetailPrice:          in.RetailPrice,
		CanBeReturnedForDays: in.CanBeReturnedForDays,
		TaxRateID:            in.TaxRateID,
		Weight:               in.Weight,
		MediaIDs:             in.MediaIds,
		CategoricalValues:    toCategoricalValueInputs(in.CategoricalCharacteristicValues),
		NumericalValues:      toNumericalValueInputs(in.NumericalCharacteristicValues),
	}
}

// toVersionInputFromCreate converts a CreateProductVariantVersionInput (same
// version fields plus a top-level productVariantId handled by the caller).
func toVersionInputFromCreate(in CreateProductVariantVersionInput) store.VersionInput {
	return store.VersionInput{
		Name:                 in.Name,
		Description:          in.Description,
		RetailPrice:          in.RetailPrice,
		CanBeReturnedForDays: in.CanBeReturnedForDays,
		TaxRateID:            in.TaxRateID,
		Weight:               in.Weight,
		MediaIDs:             in.MediaIds,
		CategoricalValues:    toCategoricalValueInputs(in.CategoricalCharacteristicValues),
		NumericalValues:      toNumericalValueInputs(in.NumericalCharacteristicValues),
	}
}

func toCategoricalValueInputs(in []CategoricalCategoryCharacteristicValueInput) []store.CharacteristicValueInput {
	out := make([]store.CharacteristicValueInput, len(in))
	for i, v := range in {
		out[i] = store.CharacteristicValueInput{CharacteristicID: v.CharacteristicID, Value: v.Value}
	}
	return out
}

func toNumericalValueInputs(in []NumericalCategoryCharacteristicValueInput) []store.NumericalValueInput {
	out := make([]store.NumericalValueInput, len(in))
	for i, v := range in {
		out[i] = store.NumericalValueInput{CharacteristicID: v.CharacteristicID, Value: v.Value}
	}
	return out
}

// variantCreatedEvent builds the product-variant/created payload from a created
// variant.
func variantCreatedEvent(v store.ProductVariant) events.CreatedProductVariant {
	return events.CreatedProductVariant{
		ID:                v.ID,
		ProductID:         v.ProductID,
		CurrentVersionID:  v.CurrentVersion,
		IsPubliclyVisible: v.IsPubliclyVisible,
	}
}

// versionCreatedEvent builds the product-variant-version/created payload from a
// created version, formatting createdAt as the ISO-8601 offset string and using
// the deduplicated media id list.
func versionCreatedEvent(c store.CreatedVersion) events.ProductVariantVersion {
	v := c.Version
	return events.ProductVariantVersion{
		ID:                   v.ID,
		Name:                 v.Name,
		Description:          v.Description,
		Version:              v.Version,
		RetailPrice:          v.RetailPrice,
		CreatedAt:            events.FormatCreatedAt(v.CreatedAt),
		CanBeReturnedForDays: v.CanBeReturnedForDays,
		ProductVariantID:     v.ProductVariantID,
		TaxRateID:            v.TaxRateID,
		Weight:               v.Weight,
		MediaIDs:             c.MediaIDs,
	}
}
