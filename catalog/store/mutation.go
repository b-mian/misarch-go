package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// VersionInput is the store-level view of a product-variant-version to create.
// It flattens graphql.ProductVariantVersionInput / CreateProductVariantVersionInput
// so the store need not import the graph package. CategoricalValues /
// NumericalValues are the characteristic-value upserts; MediaIDs is the raw
// (possibly duplicated / unordered) media list from the input.
type VersionInput struct {
	Name                 string
	Description          string
	RetailPrice          int
	CanBeReturnedForDays *int
	TaxRateID            uuid.UUID
	Weight               float64
	MediaIDs             []uuid.UUID
	CategoricalValues    []CharacteristicValueInput
	NumericalValues      []NumericalValueInput
}

// CharacteristicValueInput is one categorical value upsert.
type CharacteristicValueInput struct {
	CharacteristicID uuid.UUID
	Value            string
}

// NumericalValueInput is one numerical value upsert.
type NumericalValueInput struct {
	CharacteristicID uuid.UUID
	Value            float64
}

// VariantInput is the store-level view of a product-variant to create.
type VariantInput struct {
	IsPubliclyVisible bool
	InitialVersion    VersionInput
}

// CreatedVersion bundles a freshly inserted version with the deduplicated,
// order-preserving media id list actually linked to it (the shape the
// product-variant-version/created event needs).
type CreatedVersion struct {
	Version  ProductVariantVersion
	MediaIDs []uuid.UUID
}

// CreateProduct performs the full product mini-saga in one transaction: insert
// product → check + link categories → create default variant+version → back-patch
// variant.currentversion and product.defaultvariantid. It returns the product,
// its variant, and the initial version (with linked media ids) plus the
// deduplicated category id list, so the resolver can publish the three events in
// order after commit. Any validation error rolls everything back.
func (s *Store) CreateProduct(
	ctx context.Context, internalName string, isPubliclyVisible bool,
	categoryIDs []uuid.UUID, variant VariantInput, now time.Time,
) (product Product, createdVariant ProductVariant, created CreatedVersion, dedupedCategoryIDs []uuid.UUID, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Product{}, ProductVariant{}, CreatedVersion{}, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Insert product row (defaultvariantid NULL initially).
	var productID uuid.UUID
	if err = tx.QueryRow(ctx,
		"INSERT INTO productentity (internalname, ispubliclyvisible, defaultvariantid) VALUES ($1, $2, NULL) RETURNING id",
		internalName, isPubliclyVisible,
	).Scan(&productID); err != nil {
		return Product{}, ProductVariant{}, CreatedVersion{}, nil, fmt.Errorf("insert product: %w", err)
	}

	// 2. Check each category exists (in order), then link. Duplicate ids in the
	// list violate the unique (productid, categoryid) constraint → DB error.
	if err = addCategories(ctx, tx, productID, categoryIDs); err != nil {
		return Product{}, ProductVariant{}, CreatedVersion{}, nil, err
	}

	// 3. Create the default variant + its initial version.
	createdVariant, created, err = createVariantInternal(ctx, tx, variant, productID, now)
	if err != nil {
		return Product{}, ProductVariant{}, CreatedVersion{}, nil, err
	}

	// 4. Back-patch product.defaultvariantid.
	if _, err = tx.Exec(ctx,
		"UPDATE productentity SET defaultvariantid = $1 WHERE id = $2", createdVariant.ID, productID,
	); err != nil {
		return Product{}, ProductVariant{}, CreatedVersion{}, nil, fmt.Errorf("set default variant: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return Product{}, ProductVariant{}, CreatedVersion{}, nil, err
	}

	product = Product{
		ID:                productID,
		InternalName:      internalName,
		IsPubliclyVisible: isPubliclyVisible,
		DefaultVariantID:  createdVariant.ID,
	}
	return product, createdVariant, created, dedupeUUIDs(categoryIDs), nil
}

// CreateProductVariant creates a variant (and its initial version) for an
// existing product; it does NOT touch the product's defaultVariant. Returns the
// variant and created version for the two events published after commit.
func (s *Store) CreateProductVariant(
	ctx context.Context, productID uuid.UUID, variant VariantInput, now time.Time,
) (ProductVariant, CreatedVersion, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ProductVariant{}, CreatedVersion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	exists, err := existsByID(ctx, tx, "productentity", productID)
	if err != nil {
		return ProductVariant{}, CreatedVersion{}, err
	}
	if !exists {
		return ProductVariant{}, CreatedVersion{}, fmt.Errorf("Product with id %s does not exist.", productID)
	}

	createdVariant, created, err := createVariantInternal(ctx, tx, variant, productID, now)
	if err != nil {
		return ProductVariant{}, CreatedVersion{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return ProductVariant{}, CreatedVersion{}, err
	}
	return createdVariant, created, nil
}

// CreateProductVariantVersion creates a new version for an existing variant and
// repoints the variant's currentversion to it (no product-variant/updated
// event). Returns the created version for the single event published after
// commit.
func (s *Store) CreateProductVariantVersion(
	ctx context.Context, variantID uuid.UUID, in VersionInput, now time.Time,
) (CreatedVersion, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CreatedVersion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	exists, err := existsByID(ctx, tx, "productvariantentity", variantID)
	if err != nil {
		return CreatedVersion{}, err
	}
	if !exists {
		return CreatedVersion{}, fmt.Errorf("Product variant with id %s does not exist.", variantID)
	}

	created, err := createVersionInternal(ctx, tx, in, variantID, now)
	if err != nil {
		return CreatedVersion{}, err
	}

	// Creating a version always repoints currentVersion.
	if _, err := tx.Exec(ctx,
		"UPDATE productvariantentity SET currentversion = $1 WHERE id = $2", created.Version.ID, variantID,
	); err != nil {
		return CreatedVersion{}, fmt.Errorf("set current version: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return CreatedVersion{}, err
	}
	return created, nil
}

// createVariantInternal inserts a variant (currentversion NULL), creates its
// initial version, then back-patches currentversion — the shared flow used by
// CreateProduct and CreateProductVariant.
func createVariantInternal(
	ctx context.Context, tx pgx.Tx, in VariantInput, productID uuid.UUID, now time.Time,
) (ProductVariant, CreatedVersion, error) {
	var variantID uuid.UUID
	if err := tx.QueryRow(ctx,
		"INSERT INTO productvariantentity (ispubliclyvisible, productid, currentversion) VALUES ($1, $2, NULL) RETURNING id",
		in.IsPubliclyVisible, productID,
	).Scan(&variantID); err != nil {
		return ProductVariant{}, CreatedVersion{}, fmt.Errorf("insert product variant: %w", err)
	}

	created, err := createVersionInternal(ctx, tx, in.InitialVersion, variantID, now)
	if err != nil {
		return ProductVariant{}, CreatedVersion{}, err
	}

	if _, err := tx.Exec(ctx,
		"UPDATE productvariantentity SET currentversion = $1 WHERE id = $2", created.Version.ID, variantID,
	); err != nil {
		return ProductVariant{}, CreatedVersion{}, fmt.Errorf("set current version: %w", err)
	}

	variant := ProductVariant{
		ID:                variantID,
		IsPubliclyVisible: in.IsPubliclyVisible,
		ProductID:         productID,
		CurrentVersion:    created.Version.ID,
	}
	return variant, created, nil
}

// createVersionInternal implements the version flow shared by all three create*
// mutations: check tax rate → check medias → MAX(version)+1 → insert version →
// upsert characteristic values → link deduplicated medias.
func createVersionInternal(
	ctx context.Context, tx pgx.Tx, in VersionInput, variantID uuid.UUID, now time.Time,
) (CreatedVersion, error) {
	// Tax rate must exist (NOTE: this message has NO trailing period — the only
	// such message in the service).
	taxExists, err := existsByID(ctx, tx, "taxrateentity", in.TaxRateID)
	if err != nil {
		return CreatedVersion{}, err
	}
	if !taxExists {
		return CreatedVersion{}, fmt.Errorf("Tax rate with id %s does not exist", in.TaxRateID)
	}

	// Each media must exist (checked in list order).
	for _, mediaID := range in.MediaIDs {
		exists, err := existsByID(ctx, tx, "mediaentity", mediaID)
		if err != nil {
			return CreatedVersion{}, err
		}
		if !exists {
			return CreatedVersion{}, fmt.Errorf("Media with id %s does not exist.", mediaID)
		}
	}

	// version = MAX(version)+1 for this variant, or 1 if none exist yet.
	var maxVersion *int
	if err := tx.QueryRow(ctx,
		"SELECT MAX(version) FROM productvariantversionentity WHERE productvariantid = $1", variantID,
	).Scan(&maxVersion); err != nil {
		return CreatedVersion{}, fmt.Errorf("max version: %w", err)
	}
	version := 1
	if maxVersion != nil {
		version = *maxVersion + 1
	}

	var v ProductVariantVersion
	if err := tx.QueryRow(ctx,
		`INSERT INTO productvariantversionentity
		 (name, description, version, retailprice, createdat, canbereturnedfordays, productvariantid, taxrateid, weight)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING `+productVariantVersionColumns,
		in.Name, in.Description, version, in.RetailPrice, now, in.CanBeReturnedForDays, variantID, in.TaxRateID, in.Weight,
	).Scan(&v.ID, &v.Name, &v.Description, &v.Version, &v.RetailPrice, &v.CreatedAt,
		&v.CanBeReturnedForDays, &v.ProductVariantID, &v.TaxRateID, &v.Weight); err != nil {
		return CreatedVersion{}, fmt.Errorf("insert product variant version: %w", err)
	}

	// Upsert the characteristic values (validation + writes).
	if err := upsertCharacteristicValues(ctx, tx, v.ID, in.CategoricalValues, in.NumericalValues); err != nil {
		return CreatedVersion{}, err
	}

	// Link the deduplicated media ids (first-occurrence order preserved).
	mediaIDs := dedupeUUIDs(in.MediaIDs)
	for _, mediaID := range mediaIDs {
		if _, err := tx.Exec(ctx,
			"INSERT INTO productvariantversiontomediaentity (productvariantversionid, mediaid) VALUES ($1, $2)",
			v.ID, mediaID,
		); err != nil {
			return CreatedVersion{}, fmt.Errorf("link media: %w", err)
		}
	}

	return CreatedVersion{Version: v, MediaIDs: mediaIDs}, nil
}

// addCategories checks each category exists (in order, erroring on the first
// missing one) then inserts the producttocategory link rows.
func addCategories(ctx context.Context, tx pgx.Tx, productID uuid.UUID, categoryIDs []uuid.UUID) error {
	for _, categoryID := range categoryIDs {
		exists, err := existsByID(ctx, tx, "categoryentity", categoryID)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("Category with id %s does not exist.", categoryID)
		}
	}
	for _, categoryID := range categoryIDs {
		if _, err := tx.Exec(ctx,
			"INSERT INTO producttocategoryentity (productid, categoryid) VALUES ($1, $2)",
			productID, categoryID,
		); err != nil {
			return fmt.Errorf("link category: %w", err)
		}
	}
	return nil
}

// existsByID reports whether a row with the given id exists in table (table is
// a trusted constant, never user input).
func existsByID(ctx context.Context, q pgx.Tx, table string, id uuid.UUID) (bool, error) {
	var exists bool
	if err := q.QueryRow(ctx,
		fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE id = $1)", table), id,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("exists %s: %w", table, err)
	}
	return exists, nil
}

// dedupeUUIDs returns the distinct ids preserving first-occurrence order (the
// Kotlin Set/`.distinct()` semantics used for mediaIds and categoryIds in the
// event payloads and media links).
func dedupeUUIDs(ids []uuid.UUID) []uuid.UUID {
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
