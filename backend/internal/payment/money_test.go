package payment

import (
	"errors"
	"testing"
)

func TestParseMinor(t *testing.T) {
	for _, value := range []string{"9223372036854775808", "99999999999999999999", "1.5", "1.0", "-1", "0", "00", " 1", "1 ", "+1", "", "01", "1e2", "0x10", "１"} {
		if _, err := ParseMinor(value, "USD"); !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("accepted %q (%v)", value, err)
		}
	}
	for _, currency := range []string{"USD", "EUR", "GBP", "JPY", "KWD"} {
		n, err := ParseMinor("9223372036854775807", currency)
		if err != nil || n != 9223372036854775807 {
			t.Errorf("roundtrip %s: %d %v", currency, n, err)
		}
		serialized, err := FormatMinor(n, currency)
		if err != nil || serialized != "9223372036854775807" {
			t.Errorf("serialization lost precision: %q %v", serialized, err)
		}
	}
	if n, err := ParseAmount("9007199254740993"); err != nil || n != 9007199254740993 {
		t.Fatalf("above MAX_SAFE_INTEGER: %d %v", n, err)
	}
	for _, currency := range []string{"usd", "US", "USDX", "", "XXX"} {
		if _, err := ParseMinor("1", currency); !errors.Is(err, ErrUnsupportedCurrency) {
			t.Errorf("accepted currency %q", currency)
		}
	}
	if exponent, ok := CurrencyExponent("KWD"); !ok || exponent != 3 {
		t.Fatalf("KWD exponent %d %v", exponent, ok)
	}
	if _, err := FormatMinor(0, "USD"); err == nil {
		t.Fatal("serialized a zero payment")
	}
	if _, err := FormatMinor(1, "usd"); err == nil {
		t.Fatal("serialized an unsupported currency")
	}
}
