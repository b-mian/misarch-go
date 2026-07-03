package graph

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"misarch/pkg/auth"
)

// authorizeUser reproduces the original Rust authorize_user + check_permissions
// exactly, including the two error messages.
//
//   - If no Authorized-User header was parsed into the context (anonymous), it
//     fails with the "not set or could not be parsed" message.
//   - Otherwise the caller is permitted iff they hold a permissive role
//     (admin or employee; buyer is NOT permissive) OR id is non-nil and equals
//     the caller's own id (resource ownership).
//   - Otherwise it fails with the "Operation not permitted" message naming the
//     caller's id.
//
// The platform auth.Middleware (applied by server.Run) parses the header into
// the context; a missing or malformed header yields a nil user here, matching
// the Rust behavior where a bad header is simply not added to the context.
func authorizeUser(ctx context.Context, id *uuid.UUID) error {
	user := auth.FromContext(ctx)
	if user == nil {
		return fmt.Errorf("Authentication failed. Authorized-User header is not set or could not be parsed.")
	}
	permissive := user.IsEmployeeOrAdmin()
	ownsResource := id != nil && *id == user.ID
	if permissive || ownsResource {
		return nil
	}
	return fmt.Errorf(
		"Authentication failed for user of UUID: `%s`. Operation not permitted.",
		user.ID,
	)
}
