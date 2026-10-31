package payment

import (
	"regexp"
	"testing"
)

func TestCanonicalRequestHash(t *testing.T) {
	base := CanonicalRequest{Operation: OperationCreatePayment, AmountMinor: 25000, Currency: "USD", PaymentMethodID: "method-1", MerchantReference: "order-1"}
	hash := base.Hash()
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(hash) {
		t.Fatalf("hash format %q", hash)
	}
	if base.Hash() != hash {
		t.Fatal("hash is not deterministic")
	}
	if want := "24:railvanta.idempotency.v1\n14:payment.create\n5:25000\n3:USD\n8:method-1\n7:order-1\n"; base.Canonical() != want {
		t.Fatalf("canonical form changed:\n%q", base.Canonical())
	}
	for name, mutate := range map[string]func(*CanonicalRequest){
		"operation": func(r *CanonicalRequest) { r.Operation = "payment.refund" },
		"amount":    func(r *CanonicalRequest) { r.AmountMinor = 25001 },
		"currency":  func(r *CanonicalRequest) { r.Currency = "EUR" },
		"method":    func(r *CanonicalRequest) { r.PaymentMethodID = "method-2" },
		"reference": func(r *CanonicalRequest) { r.MerchantReference = "order-2" },
		"spacing":   func(r *CanonicalRequest) { r.MerchantReference = "order-1 " },
		"absent":    func(r *CanonicalRequest) { r.MerchantReference = "" },
		// Boundary shifting between adjacent fields must not collide.
		"boundary": func(r *CanonicalRequest) { r.PaymentMethodID = "method-1\n7:order-1"; r.MerchantReference = "" },
	} {
		changed := base
		mutate(&changed)
		if changed.Hash() == hash {
			t.Errorf("%s change did not alter the hash", name)
		}
	}
}
