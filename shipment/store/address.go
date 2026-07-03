package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// addressColumns is the projection for a full Address row.
const addressColumns = "id, userid, street1, street2, city, postalcode, country, companyname"

// GetAddress loads an address by id, erroring if it does not exist. Used by
// Shipment.shipmentAddress (non-null field → miss must error) and when
// building the provider payload. The UserID null-ness discriminates
// UserAddress vs VendorAddress for the Address interface.
func (s *Store) GetAddress(ctx context.Context, id uuid.UUID) (Address, error) {
	var a Address
	err := s.pool.QueryRow(ctx,
		"SELECT "+addressColumns+" FROM addressentity WHERE id = $1", id,
	).Scan(&a.ID, &a.UserID, &a.Street1, &a.Street2, &a.City, &a.PostalCode, &a.Country, &a.CompanyName)
	if errors.Is(err, pgx.ErrNoRows) {
		return Address{}, fmt.Errorf("Address with id %s does not exist.", id)
	}
	if err != nil {
		return Address{}, fmt.Errorf("get address %s: %w", id, err)
	}
	return a, nil
}

// AddressExists reports whether an address with the given id exists (the
// create-shipment saga requires the shipment address to exist, else
// "Address does not exist").
func (s *Store) AddressExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM addressentity WHERE id = $1)", id,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("address exists %s: %w", id, err)
	}
	return exists, nil
}

// FindCurrentVendorAddress returns the newest vendor address (userid IS NULL,
// highest version). Returns (address, true, nil) when one exists; (zero,
// false, nil) when none — the return handler requires one, else
// "No vendor address found".
func (s *Store) FindCurrentVendorAddress(ctx context.Context) (Address, bool, error) {
	var a Address
	err := s.pool.QueryRow(ctx,
		"SELECT "+addressColumns+" FROM addressentity WHERE userid IS NULL ORDER BY version DESC LIMIT 1",
	).Scan(&a.ID, &a.UserID, &a.Street1, &a.Street2, &a.City, &a.PostalCode, &a.Country, &a.CompanyName)
	if errors.Is(err, pgx.ErrNoRows) {
		return Address{}, false, nil
	}
	if err != nil {
		return Address{}, false, fmt.Errorf("find current vendor address: %w", err)
	}
	return a, true, nil
}

// CreateAddress inserts an address with the given id (supplied by the address
// service's event). userId is NULL for vendor addresses. Plain INSERT with no
// ON CONFLICT — a duplicate delivery is a PK violation → error → 500 → Dapr
// retry, matching the reference (idempotency gap is intentional).
func (s *Store) CreateAddress(
	ctx context.Context, id uuid.UUID, userID *uuid.UUID, street1, street2, city, postalCode, country string, companyName *string,
) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO addressentity (id, userid, street1, street2, city, postalcode, country, companyname)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, userID, street1, street2, city, postalCode, country, companyName,
	)
	if err != nil {
		return fmt.Errorf("create address %s: %w", id, err)
	}
	return nil
}
