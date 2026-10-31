package provider

import (
	"context"
	"errors"
	"testing"
)

func TestSubmitRequestValidate(t *testing.T) {
	valid := SubmitRequest{WorkspaceID: "w", ProviderID: "p", IdempotencyKey: "k", AmountMinor: 1, Currency: "USD", TokenReference: "t"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*SubmitRequest){
		"workspace":      func(r *SubmitRequest) { r.WorkspaceID = "" },
		"provider":       func(r *SubmitRequest) { r.ProviderID = "" },
		"key":            func(r *SubmitRequest) { r.IdempotencyKey = "" },
		"zero amount":    func(r *SubmitRequest) { r.AmountMinor = 0 },
		"negative":       func(r *SubmitRequest) { r.AmountMinor = -5 },
		"lower currency": func(r *SubmitRequest) { r.Currency = "usd" },
		"long currency":  func(r *SubmitRequest) { r.Currency = "USDT" },
		"token":          func(r *SubmitRequest) { r.TokenReference = "" },
	}
	for name, mutate := range cases {
		r := valid
		mutate(&r)
		if err := r.Validate(); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: want ErrInvalidRequest, got %v", name, err)
		}
	}
}

func TestContextCode(t *testing.T) {
	if ContextCode(context.DeadlineExceeded) != CodeDeadlineExceeded || ContextCode(context.Canceled) != CodeTimeout {
		t.Fatal("unexpected context code mapping")
	}
	if r := Ambiguous(CodeTimeout, context.Canceled); r.Outcome != OutcomeAmbiguous || r.Code != CodeTimeout {
		t.Fatalf("%+v", r)
	}
}
