package main

import (
	"encoding/json"
	"io"
	"net/http"
)

// This file holds the HTTP response/parse helpers that reproduce NestJS's
// default response shapes: void handlers → 200 with an empty body (not 201),
// BadRequestException / NotFoundException / plain-Error envelopes.

// writeVoid writes the response for a handler that returns void/undefined:
// 200 OK with an empty body (Express adapter default when no body is returned).
func writeVoid(w http.ResponseWriter) {
	w.WriteHeader(http.StatusOK)
}

// writeJSON writes v as JSON with the given status. Used for findAll arrays,
// health, and defined-variables (the last uses a manual ordered encode).
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errorEnvelope is Nest's HttpException body. message is `any` because
// BadRequestException carries a string array (constraint messages) while
// NotFoundException carries a single string.
type errorEnvelope struct {
	StatusCode int    `json:"statusCode"`
	Message    any    `json:"message"`
	Error      string `json:"error"`
}

// writeBadRequest writes the ValidationPipe 400 envelope:
// { statusCode: 400, message: [<msgs>], error: "Bad Request" }.
func writeBadRequest(w http.ResponseWriter, msgs []string) {
	writeJSON(w, http.StatusBadRequest, errorEnvelope{
		StatusCode: http.StatusBadRequest,
		Message:    msgs,
		Error:      "Bad Request",
	})
}

// writeNotFound writes the NotFoundException 404 envelope:
// { statusCode: 404, message: "<msg>", error: "Not Found" }.
func writeNotFound(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusNotFound, errorEnvelope{
		StatusCode: http.StatusNotFound,
		Message:    msg,
		Error:      "Not Found",
	})
}

// writeInternalServerError writes Nest's default 500 body for an unhandled
// Error: { statusCode: 500, message: "Internal server error" }.
func writeInternalServerError(w http.ResponseWriter) {
	writeJSON(w, http.StatusInternalServerError, map[string]any{
		"statusCode": http.StatusInternalServerError,
		"message":    "Internal server error",
	})
}

// decodeBody parses the JSON request body into dst. On a malformed body it
// writes a 400 (Express's JSON body-parser rejects invalid JSON with 400) and
// returns false. An empty body decodes as the zero value (all fields absent),
// which then fails the field validators — matching Nest validating an empty
// object.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeBadRequest(w, []string{"Unexpected end of JSON input"})
		return false
	}
	if len(body) == 0 {
		// No body: treat as an empty object so the required-field validators run.
		return true
	}
	if err := json.Unmarshal(body, dst); err != nil {
		writeBadRequest(w, []string{"Unexpected token in JSON"})
		return false
	}
	return true
}

// readRawBody returns the raw request body, used where the full JSON must be
// re-emitted (shipment register). An empty body yields an empty object so the
// shipmentId validator runs.
func readRawBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeBadRequest(w, []string{"Unexpected end of JSON input"})
		return nil, false
	}
	if len(body) == 0 {
		return []byte("{}"), true
	}
	return body, true
}
