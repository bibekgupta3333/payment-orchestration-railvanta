// Package payment contains exact monetary values and domain validation.
package payment

import (
	"errors"
	"strconv"
)

// CurrencyExponents is the supported sandbox currency list. Amounts already use
// minor units; exponents govern display, not divisibility of integer minor units.
var CurrencyExponents = map[string]int{"USD": 2, "EUR": 2, "GBP": 2, "JPY": 0, "KWD": 3}

// ParseMinor accepts a canonical positive base-10 int64 without signs or spaces.
func ParseMinor(value, currency string) (int64, error) {
	if _, ok := CurrencyExponents[currency]; !ok {
		return 0, errors.New("unsupported currency")
	}
	if value == "" || value[0] == '0' {
		return 0, errors.New("amount must be a canonical positive integer string")
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, errors.New("amount must contain decimal digits only")
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return 0, errors.New("amount must fit a positive int64")
	}
	return n, nil
}

// FormatMinor preserves exact payment amounts at the JSON string boundary.
func FormatMinor(amount int64, currency string) (string, error) {
	if amount <= 0 {
		return "", errors.New("amount must be positive")
	}
	if _, ok := CurrencyExponents[currency]; !ok {
		return "", errors.New("unsupported currency")
	}
	return strconv.FormatInt(amount, 10), nil
}
