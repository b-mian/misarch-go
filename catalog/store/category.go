package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// NamedInput is one category characteristic to create (categorical or
// numerical). Unit is nil/ignored for categorical characteristics.
type NamedInput struct {
	Name        string
	Description string
	Unit        *string
}

// CreateCategory inserts a category and its characteristics in one transaction:
// categorical names then numerical names are collected and checked for
// duplicates (within this input only) before any characteristic is inserted,
// then categorical characteristics are inserted in order, then numerical.
// Returns the created category for the category/created event after commit.
func (s *Store) CreateCategory(
	ctx context.Context, name, description string, categorical, numerical []NamedInput,
) (Category, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Category{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var categoryID uuid.UUID
	if err := tx.QueryRow(ctx,
		"INSERT INTO categoryentity (name, description) VALUES ($1, $2) RETURNING id", name, description,
	).Scan(&categoryID); err != nil {
		return Category{}, fmt.Errorf("insert category: %w", err)
	}

	// Duplicate names (categorical then numerical) are rejected — checked only
	// within this input, not against pre-existing characteristics.
	names := make([]string, 0, len(categorical)+len(numerical))
	for _, c := range categorical {
		names = append(names, c.Name)
	}
	for _, n := range numerical {
		names = append(names, n.Name)
	}
	if dups := duplicateStrings(names); len(dups) > 0 {
		return Category{}, fmt.Errorf("Characteristic names must be unique. Duplicates: %s", formatStringSet(dups))
	}

	for _, c := range categorical {
		if err := insertCharacteristic(ctx, tx, categoryID, DiscriminatorCategorical, c.Name, c.Description, nil); err != nil {
			return Category{}, err
		}
	}
	for _, n := range numerical {
		if err := insertCharacteristic(ctx, tx, categoryID, DiscriminatorNumerical, n.Name, n.Description, n.Unit); err != nil {
			return Category{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Category{}, err
	}
	return Category{ID: categoryID, Name: name, Description: description}, nil
}

// CreateCategoricalCharacteristic checks the category exists then inserts a
// categorical characteristic (unit NULL). No event is published.
func (s *Store) CreateCategoricalCharacteristic(
	ctx context.Context, categoryID uuid.UUID, name, description string,
) (CategoryCharacteristic, error) {
	return s.createStandaloneCharacteristic(ctx, categoryID, DiscriminatorCategorical, name, description, nil)
}

// CreateNumericalCharacteristic checks the category exists then inserts a
// numerical characteristic with a unit. No event is published.
func (s *Store) CreateNumericalCharacteristic(
	ctx context.Context, categoryID uuid.UUID, name, description, unit string,
) (CategoryCharacteristic, error) {
	return s.createStandaloneCharacteristic(ctx, categoryID, DiscriminatorNumerical, name, description, &unit)
}

// createStandaloneCharacteristic is the shared flow for the two standalone
// create*CategoryCharacteristic mutations: check category, insert, return the
// full row.
func (s *Store) createStandaloneCharacteristic(
	ctx context.Context, categoryID uuid.UUID, discriminator, name, description string, unit *string,
) (CategoryCharacteristic, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CategoryCharacteristic{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	exists, err := existsByID(ctx, tx, "categoryentity", categoryID)
	if err != nil {
		return CategoryCharacteristic{}, err
	}
	if !exists {
		return CategoryCharacteristic{}, fmt.Errorf("Category with id %s does not exist.", categoryID)
	}

	c, err := insertCharacteristicReturning(ctx, tx, categoryID, discriminator, name, description, unit)
	if err != nil {
		return CategoryCharacteristic{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CategoryCharacteristic{}, err
	}
	return c, nil
}

// insertCharacteristic inserts a characteristic row (no return value needed).
func insertCharacteristic(
	ctx context.Context, tx pgx.Tx, categoryID uuid.UUID, discriminator, name, description string, unit *string,
) error {
	_, err := tx.Exec(ctx,
		"INSERT INTO categorycharacteristicentity (discriminator, name, description, unit, categoryid) VALUES ($1, $2, $3, $4, $5)",
		discriminator, name, description, unit, categoryID,
	)
	if err != nil {
		return fmt.Errorf("insert category characteristic: %w", err)
	}
	return nil
}

// insertCharacteristicReturning inserts a characteristic and returns the full row.
func insertCharacteristicReturning(
	ctx context.Context, tx pgx.Tx, categoryID uuid.UUID, discriminator, name, description string, unit *string,
) (CategoryCharacteristic, error) {
	var c CategoryCharacteristic
	err := tx.QueryRow(ctx,
		"INSERT INTO categorycharacteristicentity (discriminator, name, description, unit, categoryid) VALUES ($1, $2, $3, $4, $5) RETURNING "+characteristicColumns,
		discriminator, name, description, unit, categoryID,
	).Scan(&c.ID, &c.Discriminator, &c.Name, &c.Description, &c.Unit, &c.CategoryID)
	if err != nil {
		return CategoryCharacteristic{}, fmt.Errorf("insert category characteristic: %w", err)
	}
	return c, nil
}

// duplicateStrings returns the strings that occur more than once, in
// first-encounter order (mirrors Kotlin Iterable.duplicates()).
func duplicateStrings(values []string) []string {
	counts := make(map[string]int, len(values))
	order := make([]string, 0, len(values))
	for _, v := range values {
		if counts[v] == 0 {
			order = append(order, v)
		}
		counts[v]++
	}
	var dups []string
	for _, v := range order {
		if counts[v] > 1 {
			dups = append(dups, v)
		}
	}
	return dups
}

// formatStringSet renders a string slice the way Kotlin renders a Set in an
// exception message: "[a, b]".
func formatStringSet(values []string) string {
	return "[" + strings.Join(values, ", ") + "]"
}
