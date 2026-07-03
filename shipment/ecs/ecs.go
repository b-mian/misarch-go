// Package ecs holds the shipment service's experiment-configuration (ECS)
// state: the two runtime-tunable variables that govern the external-provider
// retry loop. Values are process-local, mutable at runtime via
// POST /ecs/variables, and reset to their defaults on restart — they are NOT
// persisted. Mirrors org.misarch.shipment.ecs.ExperimentConfigService.
package ecs

import (
	"fmt"
	"sync"
)

// Variable names (mirrors ExperimentConfigService companion constants).
const (
	ProviderRetriesName    = "providerRetries"
	ProviderRetryDelayName = "providerRetryDelay"
)

// Default values (providerRetries: 3 guarded retries; providerRetryDelay: 500ms).
const (
	defaultProviderRetries    = 3
	defaultProviderRetryDelay = 500
)

// variableType is the nested JSON-schema object advertised for each variable.
// The field order ($schema, type, minimum) matches the Kotlin anonymous object
// so the serialized JSON mirrors the reference declaration order.
type variableType struct {
	Schema  string `json:"$schema"`
	Type    string `json:"type"`
	Minimum int    `json:"minimum"`
}

// VariableDefinition is the {type, defaultValue} shape returned by
// GET /ecs/defined-variables (mirrors ecs.VariableDefinition).
type VariableDefinition struct {
	Type         variableType `json:"type"`
	DefaultValue int          `json:"defaultValue"`
}

// integerSchema is the shared JSON-schema for a non-negative integer variable.
var integerSchema = variableType{
	Schema:  "http://json-schema.org/draft-07/schema#",
	Type:    "integer",
	Minimum: 0,
}

// Config holds the mutable experiment-config state. Its zero value is not
// usable; construct with New. All access is guarded so the provider retry loop
// (reader) and POST /ecs/variables (writer) may run concurrently.
type Config struct {
	mu                 sync.RWMutex
	providerRetries    int
	providerRetryDelay int
}

// New returns a Config initialized to the default values.
func New() *Config {
	return &Config{
		providerRetries:    defaultProviderRetries,
		providerRetryDelay: defaultProviderRetryDelay,
	}
}

// ProviderRetries returns the current number of guarded provider retries (does
// NOT include the final unguarded attempt).
func (c *Config) ProviderRetries() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.providerRetries
}

// ProviderRetryDelay returns the current delay (ms) between provider retries.
func (c *Config) ProviderRetryDelay() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.providerRetryDelay
}

// DefinedVariables returns the fixed variable definitions advertised at
// GET /ecs/defined-variables.
func (c *Config) DefinedVariables() map[string]VariableDefinition {
	return map[string]VariableDefinition{
		ProviderRetriesName:    {Type: integerSchema, DefaultValue: defaultProviderRetries},
		ProviderRetryDelayName: {Type: integerSchema, DefaultValue: defaultProviderRetryDelay},
	}
}

// SetVariables applies a batch of variable assignments (from
// POST /ecs/variables). Values arrive as decoded JSON numbers; each is cast to
// int. An unknown variable name errors ("Unknown variable: <name>"), matching
// the reference `error(...)` (which surfaces as a 500). A non-integer value
// also errors (the reference throws ClassCastException → 500).
func (c *Config) SetVariables(vars map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for name, value := range vars {
		switch name {
		case ProviderRetriesName:
			n, err := toInt(value)
			if err != nil {
				return err
			}
			c.providerRetries = n
		case ProviderRetryDelayName:
			n, err := toInt(value)
			if err != nil {
				return err
			}
			c.providerRetryDelay = n
		default:
			return fmt.Errorf("Unknown variable: %s", name)
		}
	}
	return nil
}

// toInt coerces a decoded JSON value to an int. JSON numbers decode to
// float64; the reference cast `value as Int` requires an integral value.
func toInt(v any) (int, error) {
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case int:
		return n, nil
	default:
		return 0, fmt.Errorf("value %v is not an integer", v)
	}
}
