package graph

import (
	"time"

	"misarch/pkg/scalars"
	"misarch/user/store"
)

// This file maps store rows to GraphQL models and GraphQL order inputs to store
// order columns.

// toUser maps a store.User to the GraphQL model.
//
// The birthday column is a TIMESTAMPTZ but the API type is a calendar Date: the
// stored instant is truncated to its UTC year/month/day. The write path stores
// birthdays at UTC midnight, so a value written then read is unchanged. Gender
// is stored as the enum name text and cast back to the Gender enum (the
// original only ever wrote valid names).
func toUser(u store.User) *User {
	return &User{
		ID:         u.ID,
		Username:   u.Username,
		Name:       &Name{FirstName: u.FirstName, LastName: u.LastName},
		Birthday:   toDate(u.Birthday),
		Gender:     toGender(u.Gender),
		DateJoined: u.DateJoined,
	}
}

// toDate truncates a stored timestamp to a calendar Date using its UTC
// year/month/day, or returns nil for a NULL birthday.
func toDate(t *time.Time) *scalars.Date {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &scalars.Date{Time: time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)}
}

// toGender converts the stored gender name text to the Gender enum, or nil for
// a NULL gender.
func toGender(g *string) *Gender {
	if g == nil {
		return nil
	}
	gender := Gender(*g)
	return &gender
}

// toUserConnection maps a paginated store result to the GraphQL connection.
func toUserConnection(c store.Connection[store.User]) *UserConnection {
	nodes := make([]User, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toUser(n)
	}
	return &UserConnection{
		Nodes:       nodes,
		TotalCount:  c.TotalCount,
		HasNextPage: c.HasNextPage,
	}
}

// ascending reports whether an OrderDirection means ascending. The default
// (nil) is ASC, matching the original OrderDirection default.
func ascending(d *OrderDirection) bool {
	return d == nil || *d != OrderDirectionDesc
}

// userOrder resolves a UserOrderInput to a store order column set and a
// direction. Defaults: field ID, direction ASC (UserOrder.DEFAULT). USERNAME
// maps to {username, id} (a secondary id tiebreaker sharing the direction).
func userOrder(in *UserOrderInput) (store.UserOrderColumn, bool) {
	if in == nil {
		return store.UserOrderByID, true
	}
	col := store.UserOrderByID
	if field := in.Field.Value(); field != nil && *field == UserOrderFieldUsername {
		col = store.UserOrderByUsername
	}
	return col, ascending(in.Direction.Value())
}
