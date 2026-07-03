package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Column projections for full-row reads (physical lowercase column order).
const (
	productColumns               = "id, internalname, ispubliclyvisible, defaultvariantid"
	productVariantColumns        = "id, ispubliclyvisible, productid, currentversion"
	productVariantVersionColumns = "id, name, description, version, retailprice, createdat, canbereturnedfordays, productvariantid, taxrateid, weight"
	categoryColumns              = "id, name, description"
	characteristicColumns        = "id, discriminator, name, description, unit, categoryid"
	characteristicValueColumns   = "id, discriminator, stringvalue, doublevalue, categorycharacteristicid, productvariantversionid"
)

// The original data loaders assert the loaded entity is non-null; an unknown id
// surfaced a NullPointerException rendered as a GraphQL error. The Go port
// returns a clean not-found error instead (message parity is impossible — see
// the port's deviations note).

// GetProduct loads a product by id.
func (s *Store) GetProduct(ctx context.Context, id uuid.UUID) (Product, error) {
	var p Product
	err := s.pool.QueryRow(ctx,
		"SELECT "+productColumns+" FROM productentity WHERE id = $1", id,
	).Scan(&p.ID, &p.InternalName, &p.IsPubliclyVisible, &p.DefaultVariantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, fmt.Errorf("product with id %s not found", id)
	}
	if err != nil {
		return Product{}, fmt.Errorf("get product %s: %w", id, err)
	}
	return p, nil
}

// GetProductVariant loads a product variant by id.
func (s *Store) GetProductVariant(ctx context.Context, id uuid.UUID) (ProductVariant, error) {
	var v ProductVariant
	err := s.pool.QueryRow(ctx,
		"SELECT "+productVariantColumns+" FROM productvariantentity WHERE id = $1", id,
	).Scan(&v.ID, &v.IsPubliclyVisible, &v.ProductID, &v.CurrentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProductVariant{}, fmt.Errorf("product variant with id %s not found", id)
	}
	if err != nil {
		return ProductVariant{}, fmt.Errorf("get product variant %s: %w", id, err)
	}
	return v, nil
}

// GetProductVariantVersion loads a product variant version by id.
func (s *Store) GetProductVariantVersion(ctx context.Context, id uuid.UUID) (ProductVariantVersion, error) {
	var v ProductVariantVersion
	err := s.pool.QueryRow(ctx,
		"SELECT "+productVariantVersionColumns+" FROM productvariantversionentity WHERE id = $1", id,
	).Scan(&v.ID, &v.Name, &v.Description, &v.Version, &v.RetailPrice, &v.CreatedAt,
		&v.CanBeReturnedForDays, &v.ProductVariantID, &v.TaxRateID, &v.Weight)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProductVariantVersion{}, fmt.Errorf("product variant version with id %s not found", id)
	}
	if err != nil {
		return ProductVariantVersion{}, fmt.Errorf("get product variant version %s: %w", id, err)
	}
	return v, nil
}

// GetCategory loads a category by id.
func (s *Store) GetCategory(ctx context.Context, id uuid.UUID) (Category, error) {
	var c Category
	err := s.pool.QueryRow(ctx,
		"SELECT "+categoryColumns+" FROM categoryentity WHERE id = $1", id,
	).Scan(&c.ID, &c.Name, &c.Description)
	if errors.Is(err, pgx.ErrNoRows) {
		return Category{}, fmt.Errorf("category with id %s not found", id)
	}
	if err != nil {
		return Category{}, fmt.Errorf("get category %s: %w", id, err)
	}
	return c, nil
}

// GetCharacteristic loads a category characteristic by id (either discriminator).
func (s *Store) GetCharacteristic(ctx context.Context, id uuid.UUID) (CategoryCharacteristic, error) {
	c, err := scanCharacteristicRow(s.pool.QueryRow(ctx,
		"SELECT "+characteristicColumns+" FROM categorycharacteristicentity WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return CategoryCharacteristic{}, fmt.Errorf("category characteristic with id %s not found", id)
	}
	if err != nil {
		return CategoryCharacteristic{}, fmt.Errorf("get category characteristic %s: %w", id, err)
	}
	return c, nil
}

// GetCategoricalCharacteristic loads a characteristic by id and requires it to
// be CATEGORICAL. The original's typed data loader cast the row to
// CategoricalCategoryCharacteristic, so a numerical id raised a
// ClassCastException surfaced as an error; a not-categorical id errors here.
func (s *Store) GetCategoricalCharacteristic(ctx context.Context, id uuid.UUID) (CategoryCharacteristic, error) {
	c, err := s.GetCharacteristic(ctx, id)
	if err != nil {
		return CategoryCharacteristic{}, err
	}
	if c.Discriminator != DiscriminatorCategorical {
		return CategoryCharacteristic{}, fmt.Errorf("category characteristic with id %s is not categorical", id)
	}
	return c, nil
}

// GetNumericalCharacteristic loads a characteristic by id and requires it to be
// NUMERICAL (see GetCategoricalCharacteristic for the parity rationale).
func (s *Store) GetNumericalCharacteristic(ctx context.Context, id uuid.UUID) (CategoryCharacteristic, error) {
	c, err := s.GetCharacteristic(ctx, id)
	if err != nil {
		return CategoryCharacteristic{}, err
	}
	if c.Discriminator != DiscriminatorNumerical {
		return CategoryCharacteristic{}, fmt.Errorf("category characteristic with id %s is not numerical", id)
	}
	return c, nil
}

// GetTaxRateStub loads the id-only tax rate mirror row. The
// productvariantversionentity.taxrateid FK guarantees the row exists; this is a
// defensive lookup matching the original's TaxRate data loader (which returned
// only {id}).
func (s *Store) GetTaxRateStub(ctx context.Context, id uuid.UUID) (TaxRate, error) {
	var t TaxRate
	err := s.pool.QueryRow(ctx, "SELECT id FROM taxrateentity WHERE id = $1", id).Scan(&t.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaxRate{}, fmt.Errorf("tax rate with id %s not found", id)
	}
	if err != nil {
		return TaxRate{}, fmt.Errorf("get tax rate %s: %w", id, err)
	}
	return t, nil
}

// scanCharacteristicRow scans one categorycharacteristicentity row.
func scanCharacteristicRow(row pgx.Row) (CategoryCharacteristic, error) {
	var c CategoryCharacteristic
	err := row.Scan(&c.ID, &c.Discriminator, &c.Name, &c.Description, &c.Unit, &c.CategoryID)
	return c, err
}
