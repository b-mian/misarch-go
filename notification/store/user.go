package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// UserExists reports whether a user id is present in the local replica. It
// mirrors the original userRepository.existsById check that backs the FK on
// notification creation.
func (s *Store) UserExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM userentity WHERE id = $1)", id,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check user exists %s: %w", id, err)
	}
	return exists, nil
}

// CreateUser inserts a user id into the local replica. It is intentionally NOT
// idempotent: a duplicate id violates the primary key and returns an error, so
// the event handler responds 500 and Dapr redelivers — reproducing the
// original UserRepository.createUser ("INSERT INTO UserEntity (id) VALUES
// (:id)") behavior exactly.
func (s *Store) CreateUser(ctx context.Context, id uuid.UUID) error {
	if _, err := s.pool.Exec(ctx,
		"INSERT INTO userentity (id) VALUES ($1)", id,
	); err != nil {
		return fmt.Errorf("insert user %s: %w", id, err)
	}
	return nil
}
