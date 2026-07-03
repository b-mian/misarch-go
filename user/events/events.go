// Package events defines the Dapr pub/sub payloads the user service consumes
// and emits, plus thin helpers to publish them. Topic strings and JSON field
// names/casing are copied verbatim from the original Kotlin service and must
// not drift — other services subscribe to these exact shapes.
package events

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Topic constants mirror org.misarch.user.event.UserEvents.
const (
	// TopicUserCreated is published once after a user row is created.
	TopicUserCreated = "user/user/created"
	// TopicUserCreate is subscribed: "please create this user" (emitted by the
	// gateway / Keycloak-integration layer after an account is provisioned).
	TopicUserCreate = "user/user/create"
)

// Publisher is the subset of *dapr.Client this package needs, so callers can
// inject the real client (or a fake in tests).
type Publisher interface {
	Publish(ctx context.Context, topic string, payload any) error
}

// CreateUser is the inbound payload of TopicUserCreate (Kotlin CreateUserDTO,
// camelCase). Birthday/gender/dateJoined are not part of the inbound event —
// they are derived/left NULL by the service.
type CreateUser struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	FirstName string    `json:"firstName"`
	LastName  string    `json:"lastName"`
}

// UserCreated is the outbound payload of TopicUserCreated (Kotlin UserDTO,
// camelCase). dateJoined is a STRING (the original formatted the OffsetDateTime
// with ISO_OFFSET_DATE_TIME), not a JSON timestamp object.
type UserCreated struct {
	ID         uuid.UUID `json:"id"`
	Username   string    `json:"username"`
	FirstName  string    `json:"firstName"`
	LastName   string    `json:"lastName"`
	DateJoined string    `json:"dateJoined"`
}

// FormatDateJoined renders a timestamp the way the created-event payload
// expects: an ISO-8601 / RFC-3339 string in UTC with sub-second precision,
// matching the DateTime scalar wire format used everywhere else.
func FormatDateJoined(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// PublishUserCreated publishes a UserCreated event.
func PublishUserCreated(ctx context.Context, p Publisher, e UserCreated) error {
	return p.Publish(ctx, TopicUserCreated, e)
}
