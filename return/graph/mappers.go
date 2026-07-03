package graph

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"misarch/pkg/auth"
	"misarch/return/store"
)

// This file maps store rows to GraphQL models and GraphQL order inputs to store
// order directions, and holds the auth helpers. Relationship fields (Order,
// ReturnedItems, ReturnedWith, Returns) are intentionally left nil here: they
// are populated lazily by their own field resolvers. The extraFields (OrderID,
// ReturnedWithID) carry the FKs those resolvers need.

// toReturn maps a store.Return to the GraphQL model. refundedAmount is narrowed
// from int64 to a signed 32-bit value (Kotlin Long.toInt()), reproducing the
// silent overflow for values outside the int32 range; the event payload keeps
// the full int64.
func toReturn(r store.Return) *Return {
	return &Return{
		ID:             r.ID,
		Reason:         r.Reason,
		RefundedAmount: int(int32(r.RefundedAmount)),
		CreatedAt:      r.CreatedAt,
		OrderID:        r.OrderID,
	}
}

// toReturnConnection maps a paginated store result to the GraphQL connection.
func toReturnConnection(c store.Connection[store.Return]) *ReturnConnection {
	nodes := make([]Return, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toReturn(n)
	}
	return &ReturnConnection{
		Nodes:       nodes,
		TotalCount:  c.TotalCount,
		HasNextPage: c.HasNextPage,
	}
}

// toOrderItem maps a store.OrderItem to the GraphQL model, carrying the
// returnedWithId FK for the returnedWith field resolver.
func toOrderItem(oi store.OrderItem) *OrderItem {
	return &OrderItem{
		ID:             oi.ID,
		ReturnedWithID: oi.ReturnedWithID,
	}
}

// toOrderItemConnection maps a paginated store result to the GraphQL connection.
func toOrderItemConnection(c store.Connection[store.OrderItem]) *OrderItemConnection {
	nodes := make([]OrderItem, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toOrderItem(n)
	}
	return &OrderItemConnection{
		Nodes:       nodes,
		TotalCount:  c.TotalCount,
		HasNextPage: c.HasNextPage,
	}
}

// ascending reports whether an OrderDirection means ascending. The default
// (nil) is ASC, matching OrderDirection.ASC being the enum default and
// ReturnOrder.DEFAULT / CommonOrder.DEFAULT both being (ASC, ID).
func ascending(d *OrderDirection) bool {
	return d == nil || *d != OrderDirectionDesc
}

// returnAscending resolves a ReturnOrderInput to a direction. The only field
// value is ID, so only the direction is meaningful. Default ASC.
func returnAscending(in *ReturnOrderInput) bool {
	if in == nil {
		return true
	}
	return ascending(in.Direction)
}

// commonAscending resolves a CommonOrderInput to a direction (field is always
// ID). Default ASC.
func commonAscending(in *CommonOrderInput) bool {
	if in == nil {
		return true
	}
	return ascending(in.Direction)
}

// errUnauthorized reproduces the original dfe.authorizedUser accessor, which
// throws IllegalStateException("Unauthorized access") when the Authorized-User
// header is absent. The message is preserved verbatim.
var errUnauthorized = errors.New("Unauthorized access")

// requireUser returns the authenticated user or the verbatim "Unauthorized
// access" error (mirrors dfe.authorizedUser).
func requireUser(ctx context.Context) (*auth.User, error) {
	u := auth.FromContext(ctx)
	if u == nil {
		return nil, errUnauthorized
	}
	return u, nil
}

// isEmployee mirrors AuthorizedUser.isEmployee (roles contains "employee" or
// "admin").
func isEmployee(u *auth.User) bool {
	return u.IsEmployeeOrAdmin()
}

// authFilterUserID returns the user id to restrict a connection to when the
// caller is a non-employee, or nil for employees/admins (no restriction) —
// mirroring ReturnConnection.authorizedUserFilter. The user must be non-nil
// (the original dereferences authorizedUser!! and NPEs otherwise; here the
// caller guarantees a non-nil user, matching the gateway always injecting the
// header for these fields).
func authFilterUserID(u *auth.User) *uuid.UUID {
	if isEmployee(u) {
		return nil
	}
	id := u.ID
	return &id
}
