package main

import "regexp"

// This file reproduces the subset of NestJS ValidationPipe / class-validator
// behavior the simulation relies on. The reference applies a global
// ValidationPipe with DEFAULT options (no whitelist / forbidNonWhitelisted), so
// unknown fields are accepted and ignored, and only the decorated fields are
// checked. On failure the pipe throws BadRequestException with the envelope
// { statusCode: 400, message: [<constraint messages>], error: "Bad Request" }.

// uuidPattern matches the canonical 8-4-4-4-12 hyphenated hex UUID,
// case-insensitively — the form every real caller sends. class-validator's
// default @IsUUID() is version-agnostic (any RFC UUID); this accepts all such
// well-formed UUIDs.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// isUUID reports whether s is a valid UUID per the pattern above.
func isUUID(s string) bool { return uuidPattern.MatchString(s) }

// class-validator default constraint messages (validator.js v13 / class-
// validator 0.14). Reproduced for envelope fidelity; only the 400 status class
// is externally load-bearing.
func msgMustBeUUID(field string) string {
	return field + " must be a UUID"
}

func msgMustBeString(field string) string {
	return field + " must be a string"
}

func msgMustBeNumber(field string) string {
	return field + " must be a number conforming to the specified constraints"
}
