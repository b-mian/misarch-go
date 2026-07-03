package graph

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"misarch/order/store"
)

// uuidLess reports whether a sorts before b by raw 16-byte order, matching how
// Rust's bson::Uuid (a [u8; 16] newtype) compares in BTreeSet/Ord contexts.
func uuidLess(a, b uuid.UUID) bool {
	return bytes.Compare(a[:], b[:]) < 0
}

// GraphQL query bodies, copied verbatim from the original service's
// queries/*.graphql files.
const (
	queryShoppingCart = `query GetShoppingCartProductVariantIdsAndCounts($representations: [_Any!]!) {
    _entities(representations: $representations) {
        __typename
        ... on User {
            shoppingcart {
                shoppingcartItems {
                    nodes {
                        id,
                        productVariant {
                            id
                        },
                        count
                    }
                }
            }
        }
    }
}`

	queryInventory = `query GetUnreservedProductItemCounts($representations: [_Any!]!) {
    _entities(representations: $representations) {
        __typename
        ... on ProductVariant {
            id,
            inventoryCount,
        }
    }
}`

	queryDiscounts = `query GetDiscounts($findApplicableDiscountsInput: FindApplicableDiscountsInput!) {
    findApplicableDiscounts(input: $findApplicableDiscountsInput) {
        productVariantId,
        discounts {
            id,
            discount,
        }
    }
}`

	queryShipmentFees = `query GetShipmentFees($calculateShipmentFeesInput: CalculateShipmentFeesInput!) {
    calculateShipmentFees(input: $calculateShipmentFeesInput)
}`
)

// orderItemInput is the per-order-item input carried through the saga (the
// GraphQL OrderItemInput, deduped by shoppingCartItemId).
type orderItemInput struct {
	shoppingCartItemID uuid.UUID
	shipmentMethodID   uuid.UUID
	couponIDs          []uuid.UUID
}

// createInternalOrderItems is the createOrder fan-out saga. It mirrors the Rust
// create_internal_order_items → query_or_obtain_order_item_attributes →
// zip_to_internal_order_items pipeline exactly, building the embedded order-item
// snapshots (with computed compensatable amounts) for a new order.
//
// authorizedUser is the Authorized-User header JSON forwarded ONLY to the
// shoppingcart call. createdAt is the single timestamp captured at the start of
// createOrder, reused for the order and all its items.
func (r *Resolver) createInternalOrderItems(ctx context.Context, userID uuid.UUID, inputs []orderItemInput, authorizedUser string, createdAt time.Time) ([]store.OrderItem, error) {
	// 1. Resolve cart items → (productVariantId, count) and filter to the inputs.
	countsByPV, inputsByPV, err := r.queryCountsByProductVariantIDs(ctx, userID, inputs, authorizedUser)
	if err != nil {
		return nil, err
	}

	// product_variant_ids = keys(countsByPV) — all requested variants, BEFORE
	// visibility filtering. Used by the inventory and discount calls.
	productVariantIDs := make([]uuid.UUID, 0, len(countsByPV))
	for id := range countsByPV {
		productVariantIDs = append(productVariantIDs, id)
	}

	// 2. Local product variants, dropping non-publicly-visible ones.
	productVariantsByPV, err := r.queryProductVariantsByProductVariantIDs(ctx, productVariantIDs)
	if err != nil {
		return nil, err
	}

	// 3. Current product-variant versions (derived from surviving variants).
	pvvByPV := make(map[uuid.UUID]store.ProductVariantVersion, len(productVariantsByPV))
	for id, pv := range productVariantsByPV {
		pvvByPV[id] = pv.CurrentVersion
	}

	// 4. Availability check (inventory) over ALL requested variants.
	if err := r.checkProductVariantAvailability(ctx, productVariantIDs, countsByPV); err != nil {
		return nil, err
	}

	// 5. Tax rate versions (local) for the surviving variants' tax rates.
	taxRateVersionByPV, err := r.queryTaxRateVersionsByProductVariantIDs(ctx, pvvByPV)
	if err != nil {
		return nil, err
	}

	// 6. Applicable discounts (discount service); every requested variant must
	// appear in the response.
	discountsByPV, err := r.queryDiscountsByProductVariantIDs(ctx, userID, inputsByPV, productVariantIDs, pvvByPV, countsByPV)
	if err != nil {
		return nil, err
	}

	// 7. Shipment fees (shipment service) — result discarded, errors propagate.
	if err := r.queryShipmentFees(ctx, inputsByPV, pvvByPV, countsByPV); err != nil {
		return nil, err
	}

	// 8. Zip the surviving variants into order items.
	return zipToInternalOrderItems(productVariantsByPV, inputsByPV, pvvByPV, taxRateVersionByPV, countsByPV, discountsByPV, createdAt)
}

// ---- shoppingcart -------------------------------------------------------

type shoppingCartEntity struct {
	Typename     string `json:"__typename"`
	Shoppingcart *struct {
		ShoppingcartItems struct {
			Nodes []struct {
				ID             uuid.UUID `json:"id"`
				ProductVariant struct {
					ID uuid.UUID `json:"id"`
				} `json:"productVariant"`
				Count int64 `json:"count"`
			} `json:"nodes"`
		} `json:"shoppingcartItems"`
	} `json:"shoppingcart,omitempty"`
}

type shoppingCartData struct {
	Entities []shoppingCartEntity `json:"_entities"`
}

// queryCountsByProductVariantIDs invokes shoppingcart to resolve the user's cart
// items, then filters to the requested order-item inputs, producing
// {pvId → count} and {pvId → input}. Mirrors query_counts_by_product_variant_ids
// + build_counts_by_product_variant_ids + build_order_item_inputs_by_product_variant_ids.
func (r *Resolver) queryCountsByProductVariantIDs(ctx context.Context, userID uuid.UUID, inputs []orderItemInput, authorizedUser string) (map[uuid.UUID]uint64, map[uuid.UUID]orderItemInput, error) {
	vars := map[string]any{
		"representations": []any{
			map[string]any{"__typename": "User", "id": userID.String()},
		},
	}
	var data shoppingCartData
	emptyErr := fmt.Errorf("Response data of `query_counts_by_product_variant_ids` query is empty.")
	if err := r.gql.query(ctx, "shoppingcart", queryShoppingCart, vars, authorizedUser, &data, emptyErr); err != nil {
		return nil, nil, err
	}
	if len(data.Entities) == 0 {
		return nil, nil, fmt.Errorf("Response data of `query_counts_by_product_variant_ids` query is empty.")
	}
	entity := data.Entities[0]
	if entity.Shoppingcart == nil {
		return nil, nil, fmt.Errorf("`ids_and_counts_enum` does not contain a User entity")
	}

	// cartItemID → (productVariantID, count)
	type pvCount struct {
		pv    uuid.UUID
		count uint64
	}
	idsAndCounts := make(map[uuid.UUID]pvCount)
	for _, n := range entity.Shoppingcart.ShoppingcartItems.Nodes {
		idsAndCounts[n.ID] = pvCount{pv: n.ProductVariant.ID, count: uint64(n.Count)}
	}

	countsByPV := make(map[uuid.UUID]uint64)
	inputsByPV := make(map[uuid.UUID]orderItemInput)
	for _, in := range inputs {
		pc, ok := idsAndCounts[in.shoppingCartItemID]
		if !ok {
			return nil, nil, buildHashMapError(in.shoppingCartItemID)
		}
		countsByPV[pc.pv] = pc.count
		inputsByPV[pc.pv] = in
	}
	return countsByPV, inputsByPV, nil
}

// ---- local product variants --------------------------------------------

// queryProductVariantsByProductVariantIDs loads the local product variants and
// drops non-publicly-visible ones. Mirrors
// query_product_variants_by_product_variant_ids.
func (r *Resolver) queryProductVariantsByProductVariantIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]store.ProductVariant, error) {
	all, err := r.Store.GetProductVariants(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("%s with UUIDs: `%v` not found.", "ProductVariant", ids)
	}
	visible := make(map[uuid.UUID]store.ProductVariant, len(all))
	for id, pv := range all {
		if pv.IsVisible() {
			visible[id] = pv
		}
	}
	return visible, nil
}

// ---- inventory ----------------------------------------------------------

type inventoryEntity struct {
	Typename       string     `json:"__typename"`
	ID             *uuid.UUID `json:"id"`
	InventoryCount *int64     `json:"inventoryCount"`
}

type inventoryData struct {
	Entities []inventoryEntity `json:"_entities"`
}

// checkProductVariantAvailability invokes inventory for the given product
// variant ids and requires stock >= expected for every entry in countsByPV.
// Mirrors check_product_variant_availability + calculate_availability_of_product_variant_ids.
func (r *Resolver) checkProductVariantAvailability(ctx context.Context, productVariantIDs []uuid.UUID, countsByPV map[uuid.UUID]uint64) error {
	reps := make([]any, 0, len(productVariantIDs))
	for _, id := range productVariantIDs {
		reps = append(reps, map[string]any{"__typename": "ProductVariant", "id": id.String()})
	}
	vars := map[string]any{"representations": reps}
	var data inventoryData
	emptyErr := fmt.Errorf("Response data of `check_product_variant_availability` query is empty.")
	if err := r.gql.query(ctx, "inventory", queryInventory, vars, "", &data, emptyErr); err != nil {
		return err
	}

	stock := make(map[uuid.UUID]uint64)
	for _, e := range data.Entities {
		if e.ID == nil || e.InventoryCount == nil {
			return fmt.Errorf("Response data of `check_product_variant_availability` query could not be parsed, `maybe_product_variant_enum` is `None`")
		}
		stock[*e.ID] = uint64(*e.InventoryCount)
	}

	for id, expected := range countsByPV {
		count, ok := stock[id]
		if !ok {
			return buildHashMapError(id)
		}
		if count < expected {
			return fmt.Errorf("Not all requested product variants are available.")
		}
	}
	return nil
}

// ---- tax rates ----------------------------------------------------------

// queryTaxRateVersionsByProductVariantIDs loads the local tax rates for the
// surviving variants' versions and maps {pvId → taxRate.current_version}. Every
// needed tax rate must exist. Mirrors query_tax_rate_versions_by_product_variant_ids.
func (r *Resolver) queryTaxRateVersionsByProductVariantIDs(ctx context.Context, pvvByPV map[uuid.UUID]store.ProductVariantVersion) (map[uuid.UUID]store.TaxRateVersion, error) {
	taxRateIDs := make([]uuid.UUID, 0, len(pvvByPV))
	for _, pvv := range pvvByPV {
		taxRateIDs = append(taxRateIDs, pvv.TaxRateID)
	}
	taxRates, err := r.Store.GetTaxRates(ctx, taxRateIDs)
	if err != nil {
		return nil, fmt.Errorf("%s with UUIDs: `%v` not found.", "TaxRate", taxRateIDs)
	}
	out := make(map[uuid.UUID]store.TaxRateVersion, len(pvvByPV))
	for id, pvv := range pvvByPV {
		tr, ok := taxRates[pvv.TaxRateID]
		if !ok {
			return nil, buildHashMapError(id)
		}
		out[id] = tr.CurrentVersion
	}
	return out, nil
}

// ---- discounts ----------------------------------------------------------

type discountData struct {
	FindApplicableDiscounts []struct {
		ProductVariantID uuid.UUID `json:"productVariantId"`
		Discounts        []struct {
			ID       uuid.UUID `json:"id"`
			Discount float64   `json:"discount"`
		} `json:"discounts"`
	} `json:"findApplicableDiscounts"`
}

// queryDiscountsByProductVariantIDs invokes discount for the requested variants
// and remaps the response to {pvId → sorted discounts}. Every requested variant
// must appear in the response. Mirrors query_discounts_by_product_variant_ids.
func (r *Resolver) queryDiscountsByProductVariantIDs(ctx context.Context, userID uuid.UUID, inputsByPV map[uuid.UUID]orderItemInput, productVariantIDs []uuid.UUID, pvvByPV map[uuid.UUID]store.ProductVariantVersion, countsByPV map[uuid.UUID]uint64) (map[uuid.UUID][]store.Discount, error) {
	// Build the productVariants input over ALL requested variants.
	pvInputs := make([]any, 0, len(productVariantIDs))
	for _, id := range productVariantIDs {
		count, ok := countsByPV[id]
		if !ok {
			return nil, buildHashMapError(id)
		}
		in, ok := inputsByPV[id]
		if !ok {
			return nil, buildHashMapError(id)
		}
		coupons := make([]string, len(in.couponIDs))
		for i, c := range in.couponIDs {
			coupons[i] = c.String()
		}
		pvInputs = append(pvInputs, map[string]any{
			"productVariantId": id.String(),
			"count":            count,
			"couponIds":        coupons,
		})
	}
	// orderAmount = sum of surviving product-variant-version prices.
	var orderAmount uint64
	for _, pvv := range pvvByPV {
		orderAmount += uint64(pvv.Price)
	}
	vars := map[string]any{
		"findApplicableDiscountsInput": map[string]any{
			"userId":          userID.String(),
			"productVariants": pvInputs,
			"orderAmount":     orderAmount,
		},
	}
	var data discountData
	emptyErr := fmt.Errorf("Response data of `query_discounts` query is empty.")
	if err := r.gql.query(ctx, "discount", queryDiscounts, vars, "", &data, emptyErr); err != nil {
		return nil, err
	}

	byPV := make(map[uuid.UUID][]store.Discount)
	for _, d := range data.FindApplicableDiscounts {
		ds := make([]store.Discount, len(d.Discounts))
		for i, dd := range d.Discounts {
			ds[i] = store.Discount{ID: dd.ID, Discount: dd.Discount}
		}
		byPV[d.ProductVariantID] = ds
	}

	// Every requested variant must be present in the response.
	out := make(map[uuid.UUID][]store.Discount, len(productVariantIDs))
	for _, id := range productVariantIDs {
		ds, ok := byPV[id]
		if !ok {
			return nil, fmt.Errorf("Product variant of UUID: `%s` is not contained in the result which `findApplicableDiscounts` provides.", id)
		}
		// internal_discounts is a BTreeSet by _id → sort ascending and dedupe.
		out[id] = sortDedupeDiscounts(ds)
	}
	return out, nil
}

// ---- shipment -----------------------------------------------------------

type shipmentData struct {
	CalculateShipmentFees int64 `json:"calculateShipmentFees"`
}

// queryShipmentFees invokes shipment to compute fees. The numeric result is
// DISCARDED, but a service error / non-parseable response aborts order creation.
// Mirrors query_shipment_fees (the returned value is bound to _shipment_fees).
func (r *Resolver) queryShipmentFees(ctx context.Context, inputsByPV map[uuid.UUID]orderItemInput, pvvByPV map[uuid.UUID]store.ProductVariantVersion, countsByPV map[uuid.UUID]uint64) error {
	items := make([]any, 0, len(pvvByPV))
	for id, pvv := range pvvByPV {
		count, ok := countsByPV[id]
		if !ok {
			return buildHashMapError(id)
		}
		in, ok := inputsByPV[id]
		if !ok {
			return buildHashMapError(id)
		}
		items = append(items, map[string]any{
			"productVariantVersionId": pvv.ID.String(),
			"quantity":                count,
			"shipmentMethodId":        in.shipmentMethodID.String(),
		})
	}
	vars := map[string]any{
		"calculateShipmentFeesInput": map[string]any{"items": items},
	}
	var data shipmentData
	emptyErr := fmt.Errorf("Response data of `query_shipment_fees` query is empty.")
	return r.gql.query(ctx, "shipment", queryShipmentFees, vars, "", &data, emptyErr)
}

// ---- zip ----------------------------------------------------------------

// zipToInternalOrderItems builds the embedded order items by iterating the
// surviving (visible) product variants and gathering the input, version, tax
// rate, count and discounts for each. Any missing map entry → error. Mirrors
// zip_to_internal_order_items + OrderItem::new.
func zipToInternalOrderItems(
	productVariantsByPV map[uuid.UUID]store.ProductVariant,
	inputsByPV map[uuid.UUID]orderItemInput,
	pvvByPV map[uuid.UUID]store.ProductVariantVersion,
	taxRateVersionByPV map[uuid.UUID]store.TaxRateVersion,
	countsByPV map[uuid.UUID]uint64,
	discountsByPV map[uuid.UUID][]store.Discount,
	createdAt time.Time,
) ([]store.OrderItem, error) {
	// Iterate in a stable id order for determinism (the original iterates a
	// HashMap, which is unordered; downstream must not rely on order, and a
	// sorted iteration is an observably-valid ordering).
	ids := make([]uuid.UUID, 0, len(productVariantsByPV))
	for id := range productVariantsByPV {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return uuidLess(ids[i], ids[j]) })

	items := make([]store.OrderItem, 0, len(ids))
	for _, id := range ids {
		pv := productVariantsByPV[id]
		in, ok := inputsByPV[id]
		if !ok {
			return nil, buildHashMapError(id)
		}
		pvv, ok := pvvByPV[id]
		if !ok {
			return nil, buildHashMapError(id)
		}
		trv, ok := taxRateVersionByPV[id]
		if !ok {
			return nil, buildHashMapError(id)
		}
		count, ok := countsByPV[id]
		if !ok {
			return nil, buildHashMapError(id)
		}
		discounts, ok := discountsByPV[id]
		if !ok {
			return nil, buildHashMapError(id)
		}
		items = append(items, store.OrderItem{
			ID:                    uuid.New(),
			CreatedAt:             createdAt,
			ProductVariant:        pv,
			ProductVariantVersion: pvv,
			TaxRateVersion:        trv,
			ShoppingCartItem:      store.UUIDRef{ID: in.shoppingCartItemID},
			Count:                 count,
			CompensatableAmount:   calculateCompensatableAmount(pvv, discounts),
			ShipmentMethod:        store.UUIDRef{ID: in.shipmentMethodID},
			InternalDiscounts:     discounts,
		})
	}
	return items, nil
}

// calculateCompensatableAmount computes floor(price × Πdiscount) as a uint64,
// folding the discounts in BTreeSet (by-_id ascending) order with float
// multiplication and truncating the float→int cast toward zero. Mirrors
// calculate_compensatable_amount.
func calculateCompensatableAmount(pvv store.ProductVariantVersion, discounts []store.Discount) uint64 {
	price := float64(pvv.Price)
	for _, d := range discounts {
		price *= d.Discount
	}
	return uint64(price)
}

// sortDedupeDiscounts sorts discounts ascending by _id and removes duplicates by
// _id, reproducing the BTreeSet<Discount> (Ord/Eq by _id) semantics.
func sortDedupeDiscounts(ds []store.Discount) []store.Discount {
	sort.Slice(ds, func(i, j int) bool { return uuidLess(ds[i].ID, ds[j].ID) })
	out := ds[:0]
	var last uuid.UUID
	haveLast := false
	for _, d := range ds {
		if haveLast && d.ID == last {
			continue
		}
		out = append(out, d)
		last = d.ID
		haveLast = true
	}
	return out
}

// buildHashMapError reproduces the original build_hash_map_error message shape
// (the exact Rust type_name strings cannot be reproduced; a stable message is
// used — callers must not depend on the text).
func buildHashMapError(id uuid.UUID) error {
	return fmt.Errorf("value for product variant of UUID: `%s` is not present.", id)
}
