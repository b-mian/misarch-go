package store

import (
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// condBuilder assembles a parameterized SQL condition by AND-ing fragments,
// numbering placeholders ($1, $2, ...) sequentially as arguments are added. It
// mirrors the original BaseConnection.buildCondition, which AND-s the field
// predicate, the filter expression, and the authorized-user filter, omitting
// any that are null (here: any fragment not appended).
type condBuilder struct {
	parts []string
	args  []any
}

// placeholder registers one argument and returns its $N placeholder token.
func (c *condBuilder) placeholder(arg any) string {
	c.args = append(c.args, arg)
	return "$" + strconv.Itoa(len(c.args))
}

// addRaw appends a pre-rendered SQL fragment (already using placeholders from
// prior placeholder() calls, or none). Empty fragments are ignored.
func (c *condBuilder) addRaw(sql string) {
	if sql != "" {
		c.parts = append(c.parts, sql)
	}
}

// eq appends "<col> = $N" binding arg.
func (c *condBuilder) eq(col string, arg any) {
	c.parts = append(c.parts, col+" = "+c.placeholder(arg))
}

// where returns the combined predicate (AND of all parts) with no WHERE
// keyword, and the collected args. An empty predicate yields "".
func (c *condBuilder) where() (string, []any) {
	if len(c.parts) == 0 {
		return "", nil
	}
	return strings.Join(c.parts, " AND "), c.args
}

// couponAuthFilterSQL renders the CouponConnection authorizedUserFilter (§6):
//   - anonymous (authUser == nil)  -> "FALSE" (connection always empty),
//   - employee/admin               -> "" (no extra restriction),
//   - authenticated non-employee   -> only coupons the caller has redeemed.
//
// It appends any needed argument to b and returns the fragment (possibly "").
func couponAuthFilterSQL(b *condBuilder, authUser *AuthUser) string {
	if authUser == nil {
		return "FALSE"
	}
	if authUser.IsEmployee() {
		return ""
	}
	ph := b.placeholder(authUser.ID)
	return "couponentity.id IN (SELECT couponredemptionentity.couponid FROM couponredemptionentity WHERE couponredemptionentity.userid = " + ph + ")"
}

// userHasCouponSQL renders the CouponFilterInput.userHasCoupon condition and the
// standalone userHasCouponCondition used above: coupons redeemed by userID.
func userHasCouponSQL(b *condBuilder, userID uuid.UUID) string {
	ph := b.placeholder(userID)
	return "couponentity.id IN (SELECT couponredemptionentity.couponid FROM couponredemptionentity WHERE couponredemptionentity.userid = " + ph + ")"
}

// AuthUser is the minimal authenticated-user view the store needs for the
// coupon connection's user-scoped filters. It is populated by the graph layer
// from the request's auth context (nil = anonymous).
type AuthUser struct {
	ID       uuid.UUID
	Employee bool
}

// IsEmployee reports whether the user is an employee or admin.
func (u *AuthUser) IsEmployee() bool { return u != nil && u.Employee }
