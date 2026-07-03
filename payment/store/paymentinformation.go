package store

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// PaymentInformationFilterArgs carries the resolved PaymentInformationFilter.
//
// The original passes the raw filter object straight to Mongo find/count, and
// the two call sites shape `user` differently — this is the source of the two
// user-filter quirks (spec §9 #3/#4). We preserve both shapes:
//
//   - UserScalar: the top-level query path sets { user: "<uuid>" } (a scalar).
//     Documents store user as a subdocument { id: "<uuid>" }, so scalar
//     equality NEVER matches — top-level filtering by user returns 0 rows.
//     [BUG #4 — reproduced for parity.]
//   - UserSubID: the User.paymentInformations resolver sets
//     { user: { id: "<uuid>" } } (nested object). Mongo deep-equals this against
//     the stored subdocument, so it correctly matches the owner's rows.
type PaymentInformationFilterArgs struct {
	UserScalar    *string // top-level query: { user: <uuid> } (scalar, never matches)
	UserSubID     *string // User resolver: { user: { id: <uuid> } } (nested, matches)
	PaymentMethod *string
}

// filterDoc renders the args to the exact Mongo filter document the original
// would have produced, preserving both user-filter shapes.
func (f *PaymentInformationFilterArgs) filterDoc() bson.M {
	q := bson.M{}
	if f == nil {
		return q
	}
	if f.UserScalar != nil {
		q["user"] = *f.UserScalar
	} else if f.UserSubID != nil {
		q["user"] = bson.M{"id": *f.UserSubID}
	}
	if f.PaymentMethod != nil {
		q["paymentMethod"] = *f.PaymentMethod
	}
	return q
}

// GetPaymentInformation loads a payment information by id, returning the
// original NestJS not-found message when it does not exist.
func (s *Store) GetPaymentInformation(ctx context.Context, id string) (PaymentInformationDoc, error) {
	var pi PaymentInformationDoc
	err := s.paymentInformation.FindOne(ctx, bson.M{"_id": id}).Decode(&pi)
	if err == mongo.ErrNoDocuments {
		return PaymentInformationDoc{}, fmt.Errorf(`Payment Information with id "%s" not found`, id)
	}
	if err != nil {
		return PaymentInformationDoc{}, fmt.Errorf("get payment information %s: %w", id, err)
	}
	return pi, nil
}

// findPaymentInformations runs the paged find: filter, limit(first), skip,
// sort({_id: dir}).
func (s *Store) findPaymentInformations(ctx context.Context, f *PaymentInformationFilterArgs, first, skip *int, sortField string, dir int) ([]PaymentInformationDoc, error) {
	opts := options.Find().SetSort(bson.D{{Key: sortField, Value: dir}})
	if first != nil {
		opts.SetLimit(int64(*first))
	}
	if skip != nil {
		opts.SetSkip(int64(*skip))
	}
	cur, err := s.paymentInformation.Find(ctx, f.filterDoc(), opts)
	if err != nil {
		return nil, fmt.Errorf("find payment informations: %w", err)
	}
	var out []PaymentInformationDoc
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("decode payment informations: %w", err)
	}
	return out, nil
}

// countPaymentInformations runs countDocuments with the same filter.
func (s *Store) countPaymentInformations(ctx context.Context, f *PaymentInformationFilterArgs) (int, error) {
	n, err := s.paymentInformation.CountDocuments(ctx, f.filterDoc())
	if err != nil {
		return 0, fmt.Errorf("count payment informations: %w", err)
	}
	return int(n), nil
}

// ListPaymentInformations builds a PaymentInformationConnection: nodes (paged) +
// totalCount + hasNextPage. Both counts always computed (benign divergence from
// the original selection-set optimization).
func (s *Store) ListPaymentInformations(ctx context.Context, f *PaymentInformationFilterArgs, first, skip *int, sortField string, dir int) (Connection[PaymentInformationDoc], error) {
	nodes, err := s.findPaymentInformations(ctx, f, first, skip, sortField, dir)
	if err != nil {
		return Connection[PaymentInformationDoc]{}, err
	}
	total, err := s.countPaymentInformations(ctx, f)
	if err != nil {
		return Connection[PaymentInformationDoc]{}, err
	}
	return Connection[PaymentInformationDoc]{
		Nodes:       nodes,
		TotalCount:  total,
		HasNextPage: hasNextPage(first, skip, total),
	}, nil
}

// CreatePaymentInformation inserts a new payment information document with a
// freshly generated string UUID _id (the caller supplies the UUID so tests can
// be deterministic if needed). Returns the inserted document.
func (s *Store) CreatePaymentInformation(ctx context.Context, doc PaymentInformationDoc) (PaymentInformationDoc, error) {
	if _, err := s.paymentInformation.InsertOne(ctx, doc); err != nil {
		return PaymentInformationDoc{}, fmt.Errorf("create payment information %s: %w", doc.ID, err)
	}
	return doc, nil
}

// DeletePaymentInformation hard-deletes a payment information by id and returns
// the deleted document. Not-found yields the original NestJS message. (The
// authorization/existence checks happen in the resolver, mirroring the original
// service.delete method flow.)
func (s *Store) DeletePaymentInformation(ctx context.Context, id string) (PaymentInformationDoc, error) {
	var pi PaymentInformationDoc
	err := s.paymentInformation.FindOneAndDelete(ctx, bson.M{"_id": id}).Decode(&pi)
	if err == mongo.ErrNoDocuments {
		return PaymentInformationDoc{}, fmt.Errorf(`Payment Information with id "%s" not found`, id)
	}
	if err != nil {
		return PaymentInformationDoc{}, fmt.Errorf("delete payment information %s: %w", id, err)
	}
	return pi, nil
}
