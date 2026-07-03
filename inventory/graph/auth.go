package graph

import (
	"context"
	"fmt"

	"misarch/pkg/auth"
)

// The original NestJS RolesGuard denies a protected field with the GraphQL
// error message "Forbidden resource" when the Authorized-User header is absent
// or carries none of the required roles (spec §3.5 / §6). These helpers
// reproduce that message exactly (it is load-bearing: other services/tests
// match on it) rather than surfacing the pkg/auth sentinel errors.
//
// The role strings are the lowercase values from the header JSON: admin,
// employee, buyer (auth.RoleAdmin / auth.RoleEmployee).

// errForbidden is the exact message the original guard returned on denial.
var errForbidden = fmt.Errorf("Forbidden resource")

// requireEmployeeOrAdmin allows the request iff the caller holds the employee
// or admin role; otherwise it returns the "Forbidden resource" error.
func requireEmployeeOrAdmin(ctx context.Context) error {
	u := auth.FromContext(ctx)
	if u.HasRole(auth.RoleEmployee) || u.HasRole(auth.RoleAdmin) {
		return nil
	}
	return errForbidden
}

// requireAdmin allows the request iff the caller holds the admin role
// (deleteProductItem is admin-only, spec §6).
func requireAdmin(ctx context.Context) error {
	if auth.FromContext(ctx).HasRole(auth.RoleAdmin) {
		return nil
	}
	return errForbidden
}
