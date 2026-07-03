package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// addressColumns is the projection for a full addressentity row. Postgres folds
// unquoted identifiers to lowercase, so column names are written lowercase.
const addressColumns = "id, firstname, lastname, street1, street2, city, postalcode, country, companyname, userid, archivedat, version"

// AddressOrderColumn is a validated ORDER BY column set for the addresses
// connection. The graph layer maps the GraphQL enum to one of these.
type AddressOrderColumn []string

// AddressOrderByID orders by the id column only. It is the sole order field the
// address schema exposes (UserAddressOrderField.ID); unlike the tax NAME field
// it carries no secondary tiebreaker because id is already unique.
var AddressOrderByID = AddressOrderColumn{"id"}

// ArchivedFilter is the tri-state isArchived filter of UserAddressFilter:
// unset => no filter, true => archivedat IS NOT NULL, false => archivedat IS NULL.
type ArchivedFilter struct {
	Set   bool
	Value bool
}

// GetAddress loads an address by id, returning an error if it does not exist
// (the original data loader asserted non-null via `!!`, surfacing a GraphQL
// error rather than a null).
func (s *Store) GetAddress(ctx context.Context, id uuid.UUID) (Address, error) {
	row := s.pool.QueryRow(ctx, "SELECT "+addressColumns+" FROM addressentity WHERE id = $1", id)
	a, err := scanAddress(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Address{}, fmt.Errorf("Address with id %s does not exist.", id)
	}
	if err != nil {
		return Address{}, fmt.Errorf("get address %s: %w", id, err)
	}
	return a, nil
}

// CurrentVendorAddress returns the current vendor address — the highest-version
// row among those with userid IS NULL — or ok=false if none exists. Mirrors
// AddressRepository.findCurrentVendorAddress (nullable Query.vendorAddress).
func (s *Store) CurrentVendorAddress(ctx context.Context) (Address, bool, error) {
	row := s.pool.QueryRow(ctx,
		"SELECT "+addressColumns+" FROM addressentity WHERE userid IS NULL ORDER BY version DESC LIMIT 1")
	a, err := scanAddress(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Address{}, false, nil
	}
	if err != nil {
		return Address{}, false, fmt.Errorf("current vendor address: %w", err)
	}
	return a, true, nil
}

// UserExists reports whether a user id has been replicated locally (via the
// user/user/created event). Mirrors UserRepository.existsById.
func (s *Store) UserExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM userentity WHERE id = $1)", id,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("user exists %s: %w", id, err)
	}
	return exists, nil
}

// RegisterUser inserts a replicated user id. It performs a plain INSERT with no
// conflict handling: a duplicate delivery violates the primary key and errors,
// exactly as the original UserRepository.createUser did (Dapr then redelivers).
func (s *Store) RegisterUser(ctx context.Context, id uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, "INSERT INTO userentity (id) VALUES ($1)", id); err != nil {
		return fmt.Errorf("register user %s: %w", id, err)
	}
	return nil
}

// CreateAddress inserts an address row, omitting id and version so the DB
// defaults apply (uuid_generate_v4(), nextval), with archivedat NULL. userID
// nil produces a vendor address, non-nil a user address. The generated id and
// version are read back and returned in the full row. The write runs in its
// own transaction; the caller publishes the creation event after commit.
func (s *Store) CreateAddress(
	ctx context.Context,
	firstName, lastName *string,
	street1, street2, city, postalCode, country string,
	companyName *string,
	userID *uuid.UUID,
) (Address, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Address{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	a := Address{
		FirstName:   firstName,
		LastName:    lastName,
		Street1:     street1,
		Street2:     street2,
		City:        city,
		PostalCode:  postalCode,
		Country:     country,
		CompanyName: companyName,
		UserID:      userID,
		ArchivedAt:  nil,
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO addressentity
			(firstname, lastname, street1, street2, city, postalcode, country, companyname, userid, archivedat)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULL)
		RETURNING id, version, archivedat`,
		firstName, lastName, street1, street2, city, postalCode, country, companyName, userID,
	).Scan(&a.ID, &a.Version, &a.ArchivedAt)
	if err != nil {
		return Address{}, fmt.Errorf("insert address: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Address{}, err
	}
	return a, nil
}

// ArchiveAddress sets archivedat to the given timestamp on the row identified
// by id. The resolver has already loaded the row (via GetAddress) and enforced
// authorization and the vendor-id short-circuit, so this reproduces the
// original's load -> auth -> update ordering: only archivedat is written;
// version and every other column are untouched. Re-archiving is allowed — an
// already archived row's timestamp is simply overwritten with no idempotency
// guard, matching the original (which re-publishes the event each time).
func (s *Store) ArchiveAddress(
	ctx context.Context, id uuid.UUID, archivedAt time.Time,
) error {
	if _, err := s.pool.Exec(ctx,
		"UPDATE addressentity SET archivedat = $2 WHERE id = $1", id, archivedAt,
	); err != nil {
		return fmt.Errorf("archive address %s: %w", id, err)
	}
	return nil
}

// ListUserAddresses returns a page of a user's addresses (userid = ownerID),
// optionally filtered by archived state, ordered by the given column set and
// direction, together with the total count and next-page flag. Vendor rows
// never match the userid predicate.
func (s *Store) ListUserAddresses(
	ctx context.Context,
	ownerID uuid.UUID,
	filter ArchivedFilter,
	first, skip *int,
	order AddressOrderColumn,
	ascending bool,
) (Connection[Address], error) {
	predicates := []string{"userid = $1"}
	args := []any{ownerID}
	if filter.Set {
		if filter.Value {
			predicates = append(predicates, "archivedat IS NOT NULL")
		} else {
			predicates = append(predicates, "archivedat IS NULL")
		}
	}

	p := page{
		columns:    addressColumns,
		predicates: predicates,
		args:       args,
		orderCols:  order,
		ascending:  ascending,
		first:      first,
		skip:       skip,
	}

	total, err := p.totalCount(ctx, s.pool)
	if err != nil {
		return Connection[Address]{}, err
	}

	q, qargs := p.nodesSQL()
	rows, err := s.pool.Query(ctx, q, qargs...)
	if err != nil {
		return Connection[Address]{}, fmt.Errorf("list user addresses: %w", err)
	}
	nodes, err := scanAddresses(rows)
	if err != nil {
		return Connection[Address]{}, err
	}

	next, err := p.hasNextPage(ctx, s.pool)
	if err != nil {
		return Connection[Address]{}, err
	}

	return Connection[Address]{Nodes: nodes, TotalCount: total, HasNextPage: next}, nil
}

// scanAddress scans a single addressentity row from a pgx.Row.
func scanAddress(row pgx.Row) (Address, error) {
	var a Address
	err := row.Scan(
		&a.ID, &a.FirstName, &a.LastName, &a.Street1, &a.Street2, &a.City,
		&a.PostalCode, &a.Country, &a.CompanyName, &a.UserID, &a.ArchivedAt, &a.Version,
	)
	return a, err
}

// scanAddresses collects address rows from an open pgx.Rows.
func scanAddresses(rows pgx.Rows) ([]Address, error) {
	defer rows.Close()
	var out []Address
	for rows.Next() {
		var a Address
		if err := rows.Scan(
			&a.ID, &a.FirstName, &a.LastName, &a.Street1, &a.Street2, &a.City,
			&a.PostalCode, &a.Country, &a.CompanyName, &a.UserID, &a.ArchivedAt, &a.Version,
		); err != nil {
			return nil, fmt.Errorf("scan address: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
