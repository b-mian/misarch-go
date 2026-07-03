// Package events defines the Dapr pub/sub payloads the address service emits
// and thin helpers to publish them. Topic strings and JSON field names/casing
// are copied verbatim from the original Kotlin service (org.misarch.address.
// event.AddressEvents and the *DTO classes) and must not drift — downstream
// services (order, invoice, …) subscribe to these exact shapes.
package events

import (
	"context"

	"github.com/google/uuid"
)

// Topic constants mirror org.misarch.address.event.AddressEvents.
const (
	// TopicUserAddressCreated is published when a user address is created.
	TopicUserAddressCreated = "address/user-address/created"
	// TopicUserAddressArchived is published when a user address is archived.
	TopicUserAddressArchived = "address/user-address/archived"
	// TopicVendorAddressCreated is published when a vendor address is created.
	TopicVendorAddressCreated = "address/vendor-address/created"
)

// Publisher is the subset of *dapr.Client this package needs, so callers can
// inject the real client (or a fake in tests). *dapr.Client satisfies it.
type Publisher interface {
	Publish(ctx context.Context, topic string, payload any) error
}

// Name mirrors NameDTO: a value object with two non-null string fields. It is
// emitted only when both firstName and lastName are present (else the enclosing
// name field is null).
type Name struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

// UserAddressCreated is the payload of TopicUserAddressCreated. Mirrors
// UserAddressDTO (which extends AddressDTO): the full address plus the owning
// userId. Name is nil (JSON null) when the address has no name; CompanyName is
// nil (JSON null) when absent.
type UserAddressCreated struct {
	ID          uuid.UUID `json:"id"`
	Name        *Name     `json:"name"`
	Street1     string    `json:"street1"`
	Street2     string    `json:"street2"`
	City        string    `json:"city"`
	PostalCode  string    `json:"postalCode"`
	Country     string    `json:"country"`
	CompanyName *string   `json:"companyName"`
	UserID      uuid.UUID `json:"userId"`
}

// VendorAddressCreated is the payload of TopicVendorAddressCreated. Mirrors
// VendorAddressDTO — identical to UserAddressDTO but WITHOUT userId.
type VendorAddressCreated struct {
	ID          uuid.UUID `json:"id"`
	Name        *Name     `json:"name"`
	Street1     string    `json:"street1"`
	Street2     string    `json:"street2"`
	City        string    `json:"city"`
	PostalCode  string    `json:"postalCode"`
	Country     string    `json:"country"`
	CompanyName *string   `json:"companyName"`
}

// UserAddressArchived is the payload of TopicUserAddressArchived. Mirrors
// ArchiveUserAddressDTO: just the address id and its owning user id.
type UserAddressArchived struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"userId"`
}

// PublishUserAddressCreated publishes a UserAddressCreated event.
func PublishUserAddressCreated(ctx context.Context, p Publisher, e UserAddressCreated) error {
	return p.Publish(ctx, TopicUserAddressCreated, e)
}

// PublishVendorAddressCreated publishes a VendorAddressCreated event.
func PublishVendorAddressCreated(ctx context.Context, p Publisher, e VendorAddressCreated) error {
	return p.Publish(ctx, TopicVendorAddressCreated, e)
}

// PublishUserAddressArchived publishes a UserAddressArchived event.
func PublishUserAddressArchived(ctx context.Context, p Publisher, e UserAddressArchived) error {
	return p.Publish(ctx, TopicUserAddressArchived, e)
}
