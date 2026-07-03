package events

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"misarch/invoice/store"
)

// invoiceTerms is the static terms-and-conditions line embedded in every
// invoice. The "according the the" wording is a typo in the original and is
// preserved byte-for-byte.
const invoiceTerms = "This invoice is created according the the companies terms and conditions specified on the website."

// contentTemplate is the exact Rust format! template for the invoice content.
// It begins with a newline and ends with a newline; note the LITERAL TRAILING
// SPACE after the issued-at value on the "### Invoice ID:" line ("issued at: %s "
// then newline). It is written with explicit \n escapes so no editor/tool can
// strip that space. The verbs are filled in the same order as the original.
const contentTemplate = "\n# Invoice\n\n" +
	"### Company information:\n" +
	"%s\n" + // vendor.company_name
	"%s, %s\n" + // vendor.street1, vendor.street2
	"%s, %s\n" + // vendor.city, vendor.country
	"\nVAT number: %s\n\n" + // vatNumberOrDash
	"### Customer information:\n" +
	"ID: %s\n" + // user._id
	"Name: %s, %s\n" + // user.first_name, user.last_name
	"Address:\n" +
	"%s\n" + // userAddress.company_name
	"%s, %s\n" + // userAddress.street1, userAddress.street2
	"%s, %s\n" + // userAddress.city, userAddress.country
	"\n### Invoice ID: %s, issued at: %s \n\n" + // invoice._id, issuedAtString (note trailing space)
	"Terms and conditions: %s\n\n" + // INVOICE_TERMS
	"---\n\n" +
	"Purchased items overview:\n\n" +
	"%s\n\n" + // itemsTable (already ends with \n)
	"---\n\n" +
	"Total compensatable amount: %d\n" // compensatableOrderAmount

// Store is the subset of the persistence layer the invoice builder needs.
type Store interface {
	GetUserByAddressID(ctx context.Context, addressID uuid.UUID) (store.User, error)
	GetVendorAddress(ctx context.Context) (store.VendorAddress, error)
	GetUser(ctx context.Context, id uuid.UUID) (store.User, error)
	InsertInvoice(ctx context.Context, inv store.Invoice) error
}

// BuildInvoice assembles a new invoice document from an order event, following
// the original Rust Invoice::new / invoice_attribute_setup sequence exactly:
//
//  1. generate a random UUIDv4 for the invoice _id;
//  2. issued_at = now (UTC, ms precision) and its "%Y-%m-%d %H:%M:%S" string;
//  3. build the order-items Markdown table;
//  4. look up the invoice address (via the buggy addresses/$elemMatch query);
//  5. project the first address out of the found user;
//  6. load the vendor address (first doc, no filter);
//  7. load the ordering user by userId;
//  8. render the content (VAT falls back to "-" only inside the content);
//  9. build the Invoice document (vat_number keeps the original null).
//
// Any step returning an error aborts creation; the caller maps that to a 500
// (the original swallows the specific error into INTERNAL_SERVER_ERROR).
func BuildInvoice(ctx context.Context, s Store, order OrderEventData) (store.Invoice, error) {
	id := uuid.New()

	issuedAt := time.Now().UTC().Truncate(time.Millisecond)
	issuedAtString := issuedAt.Format("2006-01-02 15:04:05")

	itemsTable := buildOrderItemsTable(order)

	addrUser, err := s.GetUserByAddressID(ctx, order.InvoiceAddressID)
	if err != nil {
		return store.Invoice{}, err
	}
	userAddress, err := projectUserAddress(addrUser)
	if err != nil {
		return store.Invoice{}, err
	}
	vendorAddress, err := s.GetVendorAddress(ctx)
	if err != nil {
		return store.Invoice{}, err
	}
	user, err := s.GetUser(ctx, order.UserID)
	if err != nil {
		return store.Invoice{}, err
	}

	vatForContent := "-"
	if order.VatNumber != nil {
		vatForContent = *order.VatNumber
	}

	content := renderContent(vendorAddress, userAddress, user, vatForContent,
		id, issuedAtString, itemsTable, order.CompensatableOrderAmount)

	return store.Invoice{
		ID:            id,
		OrderID:       order.ID,
		IssuedAt:      issuedAt,
		Content:       content,
		UserAddress:   userAddress,
		VendorAddress: vendorAddress,
		VatNumber:     order.VatNumber,
	}, nil
}

// renderContent fills the invoice content template in the exact argument order
// of the original Rust format!. Extracted so it can be golden-tested directly.
func renderContent(vendor store.VendorAddress, userAddress store.UserAddress, user store.User,
	vatForContent string, id uuid.UUID, issuedAtString, itemsTable string, amount uint64) string {
	return fmt.Sprintf(contentTemplate,
		vendor.CompanyName,
		vendor.Street1, vendor.Street2,
		vendor.City, vendor.Country,
		vatForContent,
		user.ID,
		user.FirstName, user.LastName,
		userAddress.CompanyName,
		userAddress.Street1, userAddress.Street2,
		userAddress.City, userAddress.Country,
		id,
		issuedAtString,
		invoiceTerms,
		itemsTable,
		amount,
	)
}

// buildOrderItemsTable renders the order items as the Markdown table used in the
// invoice content. The header rows are fixed; each item row is appended in event
// order; the whole string ends with a trailing newline (so the template's
// following blank line + "---" renders correctly).
func buildOrderItemsTable(order OrderEventData) string {
	var b strings.Builder
	b.WriteString("| Item UUID | Product variant UUID | count | Compensatable amount |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, item := range order.OrderItems {
		fmt.Fprintf(&b, "| %s | %s | %d | %d |\n",
			item.ID, item.ProductVariantID, item.Count, item.CompensatableAmount)
	}
	return b.String()
}

// projectUserAddress returns the first address of a user loaded by the
// address-projection query, matching the Rust project_user_to_user_address.
// An empty address slice is an error.
func projectUserAddress(u store.User) (store.UserAddress, error) {
	if len(u.Addresses) == 0 {
		return store.UserAddress{}, fmt.Errorf("Projection failed, address could not be extracted from user.")
	}
	return u.Addresses[0], nil
}

// ToInvoiceDTO builds the invoice half of the published event from a stored
// invoice, mirroring the Rust InvoiceDTO::from(Invoice).
func ToInvoiceDTO(inv store.Invoice) InvoiceDTO {
	return InvoiceDTO{
		OrderID:  inv.OrderID,
		IssuedAt: NewTime(inv.IssuedAt),
		Content:  inv.Content,
	}
}
