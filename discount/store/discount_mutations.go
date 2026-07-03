package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// OptionalInt models the graphql-kotlin OptionalInput<Int> tri-state used by
// updateDiscount/updateCoupon: Set=false means "absent, leave unchanged";
// Set=true with Value=nil means "explicitly null, clear it"; Set=true with a
// non-nil Value means "set to that value".
type OptionalInt struct {
	Set   bool
	Value *int
}

// AppliesTo groups the three target-id sets of a discount (categories,
// products, product variants). Used both as create/update input and as the
// event payload's id lists (re-read from the join tables on update).
type AppliesTo struct {
	CategoryIDs       []uuid.UUID
	ProductIDs        []uuid.UUID
	ProductVariantIDs []uuid.UUID
}

// CreateDiscountParams is the input to CreateDiscount.
type CreateDiscountParams struct {
	Discount         float64
	MaxUsagesPerUser *int
	MinOrderAmount   *int
	ValidFrom        time.Time
	ValidUntil       time.Time
	AppliesTo        AppliesTo
}

// CreateDiscount validates referenced entities and discount bounds, inserts the
// discount and its applies-to join rows in one transaction, and returns the new
// row together with the (de-duplicated) applies-to id sets for the event. The
// validation order matches the original (missing-entity error precedes
// discount-bounds error; at least one target required).
func (s *Store) CreateDiscount(ctx context.Context, in CreateDiscountParams) (Discount, AppliesTo, error) {
	categoryIDs := dedupe(in.AppliesTo.CategoryIDs)
	productIDs := dedupe(in.AppliesTo.ProductIDs)
	productVariantIDs := dedupe(in.AppliesTo.ProductVariantIDs)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Discount{}, AppliesTo{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// ensureReferencedEntitiesExist: at least one target, then existence.
	if len(categoryIDs) == 0 && len(productIDs) == 0 && len(productVariantIDs) == 0 {
		return Discount{}, AppliesTo{}, fmt.Errorf("At least one category, product or product variant must be specified")
	}
	if err := ensureEntitiesExist(ctx, tx, categoryIDs, productIDs, productVariantIDs); err != nil {
		return Discount{}, AppliesTo{}, err
	}

	// discount bounds (checked after existence, matching error precedence).
	if !(in.Discount >= 0) {
		return Discount{}, AppliesTo{}, fmt.Errorf("A discount cannot increase the price and thus must be >= 0")
	}
	if !(in.Discount <= 1) {
		return Discount{}, AppliesTo{}, fmt.Errorf("A discount cannot be larger than 1")
	}

	var id uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO discountentity (discount, maxusagesperuser, validuntil, validfrom, minorderamount)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		in.Discount, in.MaxUsagesPerUser, in.ValidUntil, in.ValidFrom, in.MinOrderAmount,
	).Scan(&id)
	if err != nil {
		return Discount{}, AppliesTo{}, fmt.Errorf("insert discount: %w", err)
	}

	if err := addAppliedToReferences(ctx, tx, id, categoryIDs, productIDs, productVariantIDs); err != nil {
		return Discount{}, AppliesTo{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Discount{}, AppliesTo{}, err
	}

	d := Discount{
		ID:               id,
		Discount:         in.Discount,
		MaxUsagesPerUser: in.MaxUsagesPerUser,
		ValidFrom:        in.ValidFrom,
		ValidUntil:       in.ValidUntil,
		MinOrderAmount:   in.MinOrderAmount,
	}
	return d, AppliesTo{CategoryIDs: categoryIDs, ProductIDs: productIDs, ProductVariantIDs: productVariantIDs}, nil
}

// UpdateDiscountParams is the input to UpdateDiscount.
type UpdateDiscountParams struct {
	ID                       uuid.UUID
	MaxUsagesPerUser         OptionalInt
	MinOrderAmount           OptionalInt
	ValidFrom                *time.Time
	ValidUntil               *time.Time
	AddedCategoryIDs         []uuid.UUID
	AddedProductIDs          []uuid.UUID
	AddedProductVariantIDs   []uuid.UUID
	RemovedCategoryIDs       []uuid.UUID
	RemovedProductIDs        []uuid.UUID
	RemovedProductVariantIDs []uuid.UUID
}

// UpdateDiscount applies the patch, adds/removes applies-to join rows, and
// returns the updated discount together with the applies-to id sets re-read
// from the join tables (post add/remove) for the event payload. Everything runs
// in one transaction. Adds are existence-checked (missing -> error); removes are
// not. An unknown discount id errors.
func (s *Store) UpdateDiscount(ctx context.Context, in UpdateDiscountParams) (Discount, AppliesTo, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Discount{}, AppliesTo{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Load the discount (unknown id -> error, matching the data loader assert).
	d, err := getDiscountTx(ctx, tx, in.ID)
	if err != nil {
		return Discount{}, AppliesTo{}, err
	}

	// Apply the scalar patch in memory.
	if in.MaxUsagesPerUser.Set {
		d.MaxUsagesPerUser = in.MaxUsagesPerUser.Value
	}
	if in.ValidUntil != nil {
		d.ValidUntil = *in.ValidUntil
	}
	if in.ValidFrom != nil {
		d.ValidFrom = *in.ValidFrom
	}
	if in.MinOrderAmount.Set {
		d.MinOrderAmount = in.MinOrderAmount.Value
	}

	// updateDiscountReferencedEntities: validate + add, then remove.
	addedCategories := dedupe(in.AddedCategoryIDs)
	addedProducts := dedupe(in.AddedProductIDs)
	addedProductVariants := dedupe(in.AddedProductVariantIDs)
	if err := ensureEntitiesExist(ctx, tx, addedCategories, addedProducts, addedProductVariants); err != nil {
		return Discount{}, AppliesTo{}, err
	}
	if err := addAppliedToReferences(ctx, tx, in.ID, addedCategories, addedProducts, addedProductVariants); err != nil {
		return Discount{}, AppliesTo{}, err
	}
	for _, cid := range in.RemovedCategoryIDs {
		if _, err := tx.Exec(ctx, "DELETE FROM discounttocategoryentity WHERE discountid = $1 AND categoryid = $2", in.ID, cid); err != nil {
			return Discount{}, AppliesTo{}, fmt.Errorf("remove category: %w", err)
		}
	}
	for _, pid := range in.RemovedProductIDs {
		if _, err := tx.Exec(ctx, "DELETE FROM discounttoproductentity WHERE discountid = $1 AND productid = $2", in.ID, pid); err != nil {
			return Discount{}, AppliesTo{}, fmt.Errorf("remove product: %w", err)
		}
	}
	for _, pvid := range in.RemovedProductVariantIDs {
		if _, err := tx.Exec(ctx, "DELETE FROM discounttoproductvariantentity WHERE discountid = $1 AND productvariantid = $2", in.ID, pvid); err != nil {
			return Discount{}, AppliesTo{}, fmt.Errorf("remove product variant: %w", err)
		}
	}

	// Save the discount (persist the scalar patch).
	if _, err := tx.Exec(ctx,
		`UPDATE discountentity SET maxusagesperuser = $2, validuntil = $3, validfrom = $4, minorderamount = $5
		 WHERE id = $1`,
		d.ID, d.MaxUsagesPerUser, d.ValidUntil, d.ValidFrom, d.MinOrderAmount,
	); err != nil {
		return Discount{}, AppliesTo{}, fmt.Errorf("update discount: %w", err)
	}

	// Re-read applies-to id sets from the join tables for the event payload.
	applies, err := readAppliesTo(ctx, tx, in.ID)
	if err != nil {
		return Discount{}, AppliesTo{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Discount{}, AppliesTo{}, err
	}
	return d, applies, nil
}

// getDiscountTx loads a discount by id on a transaction, erroring if absent.
func getDiscountTx(ctx context.Context, tx querier, id uuid.UUID) (Discount, error) {
	var d Discount
	err := tx.QueryRow(ctx,
		"SELECT "+discountColumns+" FROM discountentity WHERE discountentity.id = $1", id,
	).Scan(&d.ID, &d.Discount, &d.MaxUsagesPerUser, &d.ValidFrom, &d.ValidUntil, &d.MinOrderAmount)
	if err != nil {
		return Discount{}, fmt.Errorf("discount with id %s not found", id)
	}
	return d, nil
}

// ensureEntitiesExist errors if any provided category/product/product-variant id
// is absent, in the original's order (categories, then products, then variants)
// so error precedence matches. The missing-id lists render as Kotlin lists.
func ensureEntitiesExist(ctx context.Context, q querier, categoryIDs, productIDs, productVariantIDs []uuid.UUID) error {
	missing, err := missingIDs(ctx, q, "categoryentity", categoryIDs)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("Categories with ids %s do not exist", formatUUIDList(missing))
	}
	missing, err = missingIDs(ctx, q, "productentity", productIDs)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("Products with ids %s do not exist", formatUUIDList(missing))
	}
	missing, err = missingIDs(ctx, q, "productvariantentity", productVariantIDs)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("Product variants with ids %s do not exist", formatUUIDList(missing))
	}
	return nil
}

// addAppliedToReferences inserts the join rows for each id set (ids assumed
// de-duplicated).
func addAppliedToReferences(ctx context.Context, q querier, discountID uuid.UUID, categoryIDs, productIDs, productVariantIDs []uuid.UUID) error {
	for _, cid := range categoryIDs {
		if _, err := q.Exec(ctx, "INSERT INTO discounttocategoryentity (discountid, categoryid) VALUES ($1, $2)", discountID, cid); err != nil {
			return fmt.Errorf("add category reference: %w", err)
		}
	}
	for _, pid := range productIDs {
		if _, err := q.Exec(ctx, "INSERT INTO discounttoproductentity (discountid, productid) VALUES ($1, $2)", discountID, pid); err != nil {
			return fmt.Errorf("add product reference: %w", err)
		}
	}
	for _, pvid := range productVariantIDs {
		if _, err := q.Exec(ctx, "INSERT INTO discounttoproductvariantentity (discountid, productvariantid) VALUES ($1, $2)", discountID, pvid); err != nil {
			return fmt.Errorf("add product variant reference: %w", err)
		}
	}
	return nil
}

// readAppliesTo reads the current applies-to id sets of a discount from the join
// tables (used for the updated-event payload).
func readAppliesTo(ctx context.Context, q querier, discountID uuid.UUID) (AppliesTo, error) {
	cat, err := readIDColumn(ctx, q, "SELECT categoryid FROM discounttocategoryentity WHERE discountid = $1", discountID)
	if err != nil {
		return AppliesTo{}, err
	}
	prod, err := readIDColumn(ctx, q, "SELECT productid FROM discounttoproductentity WHERE discountid = $1", discountID)
	if err != nil {
		return AppliesTo{}, err
	}
	pv, err := readIDColumn(ctx, q, "SELECT productvariantid FROM discounttoproductvariantentity WHERE discountid = $1", discountID)
	if err != nil {
		return AppliesTo{}, err
	}
	return AppliesTo{CategoryIDs: cat, ProductIDs: prod, ProductVariantIDs: pv}, nil
}

// readIDColumn runs a single-column UUID query and collects the results.
func readIDColumn(ctx context.Context, q querier, sql string, args ...any) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
