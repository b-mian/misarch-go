package graph

import (
	"context"
	"fmt"

	"misarch/pkg/auth"
)

// checkIsEmployee reproduces the original's authorization gate byte-for-byte:
//   - no authorized user in context (anonymous)      → error "Unauthorized access"
//     (Kotlin: accessing dfe.authorizedUser threw IllegalStateException("Unauthorized access"))
//   - user present but not an employee/admin          → error
//     "Unauthorized access: <userId> is not an employee or admin"
//     (Kotlin: AuthorizedUser.checkIsEmployee()).
//
// isEmployee ⟺ roles contains "employee" OR "admin" (admin implies employee),
// exactly as AuthorizedUser.isEmployee. Returns nil when the user is authorized.
func checkIsEmployee(ctx context.Context) error {
	u := auth.FromContext(ctx)
	if u == nil {
		return fmt.Errorf("Unauthorized access")
	}
	if !u.IsEmployeeOrAdmin() {
		return fmt.Errorf("Unauthorized access: %s is not an employee or admin", u.ID)
	}
	return nil
}

// isEmployee reports whether the current request is from an employee or admin
// (used for the implicit product/variant visibility filter). Anonymous → false.
func isEmployee(ctx context.Context) bool {
	u := auth.FromContext(ctx)
	return u != nil && u.IsEmployeeOrAdmin()
}
