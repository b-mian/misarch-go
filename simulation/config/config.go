// Package config implements the simulation service's ECS (Experiment-Config-
// Sidecar) variable store: the six runtime-tunable variables with their
// JSON-schema types and defaults, the /ecs/defined-variables payload, the
// POST /ecs/variables type-casting logic, and the getCurrentVariableValue
// resolution order (override map → env → fallback) that the rest of the
// service reads tunables through.
//
// It is a faithful port of the reference ConfigurationService: unknown
// variable keys on set surface as an error → HTTP 500 (an unhandled Error in
// the reference), values are cast by the variable's JSON-schema type using
// JS-equivalent semantics (Number/parseInt/==="true"/String), and env values
// are returned as their raw string on a map miss (the reference relied on
// Nest's ConfigService returning strings).
//
// JS numbers are float64, so all numeric tunables (both the JSON-schema
// "integer" and "number" kinds) are stored and returned as float64. This
// preserves the reference's NaN behavior exactly: a garbage value cast to NaN
// makes `count >= max` always false (rate limit effectively disabled) and
// `delay*1000` behave like 0 in setTimeout — Go has no integer NaN, so an int
// path could not reproduce this.
package config

import (
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
)

// VariableDefinition is one entry of the /ecs/defined-variables response. The
// JSON keys are lowercase (`type`, `defaultValue`) to match the reference
// service's emitted bytes exactly.
type VariableDefinition struct {
	Type         SchemaType `json:"type"`
	DefaultValue any        `json:"defaultValue"`
}

// SchemaType is the JSON-schema fragment describing a variable's type.
type SchemaType struct {
	Schema string `json:"$schema"`
	Type   string `json:"type"`
}

const jsonSchemaDraft07 = "http://json-schema.org/draft-07/schema#"

// definedVar pairs a variable name with its definition, preserving the
// reference's array-literal insertion order (defined-variables is emitted in
// this order).
type definedVar struct {
	name string
	def  VariableDefinition
}

// definedVariables mirrors variable-definitions.ts exactly: names, JSON-schema
// types (integer/number), defaults, and order.
var definedVariables = []definedVar{
	{"PAYMENTS_PER_MINUTE", VariableDefinition{SchemaType{jsonSchemaDraft07, "integer"}, 1000000}},
	{"SHIPMENTS_PER_MINUTE", VariableDefinition{SchemaType{jsonSchemaDraft07, "integer"}, 1000000}},
	{"PAYMENT_PROCESSING_TIME", VariableDefinition{SchemaType{jsonSchemaDraft07, "integer"}, 5}},
	{"SHIPMENT_PROCESSING_TIME", VariableDefinition{SchemaType{jsonSchemaDraft07, "integer"}, 5}},
	{"PAYMENT_SUCCESS_RATE", VariableDefinition{SchemaType{jsonSchemaDraft07, "number"}, 0.95}},
	{"SHIPMENT_SUCCESS_RATE", VariableDefinition{SchemaType{jsonSchemaDraft07, "number"}, 0.95}},
}

// NamedDefinition is an ordered (name, definition) pair for serializing the
// defined-variables endpoint in insertion order.
type NamedDefinition struct {
	Name string
	Def  VariableDefinition
}

// Service holds the in-memory ECS override map. The zero value is not usable;
// call New.
type Service struct {
	mu             sync.RWMutex
	configurations map[string]any
}

// New builds the config service and seeds every defined variable to its
// default value (as onModuleInit does). It panics only if a defined variable
// lacks a default — a programmer error given the static table above, and the
// reference throws at startup in the same case.
func New() *Service {
	s := &Service{configurations: make(map[string]any)}
	for _, v := range definedVariables {
		if v.def.DefaultValue == nil {
			panic(fmt.Sprintf("Variable %s does not have a default value", v.name))
		}
		// Seed through SetVariables so the stored value is type-cast exactly as
		// a sidecar push would cast it (the reference calls setVariables in
		// onModuleInit with the raw default).
		if err := s.SetVariables(map[string]any{v.name: v.def.DefaultValue}); err != nil {
			panic(err)
		}
	}
	return s
}

// DefinedVariables returns the ordered defined-variable list for the
// GET /ecs/defined-variables handler.
func DefinedVariables() []NamedDefinition {
	out := make([]NamedDefinition, len(definedVariables))
	for i, v := range definedVariables {
		out[i] = NamedDefinition{Name: v.name, Def: v.def}
	}
	return out
}

// findDefinition returns the definition for name and whether it is defined.
func findDefinition(name string) (VariableDefinition, bool) {
	for _, v := range definedVariables {
		if v.name == name {
			return v.def, true
		}
	}
	return VariableDefinition{}, false
}

// SetVariables casts and stores each key/value pair, mirroring
// ConfigurationService.setVariables:
//   - unknown key → error (surfaces as HTTP 500).
//   - value cast by the variable's JSON-schema type: number → JS Number(),
//     integer → parseInt(_,10), boolean → value==="true", string → String().
//   - unsupported schema type → error.
//
// Returning an error (rather than panicking) lets the HTTP handler map it to a
// 500 without crashing the process.
func (s *Service) SetVariables(variables map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, value := range variables {
		def, ok := findDefinition(key)
		if !ok {
			return fmt.Errorf("Variable %s is not defined", key)
		}
		cast, err := castValue(key, def.Type.Type, value)
		if err != nil {
			return err
		}
		slog.Info(fmt.Sprintf("Setting variable %s to %s", key, jsStringOf(cast)))
		s.configurations[key] = cast
	}
	return nil
}

// castValue reproduces the reference's per-type casting with JS-equivalent
// semantics. It never fails on non-numeric input for number/integer — it
// yields NaN (as JS Number()/parseInt do), preserving that quirk. Numeric
// results are stored as float64 (JS number).
func castValue(key, schemaType string, value any) (any, error) {
	switch schemaType {
	case "number":
		return jsNumber(value), nil
	case "integer":
		return jsParseIntBase10(value), nil
	case "boolean":
		return jsStringOf(value) == "true", nil
	case "string":
		return jsStringOf(value), nil
	default:
		return nil, fmt.Errorf("Variable %s has an unsupported type %s", key, schemaType)
	}
}

// GetCurrentVariableValueNumber resolves name as a float64 (a JS number),
// replicating getCurrentVariableValue<number>: override map (already cast) →
// env (string, JS-Number-parsed) → fallback. Used for every numeric tunable
// (per-minute limits, processing times, success rates). Returning float64
// preserves NaN semantics for garbage values.
func (s *Service) GetCurrentVariableValueNumber(name string, fallback float64) float64 {
	if v, ok := s.lookup(name); ok {
		return jsNumber(v)
	}
	if env, ok := os.LookupEnv(name); ok {
		// The reference returns the env string as-is; numeric callers then use
		// it in arithmetic where JS coerces it. Coerce with Number().
		return jsNumber(env)
	}
	slog.Error(fmt.Sprintf("Variable %s is not defined", name))
	return fallback
}

// GetCurrentVariableValueString resolves name as a string, replicating
// getCurrentVariableValue<string>: override map → env (returned as-is) →
// fallback. Used for RABBITMQ_URL, PAYMENT_URL, SHIPMENT_URL, RETRY_COUNT.
func (s *Service) GetCurrentVariableValueString(name, fallback string) string {
	if v, ok := s.lookup(name); ok {
		return jsStringOf(v)
	}
	if env, ok := os.LookupEnv(name); ok {
		return env
	}
	slog.Error(fmt.Sprintf("Variable %s is not defined", name))
	return fallback
}

// lookup returns the override-map value for name (map-only, no env), matching
// the first branch of getCurrentVariableValue (value !== undefined).
func (s *Service) lookup(name string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.configurations[name]
	return v, ok
}

// jsNumber mimics JavaScript's Number(v): numeric passthrough, "" → 0, a
// trimmed numeric string → its value, otherwise NaN.
func jsNumber(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case bool:
		if n {
			return 1
		}
		return 0
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return math.NaN()
		}
		return f
	default:
		return math.NaN()
	}
}

// jsParseIntBase10 mimics parseInt(v, 10): for strings it reads an optional
// sign and the leading run of decimal digits, ignoring any trailing fractional
// part or garbage ("10.9" → 10, "12px" → 12); no leading digits → NaN. Numeric
// inputs are truncated toward zero. The result is a float64 so NaN is
// representable (JS parseInt returns a number). Booleans/other → NaN.
func jsParseIntBase10(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return math.NaN()
		}
		return math.Trunc(n)
	case string:
		return parseIntPrefix(n)
	default:
		return math.NaN()
	}
}

// parseIntPrefix implements the string form of parseInt(_, 10).
func parseIntPrefix(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return math.NaN()
	}
	i := 0
	sign := 1.0
	if s[0] == '+' || s[0] == '-' {
		if s[0] == '-' {
			sign = -1
		}
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return math.NaN()
	}
	n, err := strconv.ParseFloat(s[start:i], 64)
	if err != nil {
		return math.NaN()
	}
	return sign * n
}

// jsStringOf mimics String(v) for the value shapes the config can hold,
// including integer-valued floats (rendered without a decimal point, e.g.
// 1000000, matching how the reference logs a parseInt result).
func jsStringOf(v any) string {
	switch n := v.(type) {
	case string:
		return n
	case bool:
		if n {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(n)
	case int64:
		return strconv.FormatInt(n, 10)
	case float64:
		return formatJSNumber(n)
	default:
		return fmt.Sprintf("%v", n)
	}
}

// formatJSNumber renders a float64 the way JS String(number) would for the
// values this service produces: NaN → "NaN", integer-valued → no decimal
// point, otherwise the shortest round-tripping decimal.
func formatJSNumber(f float64) string {
	if math.IsNaN(f) {
		return "NaN"
	}
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}
