package graph

import (
	"fmt"

	"github.com/google/uuid"

	"misarch/invoice/store"
)

// invoiceNotFound builds the exact error the Rust query_object::<Invoice>
// returns for both not-found and driver errors, including the fully-qualified
// type-name prefix and backticked id. Used by Query.invoice and the Invoice
// entity resolver.
func invoiceNotFound(id uuid.UUID) error {
	return fmt.Errorf("misarch_invoice::graphql::model::invoice::Invoice with UUID: `%s` not found.", id)
}

// invoiceByOrderNotFound builds the error the Rust query_invoice_by_order_id
// returns when no invoice exists for the given order id.
func invoiceByOrderNotFound(orderID uuid.UUID) error {
	return fmt.Errorf("Invoice with order_id UUID: `%s` not found.", orderID)
}

// This file maps store documents to GraphQL models. The invoice's embedded
// user/vendor addresses are stored inline in the same document, so they are
// mapped eagerly here (only their id is exposed by the schema — all other
// address fields are GraphQL-skipped). Order.invoice is likewise populated
// eagerly by the entity resolver, matching the original async-graphql service
// (there are no lazy relationship field resolvers in this subgraph).

// toInvoice maps a store.Invoice to the GraphQL model.
func toInvoice(inv store.Invoice) *Invoice {
	return &Invoice{
		ID:            inv.ID,
		OrderID:       inv.OrderID,
		IssuedAt:      inv.IssuedAt,
		Content:       inv.Content,
		UserAddress:   &UserAddress{ID: inv.UserAddress.ID},
		VendorAddress: &VendorAddress{ID: inv.VendorAddress.ID},
		VatNumber:     inv.VatNumber,
	}
}
