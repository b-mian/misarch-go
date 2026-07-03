package graph

import (
	"context"
	"fmt"

	"misarch/pkg/auth"
)

// checkIsEmployee reproduces AuthorizedUser.checkIsEmployee() combined with the
// `authorizedUser` getter: it requires an authenticated user who is an employee
// or admin. The exact error strings are contractual (byte-for-byte with the
// reference):
//   - no user in context (header absent) → "Unauthorized access"
//   - user present but not employee/admin → "Unauthorized access: <id> is not an employee or admin"
//
// admin implies employee (auth.User.IsEmployeeOrAdmin covers both roles).
func checkIsEmployee(ctx context.Context) error {
	user := auth.FromContext(ctx)
	if user == nil {
		return fmt.Errorf("Unauthorized access")
	}
	if !user.IsEmployeeOrAdmin() {
		return fmt.Errorf("Unauthorized access: %s is not an employee or admin", user.ID)
	}
	return nil
}
