// Package service holds the shipment domain logic shared by the GraphQL
// resolvers and the Dapr event handlers: fee/weight calculation, the
// least-expensive-method selection, and the create-shipment saga. It mirrors
// org.misarch.shipment.service.{ShipmentMethodService,ShipmentService}.
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"misarch/shipment/store"
)

// ItemWithQuantity is a product-variant-version id paired with a quantity —
// the common input to weight/fee calculation (mirrors
// ProductVariantVersionWithQuantityInput).
type ItemWithQuantity struct {
	ProductVariantVersionID uuid.UUID
	Quantity                int
}

// ItemWithQuantityAndMethod additionally carries the shipment method the item
// ships with (mirrors ProductVariantVersionWithQuantityAndShipmentMethodInput);
// used by calculateShipmentFees.
type ItemWithQuantityAndMethod struct {
	ProductVariantVersionID uuid.UUID
	Quantity                int
	ShipmentMethodID        uuid.UUID
}

// Service bundles the store used by the domain logic. It is embedded by the
// higher-level shipment saga (see shipment.go) and used directly for the pure
// fee calculations.
type Service struct {
	Store *store.Store
}

// NewService builds a Service over the given store.
func NewService(st *store.Store) *Service {
	return &Service{Store: st}
}

// CalculateFees computes the fee for one shipment method at a given quantity
// and weight: baseFees + quantity*feesPerItem + int(weight*feesPerKg).
//
// CRITICAL: the truncation (Kotlin Double.toInt(), truncate toward zero)
// applies to the weight*feesPerKg PRODUCT only, THEN the sum is added. Weights
// are non-negative in practice, so int() truncation toward zero matches.
func CalculateFees(m store.ShipmentMethod, quantity int, weight float64) int {
	return m.BaseFees + (quantity * m.FeesPerItem) + int(weight*float64(m.FeesPerKg))
}

// CalculateQuantityAndWeight validates the items and returns their total
// quantity and total weight. Validation (in the reference order):
//  1. every quantity > 0, else "Quantity must be greater than 0".
//  2. every productVariantVersionId exists, else
//     "Not all product variant versions are valid".
//
// weight = Σ pvv.weight * quantity; quantity = Σ quantity.
func (s *Service) CalculateQuantityAndWeight(ctx context.Context, items []ItemWithQuantity) (int, float64, error) {
	for _, it := range items {
		if it.Quantity <= 0 {
			return 0, 0, fmt.Errorf("Quantity must be greater than 0")
		}
	}

	ids := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ProductVariantVersionID)
	}
	pvvs, err := s.Store.FindProductVariantVersionsByIDs(ctx, ids)
	if err != nil {
		return 0, 0, err
	}
	weightByID := make(map[uuid.UUID]float64, len(pvvs))
	for _, p := range pvvs {
		weightByID[p.ID] = p.Weight
	}
	for _, it := range items {
		if _, ok := weightByID[it.ProductVariantVersionID]; !ok {
			return 0, 0, fmt.Errorf("Not all product variant versions are valid")
		}
	}

	totalQuantity := 0
	weight := 0.0
	for _, it := range items {
		weight += weightByID[it.ProductVariantVersionID] * float64(it.Quantity)
		totalQuantity += it.Quantity
	}
	return totalQuantity, weight, nil
}

// CalculateShipmentFees groups items by shipment method, requires every
// referenced method to exist ("Not all shipment methods are valid"), computes
// each group's fee, and returns the sum across groups. Mirrors
// ShipmentMethodService.calculateShipmentFees.
func (s *Service) CalculateShipmentFees(ctx context.Context, items []ItemWithQuantityAndMethod) (int, error) {
	// Group by shipment method, preserving a distinct id set.
	groups := map[uuid.UUID][]ItemWithQuantity{}
	var methodIDs []uuid.UUID
	for _, it := range items {
		if _, seen := groups[it.ShipmentMethodID]; !seen {
			methodIDs = append(methodIDs, it.ShipmentMethodID)
		}
		groups[it.ShipmentMethodID] = append(groups[it.ShipmentMethodID], ItemWithQuantity{
			ProductVariantVersionID: it.ProductVariantVersionID,
			Quantity:                it.Quantity,
		})
	}

	methods, err := s.Store.FindShipmentMethodsByIDs(ctx, methodIDs)
	if err != nil {
		return 0, err
	}
	methodByID := make(map[uuid.UUID]store.ShipmentMethod, len(methods))
	for _, m := range methods {
		methodByID[m.ID] = m
	}
	for _, id := range methodIDs {
		if _, ok := methodByID[id]; !ok {
			return 0, fmt.Errorf("Not all shipment methods are valid")
		}
	}

	total := 0
	for _, id := range methodIDs {
		method := methodByID[id]
		quantity, weight, err := s.CalculateQuantityAndWeight(ctx, groups[id])
		if err != nil {
			return 0, err
		}
		total += CalculateFees(method, quantity, weight)
	}
	return total, nil
}

// FindLeastExpensiveShipmentMethod returns the cheapest shipment method for the
// given order items' total quantity+weight, considering ALL methods including
// archived ones (findAll() with no archived filter). Ties resolve to the first
// method in the store's returned order (matching Kotlin minBy's first-minimum).
// Errors "No shipment methods available" if there are none.
func (s *Service) FindLeastExpensiveShipmentMethod(ctx context.Context, items []store.OrderItem) (store.ShipmentMethod, error) {
	methods, err := s.Store.FindAllShipmentMethods(ctx)
	if err != nil {
		return store.ShipmentMethod{}, err
	}
	if len(methods) == 0 {
		return store.ShipmentMethod{}, fmt.Errorf("No shipment methods available")
	}

	withQty := make([]ItemWithQuantity, 0, len(items))
	for _, it := range items {
		withQty = append(withQty, ItemWithQuantity{
			ProductVariantVersionID: it.ProductVariantVersionID,
			Quantity:                it.Quantity,
		})
	}
	quantity, weight, err := s.CalculateQuantityAndWeight(ctx, withQty)
	if err != nil {
		return store.ShipmentMethod{}, err
	}

	best := methods[0]
	bestFee := CalculateFees(best, quantity, weight)
	for _, m := range methods[1:] {
		fee := CalculateFees(m, quantity, weight)
		if fee < bestFee {
			best = m
			bestFee = fee
		}
	}
	return best, nil
}
