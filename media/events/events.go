// Package events defines the single Dapr pub/sub payload the media service
// emits and a thin helper to publish it. The topic string and JSON field name
// are copied verbatim from the original Rust service (misarch-media) and must
// not drift — subscribers depend on these exact shapes.
package events

import (
	"context"

	"github.com/google/uuid"
)

// TopicMediaCreated is published once when a media file is uploaded. The topic
// literally contains slashes: everything after `.../publish/pubsub/` is the
// Dapr topic, so the published URL is
// POST http://localhost:3500/v1.0/publish/pubsub/media/media/created.
const TopicMediaCreated = "media/media/created"

// Publisher is the subset of *dapr.Client this package needs, so callers can
// inject the real client (or a fake in tests). *dapr.Client satisfies it.
type Publisher interface {
	Publish(ctx context.Context, topic string, payload any) error
}

// MediaCreated is the payload of TopicMediaCreated. Mirrors the Rust MediaDTO:
// exactly one field `id` (lowercase; serde default, no rename), serialized as a
// bare UUID string in canonical hyphenated lowercase — NOT a {"$uuid":...} /
// {"$binary":...} wrapper.
type MediaCreated struct {
	ID uuid.UUID `json:"id"`
}

// PublishMediaCreated publishes a MediaCreated event on TopicMediaCreated.
//
// Fidelity note: the Rust service ignores the Dapr publish response entirely
// and fails only on a transport-level error. The platform dapr.Client.Publish
// additionally surfaces a Dapr 3xx+ HTTP status as an error. In the media
// service this is only reachable from uploadMedia, where the object is already
// stored by the time this is called, so an error here maps to the Rust
// "publish transport failure → mutation errors, object already stored (orphan)"
// path. See the media spec §4/§9.
func PublishMediaCreated(ctx context.Context, p Publisher, id uuid.UUID) error {
	return p.Publish(ctx, TopicMediaCreated, MediaCreated{ID: id})
}
