package store

import (
	"fmt"

	"github.com/jackc/pgx/v5"
)

// The scan* helpers each consume an open pgx.Rows (and close it), returning the
// typed slice for a connection page. Column order matches the matching
// *Columns constant in store.go.

func scanDiscounts(rows pgx.Rows) ([]Discount, error) {
	defer rows.Close()
	var out []Discount
	for rows.Next() {
		var d Discount
		if err := rows.Scan(&d.ID, &d.Discount, &d.MaxUsagesPerUser, &d.ValidFrom, &d.ValidUntil, &d.MinOrderAmount); err != nil {
			return nil, fmt.Errorf("scan discount: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func scanCoupons(rows pgx.Rows) ([]Coupon, error) {
	defer rows.Close()
	var out []Coupon
	for rows.Next() {
		var c Coupon
		if err := rows.Scan(&c.ID, &c.Usages, &c.MaxUsages, &c.ValidFrom, &c.ValidUntil, &c.Code, &c.DiscountID); err != nil {
			return nil, fmt.Errorf("scan coupon: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanDiscountUsages(rows pgx.Rows) ([]DiscountUsage, error) {
	defer rows.Close()
	var out []DiscountUsage
	for rows.Next() {
		var u DiscountUsage
		if err := rows.Scan(&u.ID, &u.DiscountID, &u.UserID, &u.Usages); err != nil {
			return nil, fmt.Errorf("scan discount usage: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func scanCategories(rows pgx.Rows) ([]CategoryRow, error) {
	defer rows.Close()
	var out []CategoryRow
	for rows.Next() {
		var c CategoryRow
		if err := rows.Scan(&c.ID); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanProducts(rows pgx.Rows) ([]ProductRow, error) {
	defer rows.Close()
	var out []ProductRow
	for rows.Next() {
		var p ProductRow
		if err := rows.Scan(&p.ID); err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanProductVariants(rows pgx.Rows) ([]ProductVariantRow, error) {
	defer rows.Close()
	var out []ProductVariantRow
	for rows.Next() {
		var p ProductVariantRow
		if err := rows.Scan(&p.ID, &p.ProductID); err != nil {
			return nil, fmt.Errorf("scan product variant: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanUsers(rows pgx.Rows) ([]UserRow, error) {
	defer rows.Close()
	var out []UserRow
	for rows.Next() {
		var u UserRow
		if err := rows.Scan(&u.ID); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
