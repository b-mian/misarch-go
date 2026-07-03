package graph

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"misarch/pkg/auth"
)

// This file reproduces the address service's authorization checks with the
// exact error message text the MiSArch e2e tests assert as substrings. The
// platform auth helpers (auth.RequireEmployeeOrAdmin, …) return generic
// messages, so the resolvers use these thin wrappers instead:
//
//   - requireUser mirrors DataFetchingEnvironment.authorizedUser: it throws
//     "Unauthorized access" when no Authorized-User header is present.
//   - checkIsEmployee mirrors AuthorizedUser.checkIsEmployee: it throws
//     "Unauthorized access: <id> is not an employee or admin" for a
//     non-employee/admin caller.

// requireUser returns the authenticated user or the "Unauthorized access"
// error the original raised when the authorizedUser getter found no header.
func requireUser(ctx context.Context) (*auth.User, error) {
	u := auth.FromContext(ctx)
	if u == nil {
		return nil, fmt.Errorf("Unauthorized access")
	}
	return u, nil
}

// checkIsEmployee returns an error unless the caller is an employee or admin,
// matching AuthorizedUser.checkIsEmployee's message exactly.
func checkIsEmployee(u *auth.User) error {
	if !u.IsEmployeeOrAdmin() {
		return fmt.Errorf("Unauthorized access: %s is not an employee or admin", u.ID)
	}
	return nil
}

// requireSelfOrEmployee reproduces the ubiquitous "self OR employee" check:
// require a header, then — if the caller is not the resource owner — require
// employee/admin. This is the exact ordering (and messages) of
//
//	val authorizedUser = dfe.authorizedUser
//	if (owner != authorizedUser.id) authorizedUser.checkIsEmployee()
func requireSelfOrEmployee(ctx context.Context, owner uuid.UUID) (*auth.User, error) {
	u, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	if u.ID != owner {
		if err := checkIsEmployee(u); err != nil {
			return nil, err
		}
	}
	return u, nil
}
