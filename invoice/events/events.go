// Package events defines the Dapr pub/sub payloads and event-data shapes of the
// invoice service, plus helpers to publish the invoice-created event.
//
// The invoice service is event-driven: it has no GraphQL mutations. It
// subscribes to five topics (see the router in main.go) and publishes exactly
// one — invoice/invoice/created — after building an invoice from a
// discount/order/validation-succeeded event.
//
// Topic strings, route strings and JSON field names/casing are copied verbatim
// from the original Rust service, INCLUDING its bugs (the broken publish URL,
// the mismatched subscribe route), because they are part of the observable
// external behavior. See the spec's "defect ledger".
package events

import (
	"github.com/google/uuid"
)

// Subscribed / published topic strings, verbatim from the Rust service.
const (
	// TopicDiscountValidationSucceeded triggers invoice creation.
	TopicDiscountValidationSucceeded = "discount/order/validation-succeeded"
	// TopicVendorAddressCreated updates the local vendor-address read model.
	TopicVendorAddressCreated = "address/vendor-address/created"
	// TopicUserCreated updates the local user read model.
	TopicUserCreated = "user/user/created"
	// TopicUserAddressCreated appends to the local user's addresses.
	TopicUserAddressCreated = "address/user-address/created"
	// TopicUserAddressArchived removes a user address.
	TopicUserAddressArchived = "address/user-address/archived"

	// TopicInvoiceCreated is the (nominal) published topic. NOTE: the actual
	// publish URL used by the original addresses pubsub component "invoice"
	// with topic "invoice/created" (a bug); see the publisher.
	TopicInvoiceCreated = "invoice/invoice/created"
)

// Dapr delivery route paths registered by the service. The subscribe list
// advertises RouteUserCreatedSubscribed for user/user/created, but the handler
// is registered at RouteUserCreated — a mismatch the original ships (Dapr 404s
// user-created deliveries and drops them). Both strings are reproduced.
const (
	RouteDiscountValidationSucceeded = "/on-discount-validation-succeded" // sic: "succeded"
	RouteVendorAddressCreated        = "/on-vendor-address-creation-event"
	RouteUserCreated                 = "/on-user-creation-event" // where the handler is registered
	RouteUserCreatedSubscribed       = "/on-id-creation-event"   // what the subscribe list advertises (bug)
	RouteUserAddressCreated          = "/on-user-address-creation-event"
	RouteUserAddressArchived         = "/on-user-address-archived-event"
)

// Envelope is the CloudEvent subset every handler reads: the topic (asserted
// against the expected value) and the typed data payload.
type Envelope[T any] struct {
	Topic string `json:"topic"`
	Data  T      `json:"data"`
}

// VendorAddressEventData is the payload of address/vendor-address/created.
// snake_case (this DTO has NO camelCase rename in the original). All fields
// required.
type VendorAddressEventData struct {
	ID          uuid.UUID `json:"id"`
	Street1     string    `json:"street1"`
	Street2     string    `json:"street2"`
	City        string    `json:"city"`
	PostalCode  string    `json:"postal_code"`
	Country     string    `json:"country"`
	CompanyName string    `json:"company_name"`
}

// UserEventData is the payload of user/user/created. snake_case, all required.
type UserEventData struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
}

// UserAddressEventData is the payload of address/user-address/created.
// camelCase. companyName is optional (null/absent → stored as empty string).
type UserAddressEventData struct {
	ID          uuid.UUID `json:"id"`
	Street1     string    `json:"street1"`
	Street2     string    `json:"street2"`
	City        string    `json:"city"`
	PostalCode  string    `json:"postalCode"`
	Country     string    `json:"country"`
	CompanyName *string   `json:"companyName"`
	UserID      uuid.UUID `json:"userId"`
}

// UserAddressArchivedEventData is the payload of address/user-address/archived.
// camelCase; other event fields are ignored.
type UserAddressArchivedEventData struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"userId"`
}

// DiscountValidationSucceededEventData is the payload of
// discount/order/validation-succeeded: a single order object.
type DiscountValidationSucceededEventData struct {
	Order OrderEventData `json:"order"`
}

// OrderEventData mirrors the Rust OrderEventData (serde camelCase). It is both
// decoded from the incoming discount event and re-serialized verbatim into the
// published invoice-created event.
type OrderEventData struct {
	ID                       uuid.UUID             `json:"id"`
	UserID                   uuid.UUID             `json:"userId"`
	CreatedAt                Time                  `json:"createdAt"`
	OrderStatus              string                `json:"orderStatus"`
	PlacedAt                 Time                  `json:"placedAt"`
	RejectionReason          *string               `json:"rejectionReason"`
	OrderItems               []OrderItemEventData  `json:"orderItems"`
	ShipmentAddressID        uuid.UUID             `json:"shipmentAddressId"`
	InvoiceAddressID         uuid.UUID             `json:"invoiceAddressId"`
	CompensatableOrderAmount uint64                `json:"compensatableOrderAmount"`
	PaymentInformationID     uuid.UUID             `json:"paymentInformationId"`
	PaymentAuthorization     *PaymentAuthorization `json:"paymentAuthorization"`
	VatNumber                *string               `json:"vatNumber"`
}

// OrderItemEventData mirrors the Rust OrderItemEventData (serde camelCase).
type OrderItemEventData struct {
	ID                      uuid.UUID   `json:"id"`
	CreatedAt               Time        `json:"createdAt"`
	ProductVariantID        uuid.UUID   `json:"productVariantId"`
	ProductVariantVersionID uuid.UUID   `json:"productVariantVersionId"`
	TaxRateVersionID        uuid.UUID   `json:"taxRateVersionId"`
	ShoppingCartItemID      uuid.UUID   `json:"shoppingCartItemId"`
	Count                   uint64      `json:"count"`
	CompensatableAmount     uint64      `json:"compensatableAmount"`
	ShipmentMethodID        uuid.UUID   `json:"shipmentMethodId"`
	DiscountIDs             []uuid.UUID `json:"discountIds"`
}

// PaymentAuthorization is the externally-tagged Rust enum
// `PaymentAuthorizationEventData::CVC(u16)`, whose serde camelCase form is the
// single-key object {"cVC": <int>}. Modeled as a struct so it serializes to
// exactly that shape.
type PaymentAuthorization struct {
	CVC uint16 `json:"cVC"`
}

// InvoiceDTO is the invoice half of the published event (serde camelCase). Its
// issuedAt uses Go's default time JSON (RFC3339, 'Z') matching chrono's serde
// default — distinct from the GraphQL issuedAt scalar (+00:00).
type InvoiceDTO struct {
	OrderID  uuid.UUID `json:"orderId"`
	IssuedAt Time      `json:"issuedAt"`
	Content  string    `json:"content"`
}

// InvoiceCreatedDTO is the full payload published on invoice creation
// (serde camelCase): the verbatim order plus the new invoice.
type InvoiceCreatedDTO struct {
	Order   OrderEventData `json:"order"`
	Invoice InvoiceDTO     `json:"invoice"`
}
