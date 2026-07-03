package graph

import (
	"misarch/notification/store"
)

// This file maps store rows to GraphQL models and GraphQL order inputs to store
// order columns. The relationship/derived fields User and IsRead are left at
// their zero values here: they are populated lazily by their own field
// resolvers (User via the UserID extraField, IsRead computed from DateRead).

// toNotification maps a store.Notification to the GraphQL model.
func toNotification(n store.Notification) *Notification {
	return &Notification{
		ID:       n.ID,
		Title:    n.Title,
		Body:     n.Body,
		DateSent: n.DateSent,
		DateRead: n.DateRead,
		UserID:   n.UserID,
	}
}

// ascending reports whether an OrderDirection means ascending. The default
// (nil) is ASC, matching the original OrderDirection default.
func ascending(d *OrderDirection) bool {
	return d == nil || *d != OrderDirectionDesc
}

// notificationOrder resolves a NotificationOrderInput to a store order column
// set and direction. Defaults: field ID, direction ASC (NotificationOrder.
// DEFAULT); each of field/direction defaults independently when nil.
func notificationOrder(in *NotificationOrderInput) ([]string, bool) {
	if in == nil {
		return store.NotificationOrderByID, true
	}
	// The SDL exposes exactly one order field (ID → id column); any non-nil
	// field value is ID, and a nil field also defaults to ID.
	return store.NotificationOrderByID, ascending(in.Direction)
}
