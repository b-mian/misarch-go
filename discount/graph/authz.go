package graph

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"misarch/discount/store"
	"misarch/pkg/auth"
)

// The auth helpers reproduce the original graphql-kotlin error messages
// byte-for-byte (they are a contract other services/tests may match), which the
// generic misarch/pkg/auth helpers do not. Enforcement is per-resolver, so a
// query mixing gated and public fields returns partial data with per-field
// errors — exactly as the original's per-resolver checkIsEmployee.

// requireEmployee enforces employee-or-admin access. With no Authorized-User
// header it fails like reading dfe.authorizedUser ("Unauthorized access");
// authenticated-but-not-employee fails like checkIsEmployee.
func requireEmployee(ctx context.Context) error {
	u := auth.FromContext(ctx)
	if u == nil {
		return fmt.Errorf("Unauthorized access")
	}
	if !u.IsEmployeeOrAdmin() {
		return fmt.Errorf("Unauthorized access: %s is not an employee or admin", u.ID)
	}
	return nil
}

// requireSelfOrEmployee enforces "the caller is ownerID, or an employee". It
// mirrors `if (authorizedUser.id != ownerID) authorizedUser.checkIsEmployee()`:
// reading authorizedUser.id with no header throws "Unauthorized access" first;
// a non-owner non-employee gets the checkIsEmployee message.
func requireSelfOrEmployee(ctx context.Context, ownerID uuid.UUID) error {
	u := auth.FromContext(ctx)
	if u == nil {
		return fmt.Errorf("Unauthorized access")
	}
	if u.ID == ownerID {
		return nil
	}
	if !u.IsEmployeeOrAdmin() {
		return fmt.Errorf("Unauthorized access: %s is not an employee or admin", u.ID)
	}
	return nil
}

// authUser maps the request's auth context to the store's minimal AuthUser view
// (nil = anonymous), used by the coupon connection's user-scoped filters and by
// applicableDiscounts.
func authUser(ctx context.Context) *store.AuthUser {
	u := auth.FromContext(ctx)
	if u == nil {
		return nil
	}
	return &store.AuthUser{ID: u.ID, Employee: u.IsEmployeeOrAdmin()}
}

// applyCouponFilterAuth reproduces CouponFilter.toExpression's authorization,
// which the original evaluates while the CouponConnection builds its condition:
// a set userHasCoupon requires an authenticated user, and querying another
// user's coupons requires employee. It must run before the store query so the
// error surfaces. A nil/empty filter is a no-op.
func applyCouponFilterAuth(ctx context.Context, filter *CouponFilterInput) error {
	if filter == nil {
		return nil
	}
	userHasCoupon := filter.UserHasCoupon.Value()
	if userHasCoupon == nil {
		return nil
	}
	u := auth.FromContext(ctx)
	if u == nil {
		return fmt.Errorf("The userHasCoupon filter requires an authorized user")
	}
	if *userHasCoupon != u.ID && !u.IsEmployeeOrAdmin() {
		return fmt.Errorf("Unauthorized access: %s is not an employee or admin", u.ID)
	}
	return nil
}
