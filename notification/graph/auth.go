package graph

import (
	"context"
	"fmt"

	"misarch/pkg/auth"
)

// This file reproduces org.misarch.notification.graphql.AuthorizedUser and its
// DataFetchingEnvironment extensions. The shared pkg/auth helpers return
// generic error strings; the notification service's observable contract
// requires the exact Kotlin messages, so the checks are reimplemented here on
// top of auth.FromContext.

// authorizedUser returns the caller identity, mirroring
// DataFetchingEnvironment.authorizedUser: a missing/invalid Authorized-User
// header throws "Unauthorized access". (auth.Middleware only populates the
// context when the header is present and well-formed, so a nil user here means
// no valid header.)
func authorizedUser(ctx context.Context) (*auth.User, error) {
	u := auth.FromContext(ctx)
	if u == nil {
		return nil, fmt.Errorf("Unauthorized access")
	}
	return u, nil
}

// isEmployee reports whether the user is an employee or admin (roles contains
// "employee" OR isAdmin), mirroring AuthorizedUser.isEmployee.
func isEmployee(u *auth.User) bool {
	return u.HasRole(auth.RoleEmployee) || isAdmin(u)
}

// isAdmin reports whether the user is an admin (roles contains "admin"),
// mirroring AuthorizedUser.isAdmin.
func isAdmin(u *auth.User) bool {
	return u.HasRole(auth.RoleAdmin)
}

// checkIsEmployee mirrors AuthorizedUser.checkIsEmployee: it errors with
// "Unauthorized access: <id> is not an employee or admin" when the user is
// neither an employee nor an admin.
func checkIsEmployee(u *auth.User) error {
	if !isEmployee(u) {
		return fmt.Errorf("Unauthorized access: %s is not an employee or admin", u.ID)
	}
	return nil
}

// checkIsAdmin mirrors AuthorizedUser.checkIsAdmin: it errors with
// "Unauthorized access: <id> is not an admin" when the user is not an admin.
func checkIsAdmin(u *auth.User) error {
	if !isAdmin(u) {
		return fmt.Errorf("Unauthorized access: %s is not an admin", u.ID)
	}
	return nil
}
