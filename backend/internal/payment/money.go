// Package payment contains exact monetary values and domain validation.
package payment

import (
	"errors"
	"fmt"
	"strconv"
)

// CurrencyExponents is the supported sandbox currency list. Amounts already use
// minor units; exponents govern display, not divisibility of integer minor units.
var CurrencyExponents = map[string]int{"USD": 2, "EUR": 2, "GBP": 2, "JPY": 0, "KWD": 3}

var (
	// ErrUnsupportedCurrency rejects codes outside CurrencyExponents.
	ErrUnsupportedCurrency = errors.New("unsupported currency")
	// ErrInvalidAmount rejects anything other than a canonical positive int64 string.
	ErrInvalidAmount = errors.New("invalid amount")
)

// ValidateCurrency accepts only an exact upper-case code from the domain list
// whose exponent is within the range the display layer supports.
func ValidateCurrency(currency string) error {
	exponent, ok := CurrencyExponents[currency]
	if !ok || exponent < 0 || exponent > 3 {
		return ErrUnsupportedCurrency
	}
	return nil
}

// CurrencyExponent reports the display exponent for a supported currency.
func CurrencyExponent(currency string) (int, bool) {
	exponent, ok := CurrencyExponents[currency]
	return exponent, ok
}

// ParseAmount accepts a canonical positive base-10 int64: digits only, no
// sign, whitespace, leading zero, fraction or exponent, and at most MaxInt64.
func ParseAmount(value string) (int64, error) {
	if value == "" {
		return 0, fmt.Errorf("%w: amount is required", ErrInvalidAmount)
	}
	if value[0] == '0' {
		return 0, fmt.Errorf("%w: amount must be a canonical positive integer string", ErrInvalidAmount)
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("%w: amount must contain decimal digits only", ErrInvalidAmount)
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%w: amount must fit a positive int64", ErrInvalidAmount)
	}
	return n, nil
}

// ParseMinor validates currency, then parses the amount with ParseAmount.
func ParseMinor(value, currency string) (int64, error) {
	if err := ValidateCurrency(currency); err != nil {
		return 0, err
	}
	return ParseAmount(value)
}

// FormatMinor preserves exact payment amounts at the JSON string boundary.
func FormatMinor(amount int64, currency string) (string, error) {
	if amount <= 0 {
		return "", errors.New("amount must be positive")
	}
	if err := ValidateCurrency(currency); err != nil {
		return "", err
	}
	return strconv.FormatInt(amount, 10), nil
}
