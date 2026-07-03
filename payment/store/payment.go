package store

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// PaymentFilterArgs carries the resolved PaymentFilter used to build the Mongo
// query, mirroring the original PaymentFilter input.
type PaymentFilterArgs struct {
	Status               *string
	PaymentInformationID *string
	PaymentMethod        *string
	From                 *time.Time
	To                   *time.Time
}

// GetPayment loads a payment by id, returning a not-found error matching the
// original NestJS message when it does not exist.
func (s *Store) GetPayment(ctx context.Context, id string) (PaymentDoc, error) {
	var p PaymentDoc
	err := s.payments.FindOne(ctx, bson.M{"_id": id}).Decode(&p)
	if err == mongo.ErrNoDocuments {
		return PaymentDoc{}, fmt.Errorf(`Payment with id "%s" not found`, id)
	}
	if err != nil {
		return PaymentDoc{}, fmt.Errorf("get payment %s: %w", id, err)
	}
	return p, nil
}

// buildPaymentQuery reproduces PaymentService.buildQuery exactly:
//   - status → { status }
//   - paymentInformationId OR paymentMethod → { paymentInformation: { $in: allowedIds } }
//   - from → createdAt.$gte
//   - to → createdAt.$lte (merged with $gte if both present)
func (s *Store) buildPaymentQuery(ctx context.Context, f *PaymentFilterArgs) (bson.M, error) {
	query := bson.M{}
	if f == nil {
		return query, nil
	}
	if f.Status != nil {
		query["status"] = *f.Status
	}
	if f.PaymentInformationID != nil || f.PaymentMethod != nil {
		allowedIDs, err := s.buildAllowedFilterIDs(ctx, f.PaymentInformationID, f.PaymentMethod)
		if err != nil {
			return nil, err
		}
		query["paymentInformation"] = bson.M{"$in": allowedIDs}
	}
	if f.From != nil || f.To != nil {
		createdAt := bson.M{}
		if f.From != nil {
			createdAt["$gte"] = *f.From
		}
		if f.To != nil {
			createdAt["$lte"] = *f.To
		}
		query["createdAt"] = createdAt
	}
	return query, nil
}

// buildAllowedFilterIDs reproduces PaymentService.buildAllowedFilterIds:
//   - no paymentMethod → [paymentInformationId] (a single-element list; the
//     element is nil when only that branch's caller condition held, but the
//     function is only reached when at least one of the two is set).
//   - with paymentMethod → resolve all payment-information ids for that method;
//     if no paymentInformationId return them all; if the given id is not among
//     them return []; else return [paymentInformationId].
func (s *Store) buildAllowedFilterIDs(ctx context.Context, paymentInformationID, paymentMethod *string) ([]any, error) {
	if paymentMethod == nil {
		// [paymentInformationId] — element may be a typed nil pointer's value.
		if paymentInformationID == nil {
			return []any{nil}, nil
		}
		return []any{*paymentInformationID}, nil
	}

	infos, err := s.findPaymentInformations(ctx, &PaymentInformationFilterArgs{PaymentMethod: paymentMethod}, nil, nil, "_id", 1)
	if err != nil {
		return nil, err
	}
	ids := make([]any, 0, len(infos))
	idSet := make(map[string]struct{}, len(infos))
	for _, info := range infos {
		ids = append(ids, info.ID)
		idSet[info.ID] = struct{}{}
	}

	if paymentInformationID == nil {
		return ids, nil
	}
	if _, ok := idSet[*paymentInformationID]; !ok {
		return []any{}, nil
	}
	return []any{*paymentInformationID}, nil
}

// findPayments runs the paged find: filter, limit(first), skip, sort({_id: dir}).
// The graph layer populates each node's paymentInformation FK via a field
// resolver, so we do not join here (unlike the original populate()).
func (s *Store) findPayments(ctx context.Context, f *PaymentFilterArgs, first, skip *int, sortField string, dir int) ([]PaymentDoc, error) {
	query, err := s.buildPaymentQuery(ctx, f)
	if err != nil {
		return nil, err
	}
	opts := options.Find().SetSort(bson.D{{Key: sortField, Value: dir}})
	if first != nil {
		opts.SetLimit(int64(*first))
	}
	if skip != nil {
		opts.SetSkip(int64(*skip))
	}
	cur, err := s.payments.Find(ctx, query, opts)
	if err != nil {
		return nil, fmt.Errorf("find payments: %w", err)
	}
	var out []PaymentDoc
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("decode payments: %w", err)
	}
	return out, nil
}

// countPayments runs countDocuments with the same filter (no skip/limit).
func (s *Store) countPayments(ctx context.Context, f *PaymentFilterArgs) (int, error) {
	query, err := s.buildPaymentQuery(ctx, f)
	if err != nil {
		return 0, err
	}
	n, err := s.payments.CountDocuments(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("count payments: %w", err)
	}
	return int(n), nil
}

// ListPayments builds a PaymentConnection: nodes (paged) + totalCount +
// hasNextPage = skip + first < totalCount. Both counts always computed (a
// benign, safer divergence from the original selection-set optimization; the
// external nodes/totalCount/hasNextPage values are identical).
func (s *Store) ListPayments(ctx context.Context, f *PaymentFilterArgs, first, skip *int, sortField string, dir int) (Connection[PaymentDoc], error) {
	nodes, err := s.findPayments(ctx, f, first, skip, sortField, dir)
	if err != nil {
		return Connection[PaymentDoc]{}, err
	}
	total, err := s.countPayments(ctx, f)
	if err != nil {
		return Connection[PaymentDoc]{}, err
	}
	return Connection[PaymentDoc]{
		Nodes:       nodes,
		TotalCount:  total,
		HasNextPage: hasNextPage(first, skip, total),
	}, nil
}

// FindPaymentsByStatusMethod is used by the cron jobs: PENDING payments of a
// given method whose createdAt <= to. Mirrors PaymentService.find with a
// filter { status, paymentMethod, to } (no pagination → find all).
func (s *Store) FindPaymentsByStatusMethod(ctx context.Context, status, method string, to time.Time) ([]PaymentDoc, error) {
	return s.findPayments(ctx, &PaymentFilterArgs{
		Status:        &status,
		PaymentMethod: &method,
		To:            &to,
	}, nil, nil, "_id", 1)
}

// CreatePayment inserts a new payment with _id == order id (the correlation
// key), the given payment-information string ref, amount, default status OPEN,
// numberOfRetries 0, and createdAt/updatedAt timestamps (replicating Mongoose
// timestamps:true). Returns the inserted doc.
func (s *Store) CreatePayment(ctx context.Context, orderID, paymentInformationID string, totalAmount int64) (PaymentDoc, error) {
	now := time.Now()
	doc := PaymentDoc{
		ID:                 orderID,
		TotalAmount:        totalAmount,
		Status:             "OPEN",
		PaymentInformation: paymentInformationID,
		NumberOfRetries:    0,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if _, err := s.payments.InsertOne(ctx, doc); err != nil {
		return PaymentDoc{}, fmt.Errorf("create payment %s: %w", orderID, err)
	}
	return doc, nil
}

// UpdatePaymentStatus sets status (and payedAt=now when status is SUCCEEDED),
// updates updatedAt, and returns the post-update document. Not-found yields the
// original NestJS message. The original used findOneAndUpdate with new:true; we
// apply a partial $set so the amount/paymentInformation/createdAt are preserved
// (the documented external effect is "status changes, payedAt set on success").
func (s *Store) UpdatePaymentStatus(ctx context.Context, id, status string) (PaymentDoc, error) {
	now := time.Now()
	set := bson.M{"status": status, "updatedAt": now}
	if status == "SUCCEEDED" {
		set["payedAt"] = now
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var p PaymentDoc
	err := s.payments.FindOneAndUpdate(ctx, bson.M{"_id": id}, bson.M{"$set": set}, opts).Decode(&p)
	if err == mongo.ErrNoDocuments {
		return PaymentDoc{}, fmt.Errorf(`Payment with id "%s" not found`, id)
	}
	if err != nil {
		return PaymentDoc{}, fmt.Errorf("update payment status %s: %w", id, err)
	}
	return p, nil
}

// DeletePayment loads then hard-deletes a payment by id, returning the deleted
// document. Not-found yields the original NestJS message.
func (s *Store) DeletePayment(ctx context.Context, id string) (PaymentDoc, error) {
	// The original loads first (to produce the exact not-found error) then
	// findByIdAndDelete; findOneAndDelete returning the doc is equivalent, but
	// we mirror the not-found path explicitly.
	var p PaymentDoc
	err := s.payments.FindOneAndDelete(ctx, bson.M{"_id": id}).Decode(&p)
	if err == mongo.ErrNoDocuments {
		return PaymentDoc{}, fmt.Errorf(`Payment with id "%s" not found`, id)
	}
	if err != nil {
		return PaymentDoc{}, fmt.Errorf("delete payment %s: %w", id, err)
	}
	return p, nil
}

// hasNextPage reports skip + first < totalCount, matching the original. When
// first is unbounded (nil) the original always had first == MAX_INT32, so it is
// effectively never a next page; we treat nil as "no bound" → no next page.
func hasNextPage(first, skip *int, total int) bool {
	if first == nil {
		return false
	}
	s := 0
	if skip != nil {
		s = *skip
	}
	return s+*first < total
}
