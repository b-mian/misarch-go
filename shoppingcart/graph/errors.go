package graph

import (
	"errors"
	"fmt"

	"github.com/google/uuid"

	"misarch/shoppingcart/store"
)

// This file collects the GraphQL error strings the resolvers return. They are
// copied byte-for-byte from the original Rust service (async_graphql::Error
// messages), including the backtick-quoting of UUIDs and trailing periods, so
// error semantics match exactly. The one intentional deviation is the "user
// not found" string, which drops the Rust-internal module-path prefix (no
// consumer depends on it — see spec §3).

// errorsIs is a thin alias so the generated-file resolvers can test sentinel
// errors without importing errors directly in that file.
func errorsIs(err, target error) bool { return errors.Is(err, target) }

// itemNotFoundError is the "ShoppingCartItem of UUID ... not found." error.
func itemNotFoundError(id uuid.UUID) error {
	return fmt.Errorf("ShoppingCartItem of UUID: `%s` not found.", id)
}

// itemLookupError maps a store item-lookup error to the GraphQL error the
// original service returned: ErrNotFound → "ShoppingCartItem of UUID ... not
// found."; ErrProjectionFailed → the projection message; any other (driver)
// error is surfaced as-is.
func itemLookupError(id uuid.UUID, err error) error {
	switch {
	case errorsIs(err, store.ErrNotFound):
		return itemNotFoundError(id)
	case errorsIs(err, store.ErrProjectionFailed):
		return projectionFailedError()
	default:
		return err
	}
}

// projectionFailedError is returned when a matched user unexpectedly has no
// projected cart item (Rust project_user_to_shopping_cart_item failure).
func projectionFailedError() error {
	return errors.New("Projection failed, shoppingcart item could not be extracted from user.")
}

// userNotFoundError is the user-not-found error (module path dropped vs Rust).
func userNotFoundError(id uuid.UUID) error {
	return fmt.Errorf("User with UUID: `%s` not found.", id)
}

// cartNotFoundError is returned when a cart re-query finds no user (Rust
// query_shoppingcart None branch).
func cartNotFoundError(id uuid.UUID) error {
	return fmt.Errorf("ShoppingCart with UUID: `%s` not found.", id)
}

// productVariantNotPresentError is the single-variant existence error (used for
// both not-found and driver-error, matching the Rust behavior).
func productVariantNotPresentError(id uuid.UUID) error {
	return fmt.Errorf("Product variant with the UUID: `%s` is not present in the system.", id)
}

// productVariantsQueryError is the multi-variant driver-error message (Rust
// validate_shopping_cart_items Err branch).
func productVariantsQueryError() error {
	return errors.New("Product variants with the specified UUIDs are not present in the system.")
}

// replaceCartFailedError is returned when the cart-replace update fails.
func replaceCartFailedError(id uuid.UUID) error {
	return fmt.Errorf("Updating product_variant_ids of shoppingcart of id: `%s` failed in MongoDB.", id)
}

// addItemFailedError is returned when the $push of a new item fails.
func addItemFailedError(newID uuid.UUID) error {
	return fmt.Errorf("Add shoppingcart item of id: `%s` failed in MongoDB.", newID)
}

// updateCountFailedError is returned when the item-count update fails.
func updateCountFailedError(id uuid.UUID) error {
	return fmt.Errorf("Updating count of shoppingcart item of id: `%s` failed in MongoDB.", id)
}

// deleteItemFailedError is returned when the item $pull fails.
func deleteItemFailedError(id uuid.UUID) error {
	return fmt.Errorf("Deleting shoppingcart item of id: `%s` failed in MongoDB.", id)
}
