// Package events defines the Dapr pub/sub topics the notification service
// subscribes to and the JSON payloads it consumes. The notification service is
// a pure sink: it PUBLISHES nothing and makes no outbound Dapr calls. Topic and
// route strings and payload JSON field names are copied verbatim from the
// original Kotlin service (org.misarch.notification.event) and must not drift —
// the user service and every other publisher target these exact shapes.
package events

import "github.com/google/uuid"

// Topic constants mirror org.misarch.notification.event.NotificationEvents.
const (
	// TopicUserCreated carries a newly-registered user; the handler stores the
	// id in the local replica so it can back the notification FK.
	TopicUserCreated = "user/user/created"
	// TopicNotificationCreate lets any service request a notification be sent
	// to a user (same code path as the createNotification mutation, no auth).
	TopicNotificationCreate = "notification/notification/create"
)

// Route constants are the HTTP delivery paths Dapr POSTs CloudEvents to. They
// are "/subscription/" + the topic, matching the original @PostMapping routes.
const (
	RouteUserCreated        = "/subscription/" + TopicUserCreated
	RouteNotificationCreate = "/subscription/" + TopicNotificationCreate
)

// UserCreated is the CloudEvent data of TopicUserCreated. Mirrors UserDTO: only
// the id is consumed; any extra fields are ignored by deserialization.
type UserCreated struct {
	ID uuid.UUID `json:"id"`
}

// NotificationCreate is the CloudEvent data of TopicNotificationCreate. Mirrors
// NotificationDTO: title, body, and the recipient user id.
type NotificationCreate struct {
	Title  string    `json:"title"`
	Body   string    `json:"body"`
	UserID uuid.UUID `json:"userId"`
}
