package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// notificationColumns is the projection for a full Notification row (physical,
// lowercase catalog names — unquoted DDL folds identifiers to lowercase).
const notificationColumns = "id, title, body, datesent, dateread, userid"

// NotificationOrderByID is the sole ORDER BY column set for the notifications
// connection: the ID field maps to the id column (there is no secondary
// tiebreaker — id is already unique and the only order field the SDL exposes).
var NotificationOrderByID = []string{"id"}

// GetNotification loads a notification by id, returning an error if it does not
// exist. The original data loader asserted non-null (entities[id]!!), so a
// missing id surfaces as a GraphQL error rather than null.
func (s *Store) GetNotification(ctx context.Context, id uuid.UUID) (Notification, error) {
	n, err := scanNotificationRow(s.pool.QueryRow(ctx,
		"SELECT "+notificationColumns+" FROM notificationentity WHERE id = $1", id,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Notification{}, fmt.Errorf("notification with id %s not found", id)
	}
	if err != nil {
		return Notification{}, fmt.Errorf("get notification %s: %w", id, err)
	}
	return n, nil
}

// CreateNotification validates that the recipient exists, then inserts a
// notification with the DB-generated id (uuid_generate_v4() default), the given
// dateSent, and a null dateRead. The exists-check and insert share one
// transaction (the original mutation runs REQUIRES_NEW; the event handler uses
// the same service call). A missing user yields the exact error the original
// IllegalArgumentException carried, so callers surface "User with id <uuid>
// does not exist".
func (s *Store) CreateNotification(
	ctx context.Context, title, body string, userID uuid.UUID, dateSent time.Time,
) (Notification, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Notification{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists bool
	if err := tx.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM userentity WHERE id = $1)", userID,
	).Scan(&exists); err != nil {
		return Notification{}, fmt.Errorf("check user exists %s: %w", userID, err)
	}
	if !exists {
		return Notification{}, fmt.Errorf("User with id %s does not exist", userID)
	}

	// The id column is omitted so the uuid_generate_v4() default generates it;
	// RETURNING reads it (and the server-set dateSent) back.
	n, err := scanNotificationRow(tx.QueryRow(ctx,
		"INSERT INTO notificationentity (title, body, datesent, dateread, userid) "+
			"VALUES ($1, $2, $3, NULL, $4) RETURNING "+notificationColumns,
		title, body, dateSent, userID,
	))
	if err != nil {
		return Notification{}, fmt.Errorf("insert notification: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Notification{}, err
	}
	return n, nil
}

// SetNotificationRead applies the read-state toggle and returns the updated
// row. It mirrors the original updateNotification save semantics: the read
// timestamp is only rewritten when the read-state actually flips (so re-marking
// an already-read notification read keeps the original timestamp; marking
// unread nulls it); the row is saved (UPDATE issued) in both cases. currentRead
// is the dateRead of the notification already loaded (and authorized) by the
// caller, matching the original's load-then-modify-then-save flow.
func (s *Store) SetNotificationRead(
	ctx context.Context, id uuid.UUID, currentRead *time.Time, isRead bool,
) (Notification, error) {
	newRead := currentRead
	if (currentRead != nil) != isRead {
		if isRead {
			now := time.Now()
			newRead = &now
		} else {
			newRead = nil
		}
	}

	n, err := scanNotificationRow(s.pool.QueryRow(ctx,
		"UPDATE notificationentity SET dateread = $2 WHERE id = $1 RETURNING "+notificationColumns,
		id, newRead,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Notification{}, fmt.Errorf("notification with id %s not found", id)
	}
	if err != nil {
		return Notification{}, fmt.Errorf("update notification %s: %w", id, err)
	}
	return n, nil
}

// ListNotifications returns a page of a user's notifications ordered by the
// given column set and direction. It runs a single query (the connection's
// nodes field); totalCount and hasNextPage are separate queries.
func (s *Store) ListNotifications(
	ctx context.Context, userID uuid.UUID, first, skip *int, orderCols []string, ascending bool,
) ([]Notification, error) {
	p := notificationPage(userID, first, skip, orderCols, ascending)
	q, args := p.nodesSQL()
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	return scanNotifications(rows)
}

// CountNotifications returns the total number of a user's notifications,
// ignoring pagination arguments (the original totalCount()).
func (s *Store) CountNotifications(ctx context.Context, userID uuid.UUID) (int, error) {
	return notificationPage(userID, nil, nil, NotificationOrderByID, true).totalCount(ctx, s.pool)
}

// NotificationsHasNextPage reports whether a page beyond first+skip exists. It
// mirrors the original: first == nil always yields false.
func (s *Store) NotificationsHasNextPage(
	ctx context.Context, userID uuid.UUID, first, skip *int, orderCols []string, ascending bool,
) (bool, error) {
	return notificationPage(userID, first, skip, orderCols, ascending).hasNextPage(ctx, s.pool)
}

// notificationPage builds the page descriptor for a user's notifications
// (predicate userid = $1).
func notificationPage(userID uuid.UUID, first, skip *int, orderCols []string, ascending bool) page {
	return page{
		table:     "notificationentity",
		columns:   notificationColumns,
		where:     "userid = $1",
		whereArgs: []any{userID},
		orderCols: orderCols,
		ascending: ascending,
		first:     first,
		skip:      skip,
	}
}

// scanNotificationRow scans a single notification row.
func scanNotificationRow(row pgx.Row) (Notification, error) {
	var n Notification
	err := row.Scan(&n.ID, &n.Title, &n.Body, &n.DateSent, &n.DateRead, &n.UserID)
	return n, err
}

// scanNotifications collects notification rows from an open pgx.Rows.
func scanNotifications(rows pgx.Rows) ([]Notification, error) {
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.Title, &n.Body, &n.DateSent, &n.DateRead, &n.UserID); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
