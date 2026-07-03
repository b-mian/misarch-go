package graph

import (
	"misarch/address/events"
	"misarch/address/store"
)

// This file maps store rows to GraphQL models and GraphQL order/filter inputs
// to store parameters. A single addressentity row becomes either a UserAddress
// or a VendorAddress depending on the userid discriminator, mirroring
// AddressEntity.toDTO(). Relationship fields (UserAddress.user) are left to
// their own field resolver; the UserID extraField carries the FK it needs.

// toName folds the row's firstName/lastName pair into a Name value object. Both
// columns are constrained (by a DB trigger and the NameInput type) to be either
// both null or both non-null; the model's name is non-null only in the latter
// case, exactly as AddressEntity.toDTO() computed it.
func toName(first, last *string) *Name {
	if first == nil || last == nil {
		return nil
	}
	return &Name{FirstName: *first, LastName: *last}
}

// toAddress maps a store row to the concrete Address implementation the row
// represents: UserAddress when userid is set, VendorAddress otherwise. The
// interface return matches Query.address's Address! result.
func toAddress(a store.Address) Address {
	if a.IsVendor() {
		return toVendorAddress(a)
	}
	return toUserAddress(a)
}

// toUserAddress maps a user-address row to the GraphQL model. IsArchived is the
// computed archivedAt != null; UserID is the extraField the user resolver uses.
// Callers only invoke this for confirmed user rows (userid non-null), so the
// dereference is safe.
func toUserAddress(a store.Address) *UserAddress {
	return &UserAddress{
		ID:          a.ID,
		Name:        toName(a.FirstName, a.LastName),
		Street1:     a.Street1,
		Street2:     a.Street2,
		City:        a.City,
		PostalCode:  a.PostalCode,
		Country:     a.Country,
		CompanyName: a.CompanyName,
		ArchivedAt:  a.ArchivedAt,
		IsArchived:  a.ArchivedAt != nil,
		UserID:      *a.UserID,
	}
}

// toVendorAddress maps a vendor-address row to the GraphQL model.
func toVendorAddress(a store.Address) *VendorAddress {
	return &VendorAddress{
		ID:          a.ID,
		Name:        toName(a.FirstName, a.LastName),
		Street1:     a.Street1,
		Street2:     a.Street2,
		City:        a.City,
		PostalCode:  a.PostalCode,
		Country:     a.Country,
		CompanyName: a.CompanyName,
	}
}

// toUserAddressConnection maps a paginated store result to the GraphQL
// connection.
func toUserAddressConnection(c store.Connection[store.Address]) *UserAddressConnection {
	nodes := make([]UserAddress, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toUserAddress(n)
	}
	return &UserAddressConnection{
		Nodes:       nodes,
		TotalCount:  c.TotalCount,
		HasNextPage: c.HasNextPage,
	}
}

// ascending reports whether an OrderDirection means ascending. The default
// (nil) is ASC, matching UserAddressOrder.DEFAULT's direction.
func ascending(d *OrderDirection) bool {
	return d == nil || *d != OrderDirectionDesc
}

// addressOrder resolves a UserAddressOrderInput to a store order column set and
// direction. The only order field is ID; defaults are field ID, direction ASC
// (applied when orderBy itself, its direction, or its field is null), exactly
// as UserAddressOrder.DEFAULT.
func addressOrder(in *UserAddressOrderInput) (store.AddressOrderColumn, bool) {
	if in == nil {
		return store.AddressOrderByID, true
	}
	// UserAddressOrderField has the single value ID, so the field selection has
	// no effect on the column; only the direction matters.
	return store.AddressOrderByID, ascending(in.Direction)
}

// archivedFilter resolves a UserAddressFilterInput to the store's tri-state
// isArchived filter: unset/nil => no filter, true => archived, false => active,
// mirroring UserAddressFilter.toExpression().
func archivedFilter(in *UserAddressFilterInput) store.ArchivedFilter {
	if in == nil || in.IsArchived == nil {
		return store.ArchivedFilter{}
	}
	return store.ArchivedFilter{Set: true, Value: *in.IsArchived}
}

// eventName maps a store name pair to the event Name payload (nil when absent),
// matching AddressEntity.toEventDTO()'s NameDTO construction.
func eventName(first, last *string) *events.Name {
	if first == nil || last == nil {
		return nil
	}
	return &events.Name{FirstName: *first, LastName: *last}
}
