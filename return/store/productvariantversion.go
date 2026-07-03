package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// findProductVariantVersionsByIDs loads pvv rows for the given ids (IN-list),
// mirroring productVariantVersionRepository.findAllById. Because of BUG-1 this
// table is empty in production, so this returns nothing for real order items.
func findProductVariantVersionsByIDs(ctx context.Context, q querier, ids []uuid.UUID) ([]ProductVariantVersion, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx,
		"SELECT id, canbereturnedfordays FROM productvariantversionentity WHERE id = ANY($1)", ids)
	if err != nil {
		return nil, fmt.Errorf("find product variant versions: %w", err)
	}
	defer rows.Close()
	var out []ProductVariantVersion
	for rows.Next() {
		var p ProductVariantVersion
		if err := rows.Scan(&p.ID, &p.CanBeReturnedForDays); err != nil {
			return nil, fmt.Errorf("scan product variant version: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateProductVariantVersion inserts a pvv from a
// catalog/product-variant-version/created event, mirroring
// ProductVariantVersionRepository.createProductVariantVersion (a raw,
// non-idempotent INSERT).
//
// NOTE (BUG-1): the original never subscribes to that topic, so this is never
// invoked in production. It is provided for completeness / for the eventual
// fix, but the subscription is intentionally NOT registered (see events and
// main.go) to preserve bug-for-bug behavior.
func (s *Store) CreateProductVariantVersion(ctx context.Context, id uuid.UUID, canBeReturnedForDays *int) error {
	if _, err := s.pool.Exec(ctx,
		"INSERT INTO productvariantversionentity (id, canbereturnedfordays) VALUES ($1, $2)", id, canBeReturnedForDays,
	); err != nil {
		return fmt.Errorf("create product variant version %s: %w", id, err)
	}
	return nil
}
