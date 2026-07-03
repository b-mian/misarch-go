package graph

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// This file reproduces the class-validator input checks the original applied to
// CreateCreditCardInformationInput: @IsCreditCard() on cardNumber and the
// custom @IsExpirationDate() on expirationDate. Error strings are copied
// verbatim (they are the messages surfaced to the client).

const (
	errInvalidCardNumber = "The credit card number is invalid"
	errInvalidExpiration = "The expiration date is invalid or expired"
)

// maskCardNumber reproduces `cardNumber.replace(/\d(?=\d{4})/g, '*')` EXACTLY:
// a digit is replaced by '*' iff the FOUR characters immediately following it
// are all digits (a contiguous `\d{4}` lookahead). Separators break the run, so
// a space/dash-formatted number like "4111 1111 1111 1111" is left entirely
// unmasked (verified against Node); "4111111111111111" → "************1111" and
// "123456789" → "*****6789".
func maskCardNumber(cardNumber string) string {
	runes := []rune(cardNumber)
	isDigit := func(i int) bool {
		return i >= 0 && i < len(runes) && runes[i] >= '0' && runes[i] <= '9'
	}
	var b strings.Builder
	for i, r := range runes {
		if isDigit(i) && isDigit(i+1) && isDigit(i+2) && isDigit(i+3) && isDigit(i+4) {
			b.WriteRune('*')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// creditCardProviderRegexes approximates validator.js's isCreditCard provider
// set (the original used class-validator's @IsCreditCard, backed by validator.js).
// A number is accepted if it matches one of these AND passes the Luhn check.
var creditCardProviderRegexes = []*regexp.Regexp{
	regexp.MustCompile(`^(?:4[0-9]{12}(?:[0-9]{3,6})?|4[0-9]{12})$`),                                           // Visa
	regexp.MustCompile(`^(?:5[1-5][0-9]{14}|2(?:22[1-9]|2[3-9][0-9]|[3-6][0-9]{2}|7[01][0-9]|720)[0-9]{12})$`), // Mastercard
	regexp.MustCompile(`^3[47][0-9]{13}$`),                                                                     // Amex
	regexp.MustCompile(`^3(?:0[0-5]|[68][0-9])[0-9]{11}$`),                                                     // Diners Club
	regexp.MustCompile(`^6(?:011|5[0-9][0-9])[0-9]{12,15}$`),                                                   // Discover
	regexp.MustCompile(`^(?:2131|1800|35\d{3})\d{11}$`),                                                        // JCB
	regexp.MustCompile(`^(6[27][0-9]{14}|^(81[0-9]{14,17}))$`),                                                 // UnionPay
	regexp.MustCompile(`^(5[06789]|6)[0-9]{0,17}$`),                                                            // Maestro
}

// isCreditCardValid reproduces @IsCreditCard(): the number (with spaces/dashes
// removed) must match a known provider format and pass the Luhn checksum.
func isCreditCardValid(cardNumber string) bool {
	sanitized := strings.NewReplacer(" ", "", "-", "").Replace(cardNumber)
	if sanitized == "" {
		return false
	}
	for _, r := range sanitized {
		if r < '0' || r > '9' {
			return false
		}
	}
	matched := false
	for _, re := range creditCardProviderRegexes {
		if re.MatchString(sanitized) {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	return luhnValid(sanitized)
}

// luhnValid runs the Luhn checksum on a digit string.
func luhnValid(digits string) bool {
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// isExpirationDateValid reproduces the custom IsExpirationDate validator exactly
// (format MM/YY or MM/YYYY):
//   - split on '/', parse both parts as base-10 ints.
//   - currentYear = now.Year() % 100; currentMonth = now.Month() (1..12).
//   - adjustedYear = year > 2000 ? year - 2000 : year.
//   - invalid if adjustedYear < currentYear || adjustedYear > currentYear + 20.
//   - else invalid if adjustedYear == currentYear && month < currentMonth.
//   - else invalid if month < 1 || month > 12.
//   - otherwise valid.
//
// The arithmetic is intentionally lenient/quirky and reproduced faithfully.
// parseInt(base 10) on a non-numeric string yields NaN in JS; any comparison
// with NaN is false, so the first two branches fall through, but the
// month<1||month>12 branch is also false for NaN, making a garbage string
// "valid". We mirror that: unparseable parts are treated as a sentinel that
// fails every numeric comparison (so the function returns true — as JS does).
func isExpirationDateValid(value string) bool {
	parts := strings.SplitN(value, "/", 2)
	if len(parts) != 2 {
		// JS: [month, year] where year is undefined → NaN → all comparisons
		// false → returns true. We treat a missing second part the same way.
		month, monthOK := parseBase10(parts[0])
		return jsExpirationLogic(month, monthOK, 0, false)
	}
	month, monthOK := parseBase10(parts[0])
	year, yearOK := parseBase10(parts[1])
	return jsExpirationLogic(month, monthOK, year, yearOK)
}

// jsExpirationLogic applies the branch logic with JS NaN semantics: a value
// that failed to parse (ok == false) makes every numeric comparison involving
// it evaluate false, matching JavaScript's NaN comparisons.
func jsExpirationLogic(month int, monthOK bool, year int, yearOK bool) bool {
	now := time.Now()
	currentYear := now.Year() % 100
	currentMonth := int(now.Month())

	// adjustedYear = year > 2000 ? year - 2000 : year (only meaningful if yearOK)
	adjustedYear := year
	adjustedOK := yearOK
	if yearOK && year > 2000 {
		adjustedYear = year - 2000
	}

	// if (adjustedYear < currentYear || adjustedYear > currentYear + 20) return false
	if adjustedOK && (adjustedYear < currentYear || adjustedYear > currentYear+20) {
		return false
	}
	// else if (adjustedYear === currentYear && month < currentMonth) return false
	if adjustedOK && monthOK && adjustedYear == currentYear && month < currentMonth {
		return false
	}
	// else if (month < 1 || month > 12) return false
	if monthOK && (month < 1 || month > 12) {
		return false
	}
	return true
}

// parseBase10 mimics JS parseInt(s, 10): it parses a leading integer, ignoring
// trailing non-digit characters, and reports failure (NaN) when no leading
// digits are present.
func parseBase10(s string) (int, bool) {
	s = strings.TrimLeft(s, " \t")
	i := 0
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return 0, false // NaN
	}
	n, err := strconv.Atoi(s[start:i])
	if err != nil {
		return 0, false
	}
	if neg {
		n = -n
	}
	return n, true
}
