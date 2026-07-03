package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The Get* methods load a single owned entity by id. The original resolved
// these through batched data loaders whose `entities[it]!!` non-null assertion
// throws on an unknown id, surfacing a GraphQL error with data:null. We match
// that by returning an error for a missing row.

// GetDiscount loads a discount by id, erroring if it does not exist.
func (s *Store) GetDiscount(ctx context.Context, id uuid.UUID) (Discount, error) {
	var d Discount
	err := s.pool.QueryRow(ctx,
		"SELECT "+discountColumns+" FROM discountentity WHERE discountentity.id = $1", id,
	).Scan(&d.ID, &d.Discount, &d.MaxUsagesPerUser, &d.ValidFrom, &d.ValidUntil, &d.MinOrderAmount)
	if errors.Is(err, pgx.ErrNoRows) {
		return Discount{}, fmt.Errorf("discount with id %s not found", id)
	}
	if err != nil {
		return Discount{}, fmt.Errorf("get discount %s: %w", id, err)
	}
	return d, nil
}

// GetCoupon loads a coupon by id, erroring if it does not exist.
func (s *Store) GetCoupon(ctx context.Context, id uuid.UUID) (Coupon, error) {
	c, err := getCoupon(ctx, s.pool, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Coupon{}, fmt.Errorf("coupon with id %s not found", id)
	}
	if err != nil {
		return Coupon{}, fmt.Errorf("get coupon %s: %w", id, err)
	}
	return c, nil
}

// getCoupon loads a coupon by id on the given querier (pool or tx), returning
// pgx.ErrNoRows if absent.
func getCoupon(ctx context.Context, q querier, id uuid.UUID) (Coupon, error) {
	var c Coupon
	err := q.QueryRow(ctx,
		"SELECT "+couponColumns+" FROM couponentity WHERE couponentity.id = $1", id,
	).Scan(&c.ID, &c.Usages, &c.MaxUsages, &c.ValidFrom, &c.ValidUntil, &c.Code, &c.DiscountID)
	return c, err
}

// GetDiscountUsage loads a discount usage by id, erroring if it does not exist.
func (s *Store) GetDiscountUsage(ctx context.Context, id uuid.UUID) (DiscountUsage, error) {
	var u DiscountUsage
	err := s.pool.QueryRow(ctx,
		"SELECT "+discountUsageColumns+" FROM discountusageentity WHERE discountusageentity.id = $1", id,
	).Scan(&u.ID, &u.DiscountID, &u.UserID, &u.Usages)
	if errors.Is(err, pgx.ErrNoRows) {
		return DiscountUsage{}, fmt.Errorf("discount usage with id %s not found", id)
	}
	if err != nil {
		return DiscountUsage{}, fmt.Errorf("get discount usage %s: %w", id, err)
	}
	return u, nil
}
