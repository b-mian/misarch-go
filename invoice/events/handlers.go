package events

import (
	"context"

	"misarch/invoice/store"
)

// onDiscountValidationSucceeded handles discount/order/validation-succeeded.
// It builds an invoice from the order, inserts it, then publishes
// invoice/invoice/created. Mirrors on_discount_order_validation_succeeded_event:
// build → (DTO) → insert → publish, aborting (→ 500) on the first error. In the
// current bug-for-bug state this always fails at the invoice-address lookup (or
// vendor-address load) and never reaches insert/publish.
func (svc *Service) onDiscountValidationSucceeded(ctx context.Context, data DiscountValidationSucceededEventData) error {
	inv, err := BuildInvoice(ctx, svc.store, data.Order)
	if err != nil {
		return err
	}
	dto := InvoiceCreatedDTO{Order: data.Order, Invoice: ToInvoiceDTO(inv)}
	if err := svc.store.InsertInvoice(ctx, inv); err != nil {
		return err
	}
	return svc.pub.PublishInvoiceCreated(ctx, dto)
}

// onVendorAddressCreated handles address/vendor-address/created. Upserts the
// vendor address keyed by _id — but only _id is written (bug preserved).
func (svc *Service) onVendorAddressCreated(ctx context.Context, data VendorAddressEventData) error {
	return svc.store.UpsertVendorAddress(ctx, data.ID)
}

// onUserCreated handles user/user/created (registered at
// /on-user-creation-event; unreachable in production due to the subscribe/route
// mismatch). Inserts a user document with an empty addresses array.
func (svc *Service) onUserCreated(ctx context.Context, data UserEventData) error {
	return svc.store.InsertUser(ctx, store.User{
		ID:        data.ID,
		FirstName: data.FirstName,
		LastName:  data.LastName,
		Addresses: []store.UserAddress{},
	})
}

// onUserAddressCreated handles address/user-address/created. Pushes the address
// subdocument onto the user's `user_addresses` array (bug: reads use
// `addresses`). companyName null/absent becomes an empty string.
func (svc *Service) onUserAddressCreated(ctx context.Context, data UserAddressEventData) error {
	companyName := ""
	if data.CompanyName != nil {
		companyName = *data.CompanyName
	}
	addr := store.UserAddress{
		ID:          data.ID,
		Street1:     data.Street1,
		Street2:     data.Street2,
		City:        data.City,
		PostalCode:  data.PostalCode,
		Country:     data.Country,
		CompanyName: companyName,
		UserID:      data.UserID,
	}
	return svc.store.PushUserAddress(ctx, data.UserID, addr)
}

// onUserAddressArchived handles address/user-address/archived. Attempts the
// (invalid) $pull on user_addresses._id (bug preserved).
func (svc *Service) onUserAddressArchived(ctx context.Context, data UserAddressArchivedEventData) error {
	return svc.store.PullUserAddress(ctx, data.UserID, data.ID)
}
