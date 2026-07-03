package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateCouponParams is the input to CreateCoupon.
type CreateCouponParams struct {
	Code       string
	DiscountID uuid.UUID
	MaxUsages  *int
	ValidFrom  time.Time
	ValidUntil time.Time
}

// CreateCoupon validates the referenced discount exists and the code is unique,
// then inserts the coupon (usages defaults to 0) and returns it. Runs in one
// transaction.
func (s *Store) CreateCoupon(ctx context.Context, in CreateCouponParams) (Coupon, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Coupon{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	exists, err := existsByID(ctx, tx, "discountentity", in.DiscountID)
	if err != nil {
		return Coupon{}, fmt.Errorf("discount exists: %w", err)
	}
	if !exists {
		return Coupon{}, fmt.Errorf("Discount with id %s does not exist", in.DiscountID)
	}

	// case-sensitive exact match on code (findByCode == null required).
	var dummy uuid.UUID
	err = tx.QueryRow(ctx, "SELECT id FROM couponentity WHERE code = $1", in.Code).Scan(&dummy)
	if err == nil {
		return Coupon{}, fmt.Errorf("Coupon with code %s already exists", in.Code)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Coupon{}, fmt.Errorf("find coupon by code: %w", err)
	}

	var id uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO couponentity (maxusages, usages, validuntil, validfrom, code, discountid)
		 VALUES ($1, 0, $2, $3, $4, $5) RETURNING id`,
		in.MaxUsages, in.ValidUntil, in.ValidFrom, in.Code, in.DiscountID,
	).Scan(&id)
	if err != nil {
		return Coupon{}, fmt.Errorf("insert coupon: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Coupon{}, err
	}
	return Coupon{
		ID:         id,
		Usages:     0,
		MaxUsages:  in.MaxUsages,
		ValidFrom:  in.ValidFrom,
		ValidUntil: in.ValidUntil,
		Code:       in.Code,
		DiscountID: in.DiscountID,
	}, nil
}

// UpdateCouponParams is the input to UpdateCoupon. Code/ValidFrom/ValidUntil are
// plain pointers (nil = leave unchanged); MaxUsages is tri-state.
type UpdateCouponParams struct {
	ID         uuid.UUID
	Code       *string
	ValidFrom  *time.Time
	ValidUntil *time.Time
	MaxUsages  OptionalInt
}

// UpdateCoupon loads the coupon (unknown id -> error), applies the patch, and
// saves it. Code uniqueness is enforced by the DB UNIQUE(code) on save (a
// colliding code surfaces as a DB error). Returns the updated coupon.
func (s *Store) UpdateCoupon(ctx context.Context, in UpdateCouponParams) (Coupon, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Coupon{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	c, err := getCoupon(ctx, tx, in.ID)
	if err != nil {
		return Coupon{}, fmt.Errorf("coupon with id %s not found", in.ID)
	}

	if in.Code != nil {
		c.Code = *in.Code
	}
	if in.ValidUntil != nil {
		c.ValidUntil = *in.ValidUntil
	}
	if in.ValidFrom != nil {
		c.ValidFrom = *in.ValidFrom
	}
	if in.MaxUsages.Set {
		c.MaxUsages = in.MaxUsages.Value
	}

	if _, err := tx.Exec(ctx,
		`UPDATE couponentity SET code = $2, validuntil = $3, validfrom = $4, maxusages = $5 WHERE id = $1`,
		c.ID, c.Code, c.ValidUntil, c.ValidFrom, c.MaxUsages,
	); err != nil {
		return Coupon{}, fmt.Errorf("update coupon: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Coupon{}, err
	}
	return c, nil
}

// RegisterCoupon looks up the coupon by code, checks the strict validity window
// against now, inserts a redemption (which fires update_coupon_usages to bump
// usages and enforce maxUsages), and returns the reloaded coupon (usages
// reflects the increment). No event is published by the caller. The
// UNIQUE(couponid, userid) constraint blocks a user claiming the same coupon
// twice. Runs in one transaction.
func (s *Store) RegisterCoupon(ctx context.Context, code string, userID uuid.UUID, now time.Time) (Coupon, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Coupon{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var c Coupon
	err = tx.QueryRow(ctx,
		"SELECT "+couponColumns+" FROM couponentity WHERE couponentity.code = $1", code,
	).Scan(&c.ID, &c.Usages, &c.MaxUsages, &c.ValidFrom, &c.ValidUntil, &c.Code, &c.DiscountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Coupon{}, fmt.Errorf("Coupon with code %s does not exist", code)
	}
	if err != nil {
		return Coupon{}, fmt.Errorf("find coupon by code: %w", err)
	}

	// Strict validity window: validFrom < now && validUntil > now (equal
	// boundary fails), matching isBefore/isAfter.
	if !(c.ValidFrom.Before(now) && c.ValidUntil.After(now)) {
		return Coupon{}, fmt.Errorf("Coupon with code %s is not valid currently", code)
	}

	if _, err := tx.Exec(ctx,
		"INSERT INTO couponredemptionentity (couponid, userid) VALUES ($1, $2)", c.ID, userID,
	); err != nil {
		if msg := triggerMessage(err); msg != "" {
			return Coupon{}, fmt.Errorf("%s", msg)
		}
		return Coupon{}, fmt.Errorf("register coupon: %w", err)
	}

	// Reload so the returned usages reflects the trigger increment.
	reloaded, err := getCoupon(ctx, tx, c.ID)
	if err != nil {
		return Coupon{}, fmt.Errorf("reload coupon: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Coupon{}, err
	}
	return reloaded, nil
}
