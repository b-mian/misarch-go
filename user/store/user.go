package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// userColumns is the projection for a full User row (physical lowercase names).
const userColumns = "id, username, firstname, lastname, gender, birthday, datejoined"

// UserOrderColumn is a validated ORDER BY column set for the users connection.
// The graph layer maps the GraphQL UserOrderField enum to one of these.
type UserOrderColumn []string

// Order-column sets for users. USERNAME carries a secondary `id` tiebreaker
// (exactly as the original UserOrderField.USERNAME(username, id) did, with both
// columns sharing the one direction); ID is a single column.
var (
	UserOrderByID       = UserOrderColumn{"id"}
	UserOrderByUsername = UserOrderColumn{"username", "id"}
)

// GetUser loads a user by id. A missing row is an error (the original resolved
// via a data loader / awaitSingle that surfaced a GraphQL error on absence).
func (s *Store) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := scanUserRow(s.pool.QueryRow(ctx,
		"SELECT "+userColumns+" FROM userentity WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, fmt.Errorf("User with id %s does not exist.", id)
	}
	if err != nil {
		return User{}, fmt.Errorf("get user %s: %w", id, err)
	}
	return u, nil
}

// ListUsers returns a page of users ordered by the given column set and
// direction, together with the total count and next-page flag. There is no
// filter predicate for users (the original UserConnection passed predicate=null
// and no authorizedUserFilter), so the WHERE clause is always empty.
func (s *Store) ListUsers(
	ctx context.Context, first, skip *int, order UserOrderColumn, ascending bool,
) (Connection[User], error) {
	p := page{
		table:     "userentity",
		columns:   userColumns,
		orderCols: order,
		ascending: ascending,
		first:     first,
		skip:      skip,
	}

	total, err := p.totalCount(ctx, s.pool)
	if err != nil {
		return Connection[User]{}, err
	}

	q, args := p.nodesSQL()
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return Connection[User]{}, fmt.Errorf("list users: %w", err)
	}
	nodes, err := scanUsers(rows)
	if err != nil {
		return Connection[User]{}, err
	}

	next, err := p.hasNextPage(ctx, s.pool)
	if err != nil {
		return Connection[User]{}, err
	}

	return Connection[User]{Nodes: nodes, TotalCount: total, HasNextPage: next}, nil
}

// CreateUser inserts a user with the id taken from the event (overriding the
// column default), dateJoined set by the caller, and birthday/gender left NULL,
// then re-reads and returns the persisted row. This mirrors the original
// insert-then-findById so the caller's outbound event carries the stored
// dateJoined. The INSERT column order matches the original repository query.
func (s *Store) CreateUser(
	ctx context.Context, id uuid.UUID, username, firstName, lastName string, dateJoined time.Time,
) (User, error) {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO userentity (id, firstname, lastname, username, datejoined)
		 VALUES ($1, $2, $3, $4, $5)`,
		id, firstName, lastName, username, dateJoined,
	); err != nil {
		return User{}, fmt.Errorf("insert user: %w", err)
	}
	return s.GetUser(ctx, id)
}

// UpdateUser applies a partial update and returns the updated row, erroring if
// the user does not exist. No event is published (matching the original).
//
// The three-valued semantics of the original updateUser are reproduced here:
//   - firstName/lastName: plain nullable — a non-nil pointer sets the column;
//     nil leaves it unchanged (a JSON null is indistinguishable from omission
//     upstream and also means "leave unchanged").
//   - birthday/gender: tri-state via the *Set flags — when set is false the
//     column is left unchanged; when set is true the value is written (a nil
//     value clears the column to NULL).
func (s *Store) UpdateUser(
	ctx context.Context,
	id uuid.UUID,
	firstName, lastName *string,
	birthdaySet bool, birthday *time.Time,
	genderSet bool, gender *string,
) (User, error) {
	// Build the SET clause from only the columns that change, so an unset
	// tri-state field is never touched and a cleared one is set to NULL.
	set := make([]string, 0, 4)
	args := make([]any, 0, 5)
	args = append(args, id) // $1 is always the WHERE id
	next := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	if firstName != nil {
		set = append(set, "firstname = "+next(*firstName))
	}
	if lastName != nil {
		set = append(set, "lastname = "+next(*lastName))
	}
	if birthdaySet {
		set = append(set, "birthday = "+next(birthday)) // birthday may be nil -> NULL
	}
	if genderSet {
		set = append(set, "gender = "+next(gender)) // gender may be nil -> NULL
	}

	if len(set) == 0 {
		// Nothing to update — the original save() still returned the (unchanged)
		// row, and a missing id still errored. Preserve both by reading it back.
		return s.GetUser(ctx, id)
	}

	q := "UPDATE userentity SET " + strings.Join(set, ", ") +
		" WHERE id = $1 RETURNING " + userColumns
	u, err := scanUserRow(s.pool.QueryRow(ctx, q, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, fmt.Errorf("User with id %s does not exist.", id)
	}
	if err != nil {
		return User{}, fmt.Errorf("update user %s: %w", id, err)
	}
	return u, nil
}

// rowScanner abstracts pgx.Row / pgx.Rows for the shared column scan.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanUserRow scans a single user row using the userColumns projection order.
func scanUserRow(row rowScanner) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.FirstName, &u.LastName, &u.Gender, &u.Birthday, &u.DateJoined)
	return u, err
}

// scanUsers collects user rows from an open pgx.Rows.
func scanUsers(rows pgx.Rows) ([]User, error) {
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
