package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
)

// validShipmentStatuses is the set of ShipmentStatus enum NAME strings the
// provider status callback accepts. An unknown value is rejected (the Kotlin
// UpdateStatusInput.status is a typed enum; Jackson would fail to deserialize
// an unknown value → 500).
var validShipmentStatuses = map[string]struct{}{
	"PENDING":     {},
	"IN_PROGRESS": {},
	"DELIVERED":   {},
	"FAILED":      {},
}

// RegisterRoutes registers the non-GraphQL REST routes on mux: the external
// provider status callback and the ECS endpoints. Wired via
// server.Config.ExtraRoutes — these are network-internal and never behind auth
// or gqlgen.
func (h *Handlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /shipment/{id}/status", h.updateShipmentStatus)
	mux.HandleFunc("GET /ecs/defined-variables", h.getDefinedVariables)
	mux.HandleFunc("POST /ecs/variables", h.setVariables)
}

// updateShipmentStatus handles POST /shipment/{id}/status. Body:
// {"status":"<ShipmentStatus>"}. Loads the shipment (missing → 500), sets the
// status, saves, publishes status-updated. Returns 200 on success.
func (h *Handlers) updateShipmentStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var in updateStatusInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, ok := validShipmentStatuses[in.Status]; !ok {
		http.Error(w, "invalid shipment status: "+in.Status, http.StatusInternalServerError)
		return
	}
	if err := h.Saga.UpdateShipmentStatus(r.Context(), id, in.Status); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// getDefinedVariables handles GET /ecs/defined-variables: return the fixed
// variable definitions.
func (h *Handlers) getDefinedVariables(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.ECS.DefinedVariables())
}

// setVariables handles POST /ecs/variables: apply {name: value} assignments.
// An unknown variable name (or non-integer value) → 500, matching the
// reference error(...). Returns 200 on success.
func (h *Handlers) setVariables(w http.ResponseWriter, r *http.Request) {
	var vars map[string]any
	if err := json.NewDecoder(r.Body).Decode(&vars); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.ECS.SetVariables(vars); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
