package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// triggerMessage returns the raw Postgres message for a RAISE EXCEPTION from one
// of the business triggers (update_coupon_usages / check_discount_usages), whose
// texts are a contract surfaced to the GraphQL caller: "Coupon has been used too
// often" and "The user has applied the discount too often". For those (SQLSTATE
// P0001, raise_exception) it returns just the message so the GraphQL error text
// matches the original byte-for-byte; otherwise it returns "" so callers keep
// their own wrapping.
func triggerMessage(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "P0001" {
		return pgErr.Message
	}
	return ""
}
