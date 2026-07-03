// Package auth parses the Authorized-User header injected by the MiSArch
// gateway after JWT validation and exposes role/ownership checks used by the
// service resolvers.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
)

// HeaderName is the header the gateway sets after validating the Keycloak JWT.
const HeaderName = "Authorized-User"

// Roles recognized by the gateway (see gateway/envelopPlugins.ts).
const (
	RoleAdmin    = "admin"
	RoleEmployee = "employee"
	RoleBuyer    = "buyer"
)

// User is the JSON payload of the Authorized-User header.
type User struct {
	ID    uuid.UUID `json:"id"`
	Roles []string  `json:"roles"`
}

// ErrUnauthorized is returned by the Ensure* helpers; services surface it as a
// GraphQL error.
var ErrUnauthorized = errors.New("unauthorized: user is not allowed to perform this action")

// ErrUnauthenticated is returned when no valid Authorized-User header exists.
var ErrUnauthenticated = errors.New("unauthenticated: no authenticated user present")

type ctxKey struct{}

// Middleware extracts the Authorized-User header (if present) into the request
// context. An absent or malformed header yields an anonymous context.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get(HeaderName)
		if raw != "" {
			var u User
			if err := json.Unmarshal([]byte(raw), &u); err == nil && u.ID != uuid.Nil {
				r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, &u))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// FromContext returns the authenticated user, or nil for anonymous requests.
func FromContext(ctx context.Context) *User {
	u, _ := ctx.Value(ctxKey{}).(*User)
	return u
}

// HasRole reports whether the user has the given role.
func (u *User) HasRole(role string) bool {
	if u == nil {
		return false
	}
	for _, r := range u.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// IsEmployeeOrAdmin reports whether the user has the employee or admin role.
func (u *User) IsEmployeeOrAdmin() bool {
	return u.HasRole(RoleEmployee) || u.HasRole(RoleAdmin)
}

// Require returns the authenticated user or ErrUnauthenticated.
func Require(ctx context.Context) (*User, error) {
	u := FromContext(ctx)
	if u == nil {
		return nil, ErrUnauthenticated
	}
	return u, nil
}

// RequireRole returns the user if it has one of the given roles.
func RequireRole(ctx context.Context, roles ...string) (*User, error) {
	u, err := Require(ctx)
	if err != nil {
		return nil, err
	}
	for _, role := range roles {
		if u.HasRole(role) {
			return u, nil
		}
	}
	return nil, ErrUnauthorized
}

// RequireEmployeeOrAdmin returns the user if it is an employee or admin.
func RequireEmployeeOrAdmin(ctx context.Context) (*User, error) {
	return RequireRole(ctx, RoleEmployee, RoleAdmin)
}

// RequireSelfOrEmployee authorizes access to a resource owned by owner: the
// caller must be that user, an employee, or an admin.
func RequireSelfOrEmployee(ctx context.Context, owner uuid.UUID) (*User, error) {
	u, err := Require(ctx)
	if err != nil {
		return nil, err
	}
	if u.ID == owner || u.IsEmployeeOrAdmin() {
		return u, nil
	}
	return nil, ErrUnauthorized
}
