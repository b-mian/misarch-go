package graph

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"misarch/pkg/auth"
)

// This file reproduces the media service's authorization semantics with the
// EXACT error strings the original Rust service returned (see media spec §6).
// The platform auth package parses the Authorized-User header into the request
// context (auth.FromContext → *auth.User, nil for anonymous); this helper
// mirrors the Rust `authorize_user` / `check_permissions` decision and messages
// rather than the platform's generic Require* helpers, so clients observe the
// identical GraphQL error text.

// permissiveRoles are the roles that grant access regardless of ownership.
// Mirrors Role::is_permissive: Admin and Employee are permissive; Buyer is not.
func isPermissive(u *auth.User) bool {
	return u.HasRole(auth.RoleAdmin) || u.HasRole(auth.RoleEmployee)
}

// authorizeUser reproduces the Rust `authorize_user(ctx, id)`:
//
//   - If no Authorized-User header was parsed into the context → error
//     "Authentication failed. Authorized-User header is not set or could not be parsed."
//   - Otherwise permit if the caller has a permissive role (admin/employee) OR
//     id is non-nil and equals the caller's id (self-ownership).
//   - Otherwise → error
//     "Authentication failed for user of UUID: `<uuid>`. Operation not permitted."
//     (the UUID is wrapped in literal backticks).
//
// Every media operation passes id == nil, so only the role path grants access;
// the ownership branch exists for parity with the shared authorization code.
func authorizeUser(ctx context.Context, id *uuid.UUID) error {
	user := auth.FromContext(ctx)
	if user == nil {
		return fmt.Errorf("Authentication failed. Authorized-User header is not set or could not be parsed.")
	}
	idContainedInHeader := id != nil && user.ID == *id
	if isPermissive(user) || idContainedInHeader {
		return nil
	}
	return fmt.Errorf("Authentication failed for user of UUID: `%s`. Operation not permitted.", user.ID)
}
