package graph

import (
	"fmt"

	"github.com/google/uuid"
)

// This file centralizes the user-facing error messages, copied byte-for-byte
// from the original Rust service (including capitalization, backticks around
// UUIDs, and trailing periods). The Rust `query_object` used type_name::<T>()
// which yields a fully-qualified path; the spec mandates reproducing the SHORT
// type name (e.g. "User", "ProductVariant", "Review"), so these helpers hard-
// code the short names the spec lists.

// errNotFound renders `"<Type> with UUID: `<id>` not found."` — the message
// query_object produces on a miss or Mongo transport error.
func errNotFound(typeName string, id uuid.UUID) error {
	return fmt.Errorf("%s with UUID: `%s` not found.", typeName, id)
}

// errUserNotFound is used when a user shadow document is absent (createReview
// validation) or on a Mongo error there.
func errUserNotFound(id uuid.UUID) error { return errNotFound("User", id) }

// errProductVariantNotFound is used when re-fetching the full product variant
// during createReview (query_object on product_variants).
func errProductVariantNotFound(id uuid.UUID) error { return errNotFound("ProductVariant", id) }

// errReviewNotFound is used when fetching a review by id (update/delete).
func errReviewNotFound(id uuid.UUID) error { return errNotFound("Review", id) }

// errProductVariantNotPresent renders the validate_product_variant_id message.
func errProductVariantNotPresent(id uuid.UUID) error {
	return fmt.Errorf("Product variant with the UUID: `%s` is not present in the system.", id)
}

// errAlreadyReviewed renders the review_is_already_written_by_user message.
func errAlreadyReviewed(userID, variantID uuid.UUID) error {
	return fmt.Errorf(
		"User of UUID: `%s` has already written a review for product variant of UUID: `%s`.",
		userID, variantID,
	)
}

// errRetrievingReviews is the message returned on any Mongo error while paging
// reviews (Query.reviews and every entity reviews field).
func errRetrievingReviews() error {
	return fmt.Errorf("Retrieving reviews failed in MongoDB.")
}

// errAddingReview is the message returned when the review insert fails.
func errAddingReview() error {
	return fmt.Errorf("Adding review failed in MongoDB.")
}

// errUpdatingBody / errUpdatingRating / errUpdatingVisibility render the
// per-field update failure messages.
func errUpdatingBody(id uuid.UUID) error {
	return fmt.Errorf("Updating body of review of id: `%s` failed in MongoDB.", id)
}

func errUpdatingRating(id uuid.UUID) error {
	return fmt.Errorf("Updating rating of review of id: `%s` failed in MongoDB.", id)
}

func errUpdatingVisibility(id uuid.UUID) error {
	return fmt.Errorf("Updating visibility of review of id: `%s` failed in MongoDB.", id)
}

// errDeletingReview renders the delete failure message.
func errDeletingReview(id uuid.UUID) error {
	return fmt.Errorf("Deleting review of id: `%s` failed in MongoDB.", id)
}

// errNoVisibleReviews renders the null-on-non-null-field error that
// async-graphql produced when averageRating resolved to None (zero visible
// reviews, or the underlying reviews query errored). gqlgen only auto-generates
// this message for nilable return types; averageRating is Float! (a Go float64,
// non-nilable), so the resolver returns this error explicitly to fail the
// field. fieldPath is e.g. "Product.averageRating" or
// "ProductVariant.averageRating".
func errNoVisibleReviews(fieldPath string) error {
	return fmt.Errorf("Cannot return null for non-nullable field %s", fieldPath)
}

// badRatingError signals a stored rating string that cannot be mapped to the
// enum. It carries no user-facing text of its own beyond a generic wrapper; in
// practice the tolerant reader accepts every value the service itself writes,
// so this only triggers for externally-corrupted documents (mirroring the
// original's deserialization failure on such a document).
type badRatingError struct{ value string }

func (e *badRatingError) Error() string {
	return fmt.Sprintf("invalid stored rating value: %q", e.value)
}
