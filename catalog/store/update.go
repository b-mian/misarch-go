package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UpdateProduct applies a partial update to a product and returns the updated
// product together with its (order-preserving) category id list for the
// product/updated event, which always fires.
//
// Steps mirror ProductService.updateProduct: load product (missing → error);
// patch isPubliclyVisible / internalName if present; if defaultVariantId is
// present, load that variant (missing → error) and verify it belongs to the
// product (else error) before setting it; save; fetch category ids.
func (s *Store) UpdateProduct(
	ctx context.Context, id uuid.UUID, isPubliclyVisible *bool, internalName *string, defaultVariantID *uuid.UUID,
) (Product, []uuid.UUID, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Product{}, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Load the product (locks nothing extra; parity with findById).
	var p Product
	err = tx.QueryRow(ctx,
		"SELECT "+productColumns+" FROM productentity WHERE id = $1", id,
	).Scan(&p.ID, &p.InternalName, &p.IsPubliclyVisible, &p.DefaultVariantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, nil, fmt.Errorf("product with id %s not found", id)
	}
	if err != nil {
		return Product{}, nil, fmt.Errorf("load product %s: %w", id, err)
	}

	if isPubliclyVisible != nil {
		p.IsPubliclyVisible = *isPubliclyVisible
	}
	if internalName != nil {
		p.InternalName = *internalName
	}
	if defaultVariantID != nil {
		var variantProductID uuid.UUID
		err = tx.QueryRow(ctx,
			"SELECT productid FROM productvariantentity WHERE id = $1", *defaultVariantID,
		).Scan(&variantProductID)
		if errors.Is(err, pgx.ErrNoRows) {
			return Product{}, nil, fmt.Errorf("product variant with id %s not found", *defaultVariantID)
		}
		if err != nil {
			return Product{}, nil, fmt.Errorf("load variant %s: %w", *defaultVariantID, err)
		}
		if variantProductID != p.ID {
			return Product{}, nil, fmt.Errorf("Variant with id %s does not belong to product with id %s.", *defaultVariantID, p.ID)
		}
		p.DefaultVariantID = *defaultVariantID
	}

	if _, err = tx.Exec(ctx,
		"UPDATE productentity SET ispubliclyvisible = $1, internalname = $2, defaultvariantid = $3 WHERE id = $4",
		p.IsPubliclyVisible, p.InternalName, p.DefaultVariantID, p.ID,
	); err != nil {
		return Product{}, nil, fmt.Errorf("update product: %w", err)
	}

	categoryIDs, err := categoryIDsForProduct(ctx, tx, p.ID)
	if err != nil {
		return Product{}, nil, err
	}

	if err = tx.Commit(ctx); err != nil {
		return Product{}, nil, err
	}
	return p, categoryIDs, nil
}

// UpdateProductVariant applies a partial update (isPubliclyVisible) to a variant
// and returns it for the product-variant/updated event, which always fires.
func (s *Store) UpdateProductVariant(
	ctx context.Context, id uuid.UUID, isPubliclyVisible *bool,
) (ProductVariant, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ProductVariant{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var v ProductVariant
	err = tx.QueryRow(ctx,
		"SELECT "+productVariantColumns+" FROM productvariantentity WHERE id = $1", id,
	).Scan(&v.ID, &v.IsPubliclyVisible, &v.ProductID, &v.CurrentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProductVariant{}, fmt.Errorf("product variant with id %s not found", id)
	}
	if err != nil {
		return ProductVariant{}, fmt.Errorf("load product variant %s: %w", id, err)
	}

	if isPubliclyVisible != nil {
		v.IsPubliclyVisible = *isPubliclyVisible
	}

	if _, err = tx.Exec(ctx,
		"UPDATE productvariantentity SET ispubliclyvisible = $1 WHERE id = $2", v.IsPubliclyVisible, v.ID,
	); err != nil {
		return ProductVariant{}, fmt.Errorf("update product variant: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return ProductVariant{}, err
	}
	return v, nil
}

// categoryIDsForProduct returns the category ids linked to a product, in row
// (insertion) order — matching ProductToCategoryRepository.findByProductId used
// to build the event's categoryIds.
func categoryIDsForProduct(ctx context.Context, q querier, productID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx,
		"SELECT categoryid FROM producttocategoryentity WHERE productid = $1 ORDER BY id", productID)
	if err != nil {
		return nil, fmt.Errorf("category ids for product: %w", err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan category id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
