package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"misarch/pkg/auth"
)

// errNoAuthHeader is the exact error the original returns when the
// Authorized-User header is absent or unparseable (authorize_user's Err branch).
var errNoAuthHeader = errors.New("Authentication failed. Authorized-User header is not set or could not be parsed.")

// authorizeUser reproduces the Rust authorize_user(ctx, Some(id)) semantics
// against the parsed Authorized-User header:
//
//   - header missing/unparseable → errNoAuthHeader;
//   - a permissive role (admin/employee) → OK regardless of id;
//   - header.id == id (ownership) → OK;
//   - otherwise → "Authentication failed for user of UUID: `<header.id>`.
//     Operation not permitted.".
//
// (The Some(id) form is the only one used by resolvers; the None form is not
// reproduced because nothing calls it.)
func authorizeUser(ctx context.Context, id uuid.UUID) error {
	u := auth.FromContext(ctx)
	if u == nil {
		return errNoAuthHeader
	}
	if u.IsEmployeeOrAdmin() || u.ID == id {
		return nil
	}
	return fmt.Errorf("Authentication failed for user of UUID: `%s`. Operation not permitted.", u.ID)
}

// authorizedUserHeaderJSON reconstructs the Authorized-User header JSON
// ({"id":..,"roles":[..]}) from the parsed context user, for forwarding to the
// shoppingcart service (the only downstream call that receives it). Returns
// errNoAuthHeader when no user is present — matching create_internal_order_items,
// which requires the header in context.
func authorizedUserHeaderJSON(ctx context.Context) (string, error) {
	u := auth.FromContext(ctx)
	if u == nil {
		return "", errNoAuthHeader
	}
	roles := u.Roles
	if roles == nil {
		roles = []string{}
	}
	b, err := json.Marshal(struct {
		ID    uuid.UUID `json:"id"`
		Roles []string  `json:"roles"`
	}{ID: u.ID, Roles: roles})
	if err != nil {
		return "", err
	}
	return string(b), nil
}
