// Package handlers implements the shipment service's non-GraphQL HTTP surface:
// the five Dapr event subscription handlers, the external-provider status
// callback (POST /shipment/{id}/status), and the ECS endpoints
// (GET /ecs/defined-variables, POST /ecs/variables). Incoming event payloads
// are the CloudEvent `data` object; unknown JSON fields are ignored (matching
// Jackson's FAIL_ON_UNKNOWN_PROPERTIES=false).
package handlers

import "github.com/google/uuid"

// userAddressDTO is the data payload of address/user-address/created.
type userAddressDTO struct {
	ID          uuid.UUID `json:"id"`
	Street1     string    `json:"street1"`
	Street2     string    `json:"street2"`
	City        string    `json:"city"`
	PostalCode  string    `json:"postalCode"`
	Country     string    `json:"country"`
	CompanyName *string   `json:"companyName"`
	UserID      uuid.UUID `json:"userId"`
}

// vendorAddressDTO is the data payload of address/vendor-address/created (no
// userId → stored with userId NULL, marking it a vendor address).
type vendorAddressDTO struct {
	ID          uuid.UUID `json:"id"`
	Street1     string    `json:"street1"`
	Street2     string    `json:"street2"`
	City        string    `json:"city"`
	PostalCode  string    `json:"postalCode"`
	Country     string    `json:"country"`
	CompanyName *string   `json:"companyName"`
}

// productVariantVersionDTO is the data payload of
// catalog/product-variant-version/created. Only id and weight are read; the
// real event carries many more fields (ignored).
type productVariantVersionDTO struct {
	ID     uuid.UUID `json:"id"`
	Weight float64   `json:"weight"`
}

// paymentEnabledDTO is the data payload of payment/payment/payment-enabled.
type paymentEnabledDTO struct {
	Order orderDTO `json:"order"`
}

// orderDTO is the subset of the payment-enabled order that is read (other
// fields are ignored on decode).
type orderDTO struct {
	ID                uuid.UUID      `json:"id"`
	OrderItems        []orderItemDTO `json:"orderItems"`
	ShipmentAddressID uuid.UUID      `json:"shipmentAddressId"`
}

// orderItemDTO is the subset of an order item that is read. Count (Long) is
// used as the quantity.
type orderItemDTO struct {
	ID                      uuid.UUID `json:"id"`
	ProductVariantVersionID uuid.UUID `json:"productVariantVersionId"`
	Count                   int64     `json:"count"`
	ShipmentMethodID        uuid.UUID `json:"shipmentMethodId"`
}

// returnDTO is the data payload of return/return/created.
type returnDTO struct {
	ID           uuid.UUID   `json:"id"`
	OrderID      uuid.UUID   `json:"orderId"`
	OrderItemIDs []uuid.UUID `json:"orderItemIds"`
}

// updateStatusInput is the body of POST /shipment/{id}/status.
type updateStatusInput struct {
	Status string `json:"status"`
}
