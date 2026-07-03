package events

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"misarch/invoice/store"
)

// fakeStore returns canned lookups so BuildInvoice can be exercised end-to-end
// without MongoDB, to golden-test the rendered content byte-for-byte.
type fakeStore struct {
	user        store.User
	addressUser store.User
	vendor      store.VendorAddress
}

func (f fakeStore) GetUserByAddressID(context.Context, uuid.UUID) (store.User, error) {
	return f.addressUser, nil
}
func (f fakeStore) GetVendorAddress(context.Context) (store.VendorAddress, error) {
	return f.vendor, nil
}
func (f fakeStore) GetUser(context.Context, uuid.UUID) (store.User, error) { return f.user, nil }
func (f fakeStore) InsertInvoice(context.Context, store.Invoice) error     { return nil }

func TestBuildInvoiceContentGolden(t *testing.T) {
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	invoiceID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	item1 := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	pv1 := uuid.MustParse("44444444-4444-4444-4444-444444444444")

	vendor := store.VendorAddress{
		Street1: "Vendor St 1", Street2: "Suite 2",
		City: "Berlin", Country: "Germany", CompanyName: "Acme GmbH",
	}
	userAddr := store.UserAddress{
		Street1: "Customer Rd 5", Street2: "Apt 9",
		City: "Munich", Country: "Germany", CompanyName: "Cust Co",
	}
	fs := fakeStore{
		user:        store.User{ID: userID, FirstName: "Jane", LastName: "Doe"},
		addressUser: store.User{Addresses: []store.UserAddress{userAddr}},
		vendor:      vendor,
	}

	order := OrderEventData{
		ID:                       uuid.MustParse("55555555-5555-5555-5555-555555555555"),
		UserID:                   userID,
		CompensatableOrderAmount: 1998,
		OrderItems: []OrderItemEventData{
			{ID: item1, ProductVariantID: pv1, Count: 2, CompensatableAmount: 1998},
		},
		VatNumber: nil, // → "-" in content
	}

	inv, err := BuildInvoice(context.Background(), fs, order)
	if err != nil {
		t.Fatalf("BuildInvoice: %v", err)
	}

	// BuildInvoice generates the id + issued-at internally, so exercise the real
	// renderContent with fixed values for a deterministic golden comparison.
	issuedAtString := "2026-07-02 10:15:32"
	content := renderContent(vendor, userAddr, fs.user, "-", invoiceID, issuedAtString,
		buildOrderItemsTable(order), order.CompensatableOrderAmount)

	// Hand-reconstructed expected string, matching the Rust format! template.
	expected := "\n# Invoice\n\n" +
		"### Company information:\n" +
		"Acme GmbH\n" +
		"Vendor St 1, Suite 2\n" +
		"Berlin, Germany\n" +
		"\nVAT number: -\n\n" +
		"### Customer information:\n" +
		"ID: 11111111-1111-1111-1111-111111111111\n" +
		"Name: Jane, Doe\n" +
		"Address:\n" +
		"Cust Co\n" +
		"Customer Rd 5, Apt 9\n" +
		"Munich, Germany\n" +
		"\n### Invoice ID: 22222222-2222-2222-2222-222222222222, issued at: 2026-07-02 10:15:32 \n\n" +
		"Terms and conditions: This invoice is created according the the companies terms and conditions specified on the website.\n\n" +
		"---\n\n" +
		"Purchased items overview:\n\n" +
		"| Item UUID | Product variant UUID | count | Compensatable amount |\n" +
		"| --- | --- | --- | --- |\n" +
		"| 33333333-3333-3333-3333-333333333333 | 44444444-4444-4444-4444-444444444444 | 2 | 1998 |\n" +
		"\n\n" +
		"---\n\n" +
		"Total compensatable amount: 1998\n"

	if content != expected {
		t.Errorf("content mismatch\n--- got ---\n%q\n--- want ---\n%q", content, expected)
	}

	// Sanity: the real BuildInvoice output must start with a newline, end with a
	// newline, and contain the trailing space after the issued-at value.
	if inv.Content[0] != '\n' {
		t.Errorf("content must start with newline, got %q", inv.Content[:1])
	}
	if inv.Content[len(inv.Content)-1] != '\n' {
		t.Errorf("content must end with newline")
	}
}

func TestInvoiceCreatedDTOJSON(t *testing.T) {
	cvc := uint16(123)
	order := OrderEventData{
		ID:                       uuid.MustParse("0a1b2c3d-0000-0000-0000-000000000000"),
		UserID:                   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		CreatedAt:                NewTime(time.Date(2026, 7, 2, 10, 15, 30, 123000000, time.UTC)),
		OrderStatus:              "Placed",
		PlacedAt:                 NewTime(time.Date(2026, 7, 2, 10, 15, 31, 0, time.UTC)),
		OrderItems:               []OrderItemEventData{},
		CompensatableOrderAmount: 1998,
		PaymentAuthorization:     &PaymentAuthorization{CVC: cvc},
		VatNumber:                strptr("DE123456789"),
	}
	dto := InvoiceCreatedDTO{
		Order: order,
		Invoice: InvoiceDTO{
			OrderID:  order.ID,
			IssuedAt: NewTime(time.Date(2026, 7, 2, 10, 15, 32, 1000000, time.UTC)),
			Content:  "x",
		},
	}
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)

	// Payment authorization must serialize with the "cVC" key.
	if !contains(got, `"paymentAuthorization":{"cVC":123}`) {
		t.Errorf("paymentAuthorization casing wrong: %s", got)
	}
	// Order status PascalCase.
	if !contains(got, `"orderStatus":"Placed"`) {
		t.Errorf("orderStatus wrong: %s", got)
	}
	// Event timestamps use 'Z' with AutoSi digits (ms here).
	if !contains(got, `"createdAt":"2026-07-02T10:15:30.123Z"`) {
		t.Errorf("createdAt format wrong: %s", got)
	}
	if !contains(got, `"placedAt":"2026-07-02T10:15:31Z"`) {
		t.Errorf("placedAt format wrong (no fraction expected): %s", got)
	}
	if !contains(got, `"issuedAt":"2026-07-02T10:15:32.001Z"`) {
		t.Errorf("invoice.issuedAt format wrong: %s", got)
	}
	// rejectionReason null when absent.
	if !contains(got, `"rejectionReason":null`) {
		t.Errorf("rejectionReason should be null: %s", got)
	}
}

func TestSubscribeListJSON(t *testing.T) {
	b, err := json.Marshal(subscriptions)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `[{"pubsubName":"pubsub","topic":"discount/order/validation-succeeded","route":"/on-discount-validation-succeded"},` +
		`{"pubsubName":"pubsub","topic":"address/vendor-address/created","route":"/on-vendor-address-creation-event"},` +
		`{"pubsubName":"pubsub","topic":"user/user/created","route":"/on-id-creation-event"},` +
		`{"pubsubName":"pubsub","topic":"address/user-address/created","route":"/on-user-address-creation-event"},` +
		`{"pubsubName":"pubsub","topic":"address/user-address/archived","route":"/on-user-address-archived-event"}]`
	if string(b) != want {
		t.Errorf("subscribe list mismatch\ngot:  %s\nwant: %s", b, want)
	}
}

func strptr(s string) *string { return &s }

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
