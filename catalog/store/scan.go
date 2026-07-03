package store

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// scanProducts collects productentity rows.
func scanProducts(rows pgx.Rows) ([]Product, error) {
	defer rows.Close()
	var out []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.InternalName, &p.IsPubliclyVisible, &p.DefaultVariantID); err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// scanProductVariants collects productvariantentity rows.
func scanProductVariants(rows pgx.Rows) ([]ProductVariant, error) {
	defer rows.Close()
	var out []ProductVariant
	for rows.Next() {
		var v ProductVariant
		if err := rows.Scan(&v.ID, &v.IsPubliclyVisible, &v.ProductID, &v.CurrentVersion); err != nil {
			return nil, fmt.Errorf("scan product variant: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// scanProductVariantVersions collects productvariantversionentity rows.
func scanProductVariantVersions(rows pgx.Rows) ([]ProductVariantVersion, error) {
	defer rows.Close()
	var out []ProductVariantVersion
	for rows.Next() {
		var v ProductVariantVersion
		if err := rows.Scan(&v.ID, &v.Name, &v.Description, &v.Version, &v.RetailPrice, &v.CreatedAt,
			&v.CanBeReturnedForDays, &v.ProductVariantID, &v.TaxRateID, &v.Weight); err != nil {
			return nil, fmt.Errorf("scan product variant version: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// scanCategories collects categoryentity rows.
func scanCategories(rows pgx.Rows) ([]Category, error) {
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Description); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// scanCharacteristics collects categorycharacteristicentity rows.
func scanCharacteristics(rows pgx.Rows) ([]CategoryCharacteristic, error) {
	defer rows.Close()
	var out []CategoryCharacteristic
	for rows.Next() {
		var c CategoryCharacteristic
		if err := rows.Scan(&c.ID, &c.Discriminator, &c.Name, &c.Description, &c.Unit, &c.CategoryID); err != nil {
			return nil, fmt.Errorf("scan category characteristic: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// scanCharacteristicValues collects categorycharacteristicvalueentity rows.
func scanCharacteristicValues(rows pgx.Rows) ([]CategoryCharacteristicValue, error) {
	defer rows.Close()
	var out []CategoryCharacteristicValue
	for rows.Next() {
		var v CategoryCharacteristicValue
		if err := rows.Scan(&v.ID, &v.Discriminator, &v.StringValue, &v.DoubleValue,
			&v.CategoryCharacteristicID, &v.ProductVariantVersionID); err != nil {
			return nil, fmt.Errorf("scan category characteristic value: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// scanMedias collects mediaentity rows (id only).
func scanMedias(rows pgx.Rows) ([]Media, error) {
	defer rows.Close()
	var out []Media
	for rows.Next() {
		var m Media
		if err := rows.Scan(&m.ID); err != nil {
			return nil, fmt.Errorf("scan media: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// scanCategoricalValues collects DISTINCT stringvalue rows, wrapping each with
// the owning characteristic id.
func scanCategoricalValues(rows pgx.Rows, characteristicID uuid.UUID) ([]CategoricalValue, error) {
	defer rows.Close()
	var out []CategoricalValue
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan categorical value: %w", err)
		}
		out = append(out, CategoricalValue{CharacteristicID: characteristicID, Value: value})
	}
	return out, rows.Err()
}
