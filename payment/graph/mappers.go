package graph

import (
	"fmt"

	"github.com/google/uuid"

	"misarch/payment/store"
)

// validatePagination reproduces the class-validator @Min constraints on the
// shared pagination args: skip @Min(0), first @Min(1). Below the bound → a
// validation error. Kept here (not in a resolver file) so gqlgen codegen does
// not relocate it.
func validatePagination(skip, first *int) error {
	if skip != nil && *skip < 0 {
		return fmt.Errorf("skip must not be less than 0")
	}
	if first != nil && *first < 1 {
		return fmt.Errorf("first must not be less than 1")
	}
	return nil
}

// This file maps store documents to GraphQL models and GraphQL order/filter
// inputs to store arguments. Relationship fields (PaymentInformation on
// Payment; User and Payments on PaymentInformation; PaymentInformations on
// User) are intentionally left nil here: they are populated lazily by their own
// field resolvers. The extraFields (Payment.PaymentInformationID,
// PaymentInformation.UserID) carry the FKs those resolvers need.

// toPayment maps a store.PaymentDoc to the GraphQL model. totalAmount is an int
// (cents) in Mongo but the SDL type is Float!, so it is widened to float64.
func toPayment(d store.PaymentDoc) *Payment {
	p := &Payment{
		ID:                   parseUUID(d.ID),
		TotalAmount:          float64(d.TotalAmount),
		Status:               PaymentStatus(d.Status),
		NumberOfRetries:      float64(d.NumberOfRetries),
		PaymentInformationID: d.PaymentInformation,
	}
	if d.PayedAt != nil {
		t := d.PayedAt.UTC()
		p.PayedAt = &t
	}
	return p
}

// toPaymentInformation maps a store.PaymentInformationDoc to the GraphQL model.
// secretMethodDetails is never copied — it is @HideField() in the original and
// must never be exposed via GraphQL.
func toPaymentInformation(d store.PaymentInformationDoc) *PaymentInformation {
	return &PaymentInformation{
		ID:                  parseUUID(d.ID),
		PaymentMethod:       PaymentMethod(d.PaymentMethod),
		PublicMethodDetails: d.PublicMethodDetails,
		UserID:              parseUUID(d.User.ID),
	}
}

// toPaymentConnection maps a paginated store result to the GraphQL connection.
func toPaymentConnection(c store.Connection[store.PaymentDoc]) *PaymentConnection {
	nodes := make([]Payment, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toPayment(n)
	}
	return &PaymentConnection{
		Nodes:       nodes,
		TotalCount:  c.TotalCount,
		HasNextPage: c.HasNextPage,
	}
}

// toPaymentInformationConnection maps a paginated store result to the GraphQL
// connection.
func toPaymentInformationConnection(c store.Connection[store.PaymentInformationDoc]) *PaymentInformationConnection {
	nodes := make([]PaymentInformation, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toPaymentInformation(n)
	}
	return &PaymentInformationConnection{
		Nodes:       nodes,
		TotalCount:  c.TotalCount,
		HasNextPage: c.HasNextPage,
	}
}

// parseUUID parses a string UUID, returning uuid.Nil on failure. Mongo _id
// values are app-generated UUIDs, so this should not fail; a Nil fallback keeps
// resolution from panicking on unexpected data.
func parseUUID(s string) uuid.UUID {
	u, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return u
}

// sortDirection maps an OrderDirection to the Mongo sort int: ASC → 1,
// DESC → -1. The default (nil) is ASC, matching the original OrderDirection
// default of ascending.
func sortDirection(d *OrderDirection) int {
	if d != nil && *d == OrderDirectionDesc {
		return -1
	}
	return 1
}

// paymentInformationSortField resolves a PaymentInformationOrder to the Mongo
// sort field and direction. The only order field is ID → `_id`; default is
// { field: ID, direction: ASC }.
func paymentInformationSortField(o *PaymentInformationOrder) (string, int) {
	if o == nil {
		return "_id", 1
	}
	return "_id", sortDirection(o.Direction)
}

// paymentSortField resolves a PaymentOrder to the Mongo sort field and
// direction. The only order field is ID → `_id`; default is
// { field: ID, direction: ASC }.
func paymentSortField(o *PaymentOrder) (string, int) {
	if o == nil {
		return "_id", 1
	}
	return "_id", sortDirection(o.Direction)
}

// paymentFilterArgs converts a GraphQL PaymentFilter to store filter args.
func paymentFilterArgs(f *PaymentFilter) *store.PaymentFilterArgs {
	if f == nil {
		return nil
	}
	args := &store.PaymentFilterArgs{
		From: f.From,
		To:   f.To,
	}
	if f.Status != nil {
		s := string(*f.Status)
		args.Status = &s
	}
	args.PaymentInformationID = f.PaymentInformationID
	if f.PaymentMethod != nil {
		m := string(*f.PaymentMethod)
		args.PaymentMethod = &m
	}
	return args
}
