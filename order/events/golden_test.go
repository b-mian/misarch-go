package events

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestOrderDTOJSON verifies the order/order/created payload serializes with the
// exact field names, casing, enum form, Z-suffixed timestamps, and the {"cVC"}
// payment-authorization shape the original produces.
func TestOrderDTOJSON(t *testing.T) {
	id := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	created := time.Date(2026, 7, 2, 10, 15, 30, 123_000_000, time.UTC)
	placed := time.Date(2026, 7, 2, 10, 20, 0, 0, time.UTC)
	cvc := &PaymentAuthorization{CVC: 123}
	vat := "DE123456789"

	dto := OrderDTO{
		ID:                       id,
		UserID:                   id,
		CreatedAt:                NewTime(created),
		OrderStatus:              "PLACED",
		PlacedAt:                 NewTime(placed),
		RejectionReason:          nil,
		OrderItems:               []OrderItemDTO{},
		ShipmentAddressID:        id,
		InvoiceAddressID:         id,
		CompensatableOrderAmount: 2700,
		PaymentInformationID:     id,
		PaymentAuthorization:     cvc,
		VatNumber:                &vat,
	}
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)

	// Round-trip into a generic map to assert individual fields robustly.
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["orderStatus"] != "PLACED" {
		t.Errorf("orderStatus = %v, want PLACED", m["orderStatus"])
	}
	if m["createdAt"] != "2026-07-02T10:15:30.123Z" {
		t.Errorf("createdAt = %v, want 2026-07-02T10:15:30.123Z", m["createdAt"])
	}
	if m["placedAt"] != "2026-07-02T10:20:00Z" {
		t.Errorf("placedAt = %v, want 2026-07-02T10:20:00Z (AutoSi trims zero fraction)", m["placedAt"])
	}
	if _, ok := m["rejectionReason"]; !ok {
		t.Errorf("rejectionReason key must be present (null), got body %s", got)
	}
	if m["rejectionReason"] != nil {
		t.Errorf("rejectionReason = %v, want null", m["rejectionReason"])
	}
	// paymentAuthorization must be {"cVC": 123}.
	pa, ok := m["paymentAuthorization"].(map[string]any)
	if !ok {
		t.Fatalf("paymentAuthorization not an object: %s", got)
	}
	if _, ok := pa["cVC"]; !ok {
		t.Errorf("paymentAuthorization key must be cVC, got %v", pa)
	}
	if pa["cVC"].(float64) != 123 {
		t.Errorf("cVC = %v, want 123", pa["cVC"])
	}
	// camelCase field names present.
	for _, k := range []string{"userId", "shipmentAddressId", "invoiceAddressId", "compensatableOrderAmount", "paymentInformationId", "vatNumber", "orderItems"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing camelCase field %q in %s", k, got)
		}
	}
}

// TestOrderDTONullPaymentAuth verifies an absent payment authorization
// serializes as JSON null.
func TestOrderDTONullPaymentAuth(t *testing.T) {
	dto := OrderDTO{OrderItems: []OrderItemDTO{}}
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := m["paymentAuthorization"]; !ok || v != nil {
		t.Errorf("paymentAuthorization = %v (present=%v), want null present", m["paymentAuthorization"], ok)
	}
	if v, ok := m["vatNumber"]; !ok || v != nil {
		t.Errorf("vatNumber = %v, want null", v)
	}
}

// TestOrderItemDTOJSON checks the order-item DTO field names and casing.
func TestOrderItemDTOJSON(t *testing.T) {
	id := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	dto := OrderItemDTO{
		ID:                      id,
		CreatedAt:               NewTime(time.Unix(0, 0).UTC()),
		ProductVariantID:        id,
		ProductVariantVersionID: id,
		TaxRateVersionID:        id,
		ShoppingCartItemID:      id,
		Count:                   3,
		CompensatableAmount:     2700,
		ShipmentMethodID:        id,
		DiscountIDs:             []uuid.UUID{id},
	}
	b, _ := json.Marshal(dto)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"id", "createdAt", "productVariantId", "productVariantVersionId", "taxRateVersionId", "shoppingCartItemId", "count", "compensatableAmount", "shipmentMethodId", "discountIds"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing field %q in %s", k, string(b))
		}
	}
}

// TestOrderCompensationDTOJSON verifies the compensation payload stays
// snake_case (amount_to_compensate) — NOT camelCase.
func TestOrderCompensationDTOJSON(t *testing.T) {
	dto := OrderCompensationDTO{
		ID:                 uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		AmountToCompensate: 2700,
	}
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["amount_to_compensate"]; !ok {
		t.Errorf("expected snake_case amount_to_compensate, got %s", string(b))
	}
	if _, ok := m["amountToCompensate"]; ok {
		t.Errorf("must NOT be camelCase amountToCompensate: %s", string(b))
	}
	if m["amount_to_compensate"].(float64) != 2700 {
		t.Errorf("amount_to_compensate = %v, want 2700", m["amount_to_compensate"])
	}
}
