package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// FindProductVariantVersionsByIDs loads the pvv rows with the given ids (used
// by calculateQuantityAndWeight to validate + weigh). Missing ids are absent
// from the result; the caller checks completeness.
func (s *Store) FindProductVariantVersionsByIDs(ctx context.Context, ids []uuid.UUID) ([]ProductVariantVersion, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		"SELECT id, weight FROM productvariantversionentity WHERE id = ANY($1)", ids,
	)
	if err != nil {
		return nil, fmt.Errorf("find product variant versions by ids: %w", err)
	}
	defer rows.Close()
	var out []ProductVariantVersion
	for rows.Next() {
		var p ProductVariantVersion
		if err := rows.Scan(&p.ID, &p.Weight); err != nil {
			return nil, fmt.Errorf("scan product variant version: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateProductVariantVersion inserts a pvv read-model row {id, weight}. Plain
// INSERT with no ON CONFLICT — duplicate delivery → PK violation → 500 → Dapr
// retry, matching the reference.
func (s *Store) CreateProductVariantVersion(ctx context.Context, id uuid.UUID, weight float64) error {
	_, err := s.pool.Exec(ctx,
		"INSERT INTO productvariantversionentity (id, weight) VALUES ($1, $2)",
		id, weight,
	)
	if err != nil {
		return fmt.Errorf("create product variant version %s: %w", id, err)
	}
	return nil
}
