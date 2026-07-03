package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"misarch/simulation/config"
	"misarch/simulation/queue"
	"misarch/simulation/store"
)

// queuePublisher is the subset of the queue processor the handlers use
// (registration emit). An interface keeps the HTTP layer decoupled from the
// AMQP processor and testable without a broker.
type queuePublisher interface {
	Publish(ctx context.Context, queue string, data any) error
}

// callbackSender is the subset of the connector the manual-update handlers use.
type callbackSender interface {
	SendUpdateToPayment(ctx context.Context, paymentID, status string)
	SendUpdateToShipment(ctx context.Context, shipmentID, status string)
}

// handlers bundles the dependencies the REST routes need. It is the Go analog
// of the reference's PaymentService / ShipmentService / ConfigurationService
// controllers, wired into server.Config.ExtraRoutes.
type handlers struct {
	cfg       *config.Service
	payments  *store.PaymentRepository
	shipments *store.ShipmentRepository
	connector callbackSender
	queue     queuePublisher
	// ctx is the service lifetime context used for the fire-and-forget manual
	// callbacks (the reference does not await them in the controller).
	ctx context.Context
}

// register mounts every route on mux. GraphQL, /, /register, /metrics are
// intentionally absent (see spec §4.10).
func (h *handlers) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /payment/register", h.paymentRegister)
	mux.HandleFunc("POST /payment/update", h.paymentUpdate)
	mux.HandleFunc("POST /payment/findAll", h.paymentFindAll)

	mux.HandleFunc("POST /shipment/register", h.shipmentRegister)
	mux.HandleFunc("POST /shipment/update", h.shipmentUpdate)
	mux.HandleFunc("POST /shipment/findAll", h.shipmentFindAll)

	mux.HandleFunc("GET /ecs/defined-variables", h.definedVariables)
	mux.HandleFunc("POST /ecs/variables", h.setVariables)

	// NOTE: GET /health is served by the platform server.Run itself (returning
	// {"status":"UP"}), so it is deliberately NOT registered here — a second
	// registration of the same pattern would panic the ServeMux. The reference
	// returns {"status":"OK"}; the body differs but both are 200, so the
	// compose/K8s `curl -f` liveness probe passes identically. See DEVIATIONS.
}

// --- payment ---

// createPaymentDto is CreatePaymentDto with presence/type tracking:
// paymentId @IsUUID, amount @IsNumber, paymentType @IsString. Extra fields are
// accepted and ignored (default ValidationPipe).
type createPaymentDto struct {
	PaymentID   *json.RawMessage `json:"paymentId"`
	Amount      *json.RawMessage `json:"amount"`
	PaymentType *json.RawMessage `json:"paymentType"`
}

// paymentRegister handles POST /payment/register.
func (h *handlers) paymentRegister(w http.ResponseWriter, r *http.Request) {
	var dto createPaymentDto
	if !decodeBody(w, r, &dto) {
		return
	}
	paymentID, amountRaw, paymentType, ok := validateCreatePayment(w, dto)
	if !ok {
		return
	}

	slog.Info("Registering payment")

	// Emit the FULL DTO (incl. amount) to the queue, then store the record
	// WITHOUT amount ([QUIRK]). Emit-before-store matches the reference order.
	body := map[string]any{
		"paymentId":   paymentID,
		"amount":      amountRaw, // preserved as the original JSON number
		"paymentType": paymentType,
	}
	if err := h.queue.Publish(r.Context(), queue.PaymentsQueue, body); err != nil {
		slog.Error("failed to emit register-payment", "error", err)
	}
	h.payments.Create(store.Payment{ID: paymentID, PaymentType: paymentType, Blocked: false})

	writeVoid(w)
}

// updatePaymentDto is UpdatePaymentDto: paymentId @IsUUID, status @IsString
// (any string; NOT an enum check).
type updatePaymentDto struct {
	PaymentID *json.RawMessage `json:"paymentId"`
	Status    *json.RawMessage `json:"status"`
}

// paymentUpdate handles POST /payment/update.
func (h *handlers) paymentUpdate(w http.ResponseWriter, r *http.Request) {
	var dto updatePaymentDto
	if !decodeBody(w, r, &dto) {
		return
	}
	paymentID, status, ok := validateUpdate(w, dto.PaymentID, dto.Status, "paymentId")
	if !ok {
		return
	}

	slog.Info(fmt.Sprintf("Manually updating payment: %s -> %s", paymentID, status))

	if !h.payments.FindByID(paymentID) {
		// [QUIRK] the reference logs "Shipment not found" for a payment.
		slog.Error(fmt.Sprintf("Shipment not found: %s", paymentID))
		writeNotFound(w, "Shipment not found")
		return
	}

	h.payments.Block(paymentID)
	// Fire-and-forget (the reference does not await the send before returning).
	h.connector.SendUpdateToPayment(h.ctx, paymentID, status)

	writeVoid(w)
}

// paymentFindAll handles POST /payment/findAll → array of Payment records.
func (h *handlers) paymentFindAll(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.payments.FindAll())
}

// --- shipment ---

// createShipmentDto is CreateShipmentDto: only shipmentId @IsUUID is validated;
// the rich ShipmentProviderShipmentDefinition fields are accepted and ignored.
type createShipmentDto struct {
	ShipmentID *json.RawMessage `json:"shipmentId"`
}

// shipmentRegister handles POST /shipment/register.
func (h *handlers) shipmentRegister(w http.ResponseWriter, r *http.Request) {
	// Keep the full raw body so the queue message carries every field the
	// caller sent (the reference emits the full DTO), while validating only
	// shipmentId.
	raw, ok := readRawBody(w, r)
	if !ok {
		return
	}
	var dto createShipmentDto
	if err := json.Unmarshal(raw, &dto); err != nil {
		// A non-object / invalid JSON body: mirror a validation failure envelope
		// (shipmentId would be missing → "must be a UUID").
		writeBadRequest(w, []string{msgMustBeUUID("shipmentId")})
		return
	}
	shipmentID, ok := validateShipmentID(w, dto.ShipmentID)
	if !ok {
		return
	}

	// [QUIRK] trailing extra "}" in the reference log string.
	slog.Info(fmt.Sprintf("Registering shipment: %s}", string(raw)))

	// Store BEFORE emit here (opposite order to payment).
	h.shipments.Create(store.Shipment{ID: shipmentID, Blocked: false})

	// Emit the full original body (all caller fields), as the reference emits
	// the full CreateShipmentDto (which, being un-whitelisted, retained them).
	if err := h.queue.Publish(r.Context(), queue.ShipmentsQueue, json.RawMessage(raw)); err != nil {
		slog.Error("failed to emit register-shipment", "error", err)
	}

	writeVoid(w)
}

// updateShipmentDto is UpdateShipmentDto: shipmentId @IsUUID, status @IsString.
type updateShipmentDto struct {
	ShipmentID *json.RawMessage `json:"shipmentId"`
	Status     *json.RawMessage `json:"status"`
}

// shipmentUpdate handles POST /shipment/update.
func (h *handlers) shipmentUpdate(w http.ResponseWriter, r *http.Request) {
	var dto updateShipmentDto
	if !decodeBody(w, r, &dto) {
		return
	}
	shipmentID, status, ok := validateUpdate(w, dto.ShipmentID, dto.Status, "shipmentId")
	if !ok {
		return
	}

	slog.Info(fmt.Sprintf("Manually updating shipment: %s -> %s", shipmentID, status))

	if !h.shipments.FindByID(shipmentID) {
		slog.Error(fmt.Sprintf("Shipment not found: %s", shipmentID))
		writeNotFound(w, "Shipment not found")
		return
	}

	h.shipments.Block(shipmentID)
	h.connector.SendUpdateToShipment(h.ctx, shipmentID, status)

	writeVoid(w)
}

// shipmentFindAll handles POST /shipment/findAll → array of Shipment records.
func (h *handlers) shipmentFindAll(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.shipments.FindAll())
}

// --- ECS ---

// definedVariables handles GET /ecs/defined-variables. It emits the six defined
// variables with lowercase keys (type, defaultValue) in insertion order, using
// an ordered manual encode so field order matches the reference bytes.
func (h *handlers) definedVariables(w http.ResponseWriter, _ *http.Request) {
	defs := config.DefinedVariables()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	buf := make([]byte, 0, 1024)
	buf = append(buf, '{')
	for i, d := range defs {
		if i > 0 {
			buf = append(buf, ',')
		}
		key, _ := json.Marshal(d.Name)
		val, _ := json.Marshal(d.Def)
		buf = append(buf, key...)
		buf = append(buf, ':')
		buf = append(buf, val...)
	}
	buf = append(buf, '}')
	_, _ = w.Write(buf)
}

// setVariables handles POST /ecs/variables. It casts each value per the
// variable's type and stores it; an unknown key surfaces as HTTP 500 (an
// unhandled Error in the reference), NOT 400. Success → 200 empty body.
func (h *handlers) setVariables(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeInternalServerError(w)
		return
	}
	// Preserve the raw JSON per key so the config caster sees the original JSON
	// value shape (string, number, bool) exactly as the sidecar sent it.
	var raw map[string]json.RawMessage
	if len(body) == 0 {
		raw = map[string]json.RawMessage{}
	} else if err := json.Unmarshal(body, &raw); err != nil {
		// A malformed body is not something the reference's setVariables loop
		// would receive (Nest parses JSON first); an invalid JSON body yields a
		// 400 from Express. Treat parse failure as a bad request envelope.
		writeBadRequest(w, []string{"Unexpected token in JSON"})
		return
	}

	vars := make(map[string]any, len(raw))
	for k, v := range raw {
		vars[k] = jsonValue(v)
	}
	if err := h.cfg.SetVariables(vars); err != nil {
		// Unknown key / unsupported type → 500 (reference throws a plain Error).
		writeInternalServerError(w)
		return
	}
	writeVoid(w)
}

// --- validation helpers ---

// validateCreatePayment validates CreatePaymentDto, collecting every failing
// constraint into one 400 envelope (as class-validator does). Returns the
// parsed paymentId (string), the raw amount JSON (to re-emit verbatim), and the
// paymentType (string).
func validateCreatePayment(w http.ResponseWriter, dto createPaymentDto) (string, json.RawMessage, string, bool) {
	var msgs []string

	paymentID, idOK := stringField(dto.PaymentID)
	if !idOK || !isUUID(paymentID) {
		msgs = append(msgs, msgMustBeUUID("paymentId"))
	}
	amountOK := isJSONNumber(dto.Amount)
	if !amountOK {
		msgs = append(msgs, msgMustBeNumber("amount"))
	}
	paymentType, typeOK := stringField(dto.PaymentType)
	if !typeOK {
		msgs = append(msgs, msgMustBeString("paymentType"))
	}

	if len(msgs) > 0 {
		writeBadRequest(w, msgs)
		return "", nil, "", false
	}
	var amount json.RawMessage
	if dto.Amount != nil {
		amount = *dto.Amount
	}
	return paymentID, amount, paymentType, true
}

// validateUpdate validates an update DTO ({<idField> @IsUUID, status
// @IsString}). Returns the id and status strings.
func validateUpdate(w http.ResponseWriter, idRaw, statusRaw *json.RawMessage, idField string) (string, string, bool) {
	var msgs []string

	id, idOK := stringField(idRaw)
	if !idOK || !isUUID(id) {
		msgs = append(msgs, msgMustBeUUID(idField))
	}
	status, statusOK := stringField(statusRaw)
	if !statusOK {
		msgs = append(msgs, msgMustBeString("status"))
	}

	if len(msgs) > 0 {
		writeBadRequest(w, msgs)
		return "", "", false
	}
	return id, status, true
}

// validateShipmentID validates just shipmentId @IsUUID.
func validateShipmentID(w http.ResponseWriter, idRaw *json.RawMessage) (string, bool) {
	id, ok := stringField(idRaw)
	if !ok || !isUUID(id) {
		writeBadRequest(w, []string{msgMustBeUUID("shipmentId")})
		return "", false
	}
	return id, true
}

// stringField reports whether raw is a present JSON string and returns its
// value. Absent or non-string → (_, false), matching @IsString failing.
func stringField(raw *json.RawMessage) (string, bool) {
	if raw == nil {
		return "", false
	}
	var s string
	if err := json.Unmarshal(*raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// isJSONNumber reports whether raw is a present JSON number (@IsNumber; no
// string coercion, since the default ValidationPipe does not transform).
func isJSONNumber(raw *json.RawMessage) bool {
	if raw == nil {
		return false
	}
	var f float64
	return json.Unmarshal(*raw, &f) == nil
}

// jsonValue decodes a raw JSON value into a Go any (float64/string/bool/nil/…)
// so the config caster receives the value's native shape.
func jsonValue(raw json.RawMessage) any {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return v
}
