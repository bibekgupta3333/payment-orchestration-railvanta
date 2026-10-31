package payment

import "testing"

func TestParseMinor(t *testing.T) {
	for _, value := range []string{"9223372036854775808", "1.5", "-1", "0", " 1", "+1", "", "01", "1e2"} {
		if _, err := ParseMinor(value, "USD"); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	for _, currency := range []string{"USD", "JPY", "KWD"} {
		n, err := ParseMinor("9223372036854775807", currency)
		if err != nil || n != 9223372036854775807 {
			t.Errorf("roundtrip %s: %d %v", currency, n, err)
		}
		serialized, err := FormatMinor(n, currency)
		if err != nil || serialized != "9223372036854775807" {
			t.Errorf("serialization lost precision: %q %v", serialized, err)
		}
	}
	if _, err := ParseMinor("1", "usd"); err == nil {
		t.Fatal("accepted unsupported currency")
	}
	if _, err := FormatMinor(0, "USD"); err == nil {
		t.Fatal("serialized a zero payment")
	}
	if _, err := FormatMinor(1, "usd"); err == nil {
		t.Fatal("serialized an unsupported currency")
	}
}
