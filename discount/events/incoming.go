package events

import "github.com/google/uuid"

// The incoming DTOs mirror the Kotlin @RequestBody types the event controller
// deserializes CloudEvent .data into. Only the fields declared here are read;
// any extra fields the producing service sends are ignored (and, for the echoed
// OrderDTO, DROPPED on the typed round-trip — see below).

// UserCreated is the data of user/user/created (extra fields ignored).
type UserCreated struct {
	ID uuid.UUID `json:"id"`
}

// CategoryCreated is the data of catalog/category/created.
type CategoryCreated struct {
	ID uuid.UUID `json:"id"`
}

// ProductCreated is the data of catalog/product/created.
type ProductCreated struct {
	ID          uuid.UUID   `json:"id"`
	CategoryIds []uuid.UUID `json:"categoryIds"`
}

// ProductVariantCreated is the data of catalog/product-variant/created.
type ProductVariantCreated struct {
	ID        uuid.UUID `json:"id"`
	ProductID uuid.UUID `json:"productId"`
}

// ReservationSucceeded is the data of
// inventory/product-item/reservation-succeeded.
type ReservationSucceeded struct {
	Order OrderDTO `json:"order"`
}

// OrderStatus mirrors the transient event enum (PENDING|PLACED|REJECTED). It is
// never stored; it exists only so the OrderDTO round-trip preserves the value.
type OrderStatus string

// RejectionReason mirrors the transient event enum
// (INVALID_ORDER_DATA|INVENTORY_RESERVATION_FAILED), nullable.
type RejectionReason string

// PaymentAuthorization mirrors the nested payment-authorization object.
type PaymentAuthorization struct {
	CVC int `json:"cvc"`
}

// OrderDTO mirrors org.misarch.discount.event.model.order.OrderDTO. It is
// deserialized from the reservation-succeeded event and RE-SERIALIZED on the
// validation-succeeded/failed events. This is a TYPED round-trip: fields the
// order service sent that are not declared here are dropped, and these field
// names/casing are what get echoed. createdAt/placedAt are typed as strings
// (echoed as received, never parsed).
type OrderDTO struct {
	ID                       uuid.UUID             `json:"id"`
	UserID                   uuid.UUID             `json:"userId"`
	CreatedAt                string                `json:"createdAt"`
	OrderStatus              OrderStatus           `json:"orderStatus"`
	PlacedAt                 string                `json:"placedAt"`
	RejectionReason          *RejectionReason      `json:"rejectionReason"`
	OrderItems               []OrderItemDTO        `json:"orderItems"`
	ShipmentAddressID        uuid.UUID             `json:"shipmentAddressId"`
	InvoiceAddressID         uuid.UUID             `json:"invoiceAddressId"`
	CompensatableOrderAmount int64                 `json:"compensatableOrderAmount"`
	PaymentInformationID     uuid.UUID             `json:"paymentInformationId"`
	PaymentAuthorization     *PaymentAuthorization `json:"paymentAuthorization"`
	VatNumber                *string               `json:"vatNumber"`
}

// OrderItemDTO mirrors org.misarch.discount.event.model.order.OrderItemDTO.
type OrderItemDTO struct {
	ID                      uuid.UUID   `json:"id"`
	CreatedAt               string      `json:"createdAt"`
	ProductVariantVersionID uuid.UUID   `json:"productVariantVersionId"`
	ProductVariantID        uuid.UUID   `json:"productVariantId"`
	TaxRateVersionID        uuid.UUID   `json:"taxRateVersionId"`
	ShoppingCartItemID      uuid.UUID   `json:"shoppingCartItemId"`
	Count                   int64       `json:"count"`
	CompensatableAmount     int64       `json:"compensatableAmount"`
	ShipmentMethodID        uuid.UUID   `json:"shipmentMethodId"`
	DiscountIds             []uuid.UUID `json:"discountIds"`
}
