package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// returnColumns is the unqualified projection for a full Return row.
const returnColumns = "id, reason, refundedamount, createdat, orderid"

// returnColumnsQualified is the projection qualified with the returnentity
// table, required when the query joins orderentity (to disambiguate id).
const returnColumnsQualified = "returnentity.id, returnentity.reason, returnentity.refundedamount, returnentity.createdat, returnentity.orderid"

// GetReturn loads a return by id, erroring if it does not exist (the original
// returnRepository.findById(id).awaitSingle() throws NoSuchElementException on
// an empty result, surfacing as a GraphQL error).
func (s *Store) GetReturn(ctx context.Context, id uuid.UUID) (Return, error) {
	return getReturn(ctx, s.pool, id)
}

func getReturn(ctx context.Context, q querier, id uuid.UUID) (Return, error) {
	var r Return
	err := q.QueryRow(ctx,
		"SELECT "+returnColumns+" FROM returnentity WHERE id = $1", id,
	).Scan(&r.ID, &r.Reason, &r.RefundedAmount, &r.CreatedAt, &r.OrderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Return{}, fmt.Errorf("return with id %s not found", id)
	}
	if err != nil {
		return Return{}, fmt.Errorf("get return %s: %w", id, err)
	}
	return r, nil
}

// GetReturnsByIDs batch-loads returns for the given ids (used by the
// OrderItem.returnedWith data loader). The original loader indexed the result
// by id and asserted non-null (entities[id]!!); this method errors if any
// requested id has no row, reproducing that dangling-reference failure.
func (s *Store) GetReturnsByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Return, error) {
	out := make(map[uuid.UUID]Return, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		"SELECT "+returnColumns+" FROM returnentity WHERE id = ANY($1)", ids,
	)
	if err != nil {
		return nil, fmt.Errorf("load returns by ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r Return
		if err := rows.Scan(&r.ID, &r.Reason, &r.RefundedAmount, &r.CreatedAt, &r.OrderID); err != nil {
			return nil, fmt.Errorf("scan return: %w", err)
		}
		out[r.ID] = r
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListReturns returns a page of returns. It always joins orderentity (matching
// ReturnConnection's default applyJoin) so that both the optional userID
// predicate (User.returns filters by the target user) and the non-employee
// authorization filter (orderentity.userid = caller) can reference the joined
// user id. Conditions are AND-combined in the original's order: predicate,
// then authorizedUserFilter.
//
//   - userID != nil  → adds "orderentity.userid = $n" (the User.returns predicate)
//   - authUserID != nil → adds "orderentity.userid = $n" (non-employee filter)
//
// For employees/admins authUserID is nil (no restriction). For Query.returns
// userID is nil (no extra predicate).
func (s *Store) ListReturns(
	ctx context.Context, userID, authUserID *uuid.UUID,
	first, skip *int, ascending bool,
) (Connection[Return], error) {
	p := page{
		table:      "returnentity",
		columns:    returnColumnsQualified,
		join:       "JOIN orderentity ON returnentity.orderid = orderentity.id",
		primaryKey: "returnentity.id",
		orderCols:  []string{"returnentity.id"},
		ascending:  ascending,
		first:      first,
		skip:       skip,
	}
	if userID != nil {
		p.condArgs = append(p.condArgs, *userID)
		p.conditions = append(p.conditions, fmt.Sprintf("orderentity.userid = $%d", len(p.condArgs)))
	}
	if authUserID != nil {
		p.condArgs = append(p.condArgs, *authUserID)
		p.conditions = append(p.conditions, fmt.Sprintf("orderentity.userid = $%d", len(p.condArgs)))
	}

	total, err := p.totalCount(ctx, s.pool)
	if err != nil {
		return Connection[Return]{}, err
	}

	q, args := p.nodesSQL()
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return Connection[Return]{}, fmt.Errorf("list returns: %w", err)
	}
	nodes, err := scanReturns(rows)
	if err != nil {
		return Connection[Return]{}, err
	}

	next, err := p.hasNextPage(ctx, s.pool)
	if err != nil {
		return Connection[Return]{}, err
	}

	return Connection[Return]{Nodes: nodes, TotalCount: total, HasNextPage: next}, nil
}

// CreateReturn runs the full createReturn operation in a single transaction,
// exactly mirroring org.misarch.returns.service.ReturnService.createReturn and
// its validateReturn / validateItemsCanStillBeReturned checks (evaluation order
// and error strings preserved). On success it returns the saved return; the
// caller publishes return/return/created after the commit.
//
// Steps: load order items → validate → insert return (id via uuid_generate_v4
// default, RETURNING id) → set each item's returnedWithId (fires the two
// BEFORE-UPDATE triggers) → commit.
func (s *Store) CreateReturn(
	ctx context.Context, orderItemIds []uuid.UUID, reason string,
	caller uuid.UUID, now time.Time,
) (Return, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Return{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Load the locally-materialized order items (missing ids simply don't
	//    come back). Ordering is irrelevant to validation.
	orderItems, err := findOrderItemsByIDs(ctx, tx, orderItemIds)
	if err != nil {
		return Return{}, err
	}

	// 2. Validate. .first() on an empty loaded list throws NoSuchElementException
	//    in Kotlin BEFORE the explicit emptiness check — reproduce that path.
	if len(orderItems) == 0 {
		return Return{}, errors.New("List is empty.")
	}
	orderID := orderItems[0].OrderID

	order, err := getOrder(ctx, tx, orderID)
	if err != nil {
		return Return{}, err
	}
	if order.UserID != caller {
		return Return{}, errors.New("Only the original buyer can create returns")
	}
	if len(orderItemIds) == 0 {
		return Return{}, errors.New("At least one order item id must be provided")
	}
	if missing := missingIDs(orderItemIds, orderItems); len(missing) > 0 {
		return Return{}, fmt.Errorf("Order item ids %s do not exist", formatUUIDList(missing))
	}
	if returned := returnedItemIDs(orderItems); len(returned) > 0 {
		return Return{}, fmt.Errorf("Order item ids %s have already been returned", formatUUIDList(returned))
	}
	for _, oi := range orderItems {
		if oi.SentWithID == nil || oi.CompensatableAmount == nil || oi.ProductVariantVersionID == nil {
			return Return{}, errors.New("Missing data about order items, please try again later")
		}
	}

	// Load shipments for the items (deduplicated by id, matching toSet()).
	shipmentIDs := distinctPtrIDs(func(oi OrderItem) *uuid.UUID { return oi.SentWithID }, orderItems)
	shipments, err := findShipmentsByIDs(ctx, tx, shipmentIDs)
	if err != nil {
		return Return{}, err
	}
	for _, sh := range shipments {
		if sh.DeliveredAt == nil {
			return Return{}, errors.New("At least one order item was not delivered")
		}
	}
	for _, oi := range orderItems {
		if oi.OrderID != orderID {
			return Return{}, errors.New("All order items must belong to the same order")
		}
	}

	// validateItemsCanStillBeReturned.
	pvvIDs := distinctPtrIDs(func(oi OrderItem) *uuid.UUID { return oi.ProductVariantVersionID }, orderItems)
	pvvs, err := findProductVariantVersionsByIDs(ctx, tx, pvvIDs)
	if err != nil {
		return Return{}, err
	}
	pvvByID := make(map[uuid.UUID]ProductVariantVersion, len(pvvs))
	for _, p := range pvvs {
		pvvByID[p.ID] = p
	}
	shipmentByID := make(map[uuid.UUID]Shipment, len(shipments))
	for _, sh := range shipments {
		shipmentByID[sh.ID] = sh
	}
	for _, oi := range orderItems {
		pvv, ok := pvvByID[*oi.ProductVariantVersionID]
		if !ok {
			// BUG-1: pvv table is empty in production because
			// catalog/product-variant-version/created is never subscribed, so
			// this fires for every real order item.
			return Return{}, errors.New("Product variant version not found")
		}
		if pvv.CanBeReturnedForDays != nil {
			if !(*pvv.CanBeReturnedForDays > 0) {
				return Return{}, fmt.Errorf("Order item %s cannot be returned", oi.ID)
			}
			deliveredAt := shipmentByID[*oi.SentWithID].DeliveredAt
			passedDays := durationDays(*deliveredAt, now)
			if !(passedDays <= int64(*pvv.CanBeReturnedForDays)) {
				return Return{}, fmt.Errorf("Order item %s can no longer be returned", oi.ID)
			}
		}
	}

	// 3. Compute refundedAmount = sum of compensatableAmount (Long).
	var refundedAmount int64
	for _, oi := range orderItems {
		refundedAmount += *oi.CompensatableAmount
	}

	// 4. Insert the return, letting the DB assign the id via its
	//    uuid_generate_v4() default (INSERT without id, RETURNING id).
	var savedID uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO returnentity (reason, orderid, refundedamount, createdat)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		reason, orderID, refundedAmount, now,
	).Scan(&savedID); err != nil {
		return Return{}, fmt.Errorf("insert return: %w", err)
	}

	// 5. Link each order item to the return (fires check_return_order_match and
	//    check_return_within_period BEFORE-UPDATE triggers).
	for _, oi := range orderItems {
		if _, err := tx.Exec(ctx,
			"UPDATE orderitementity SET returnedwithid = $1 WHERE id = $2", savedID, oi.ID,
		); err != nil {
			return Return{}, fmt.Errorf("link order item %s: %w", oi.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Return{}, err
	}

	return Return{
		ID:             savedID,
		Reason:         reason,
		RefundedAmount: refundedAmount,
		CreatedAt:      now,
		OrderID:        orderID,
	}, nil
}

// scanReturns collects return rows from an open pgx.Rows.
func scanReturns(rows pgx.Rows) ([]Return, error) {
	defer rows.Close()
	var out []Return
	for rows.Next() {
		var r Return
		if err := rows.Scan(&r.ID, &r.Reason, &r.RefundedAmount, &r.CreatedAt, &r.OrderID); err != nil {
			return nil, fmt.Errorf("scan return: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
