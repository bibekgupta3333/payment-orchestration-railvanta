package payment

import (
	"errors"
	"testing"
)

func TestValidateTransitionCoversEveryPair(t *testing.T) {
	legal := map[[2]Status]bool{
		{"", StatusCreated}:                 true,
		{StatusCreated, StatusProcessing}:   true,
		{StatusCreated, StatusCanceled}:     true,
		{StatusProcessing, StatusSucceeded}: true,
		{StatusProcessing, StatusFailed}:    true,
		{StatusProcessing, StatusUnknown}:   true,
		{StatusUnknown, StatusSucceeded}:    true,
		{StatusUnknown, StatusFailed}:       true,
	}
	froms := append([]Status{""}, Statuses...)
	tested := 0
	for _, from := range froms {
		for _, to := range Statuses {
			tested++
			err := ValidateTransition(from, to)
			if want := legal[[2]Status{from, to}]; want != (err == nil) {
				t.Errorf("%q -> %q: legal=%v err=%v", from, to, want, err)
			}
			if err != nil && !errors.Is(err, ErrIllegalTransition) {
				t.Errorf("%q -> %q: wrong error type %v", from, to, err)
			}
		}
	}
	if tested != 42 {
		t.Fatalf("expected 7x6 pairs, tested %d", tested)
	}
}

func TestValidateTransitionRejectsUnknownVocabulary(t *testing.T) {
	cases := []struct {
		name     string
		from, to Status
	}{
		{"bad target", StatusCreated, "settled"},
		{"bad source", "pending", StatusProcessing},
		{"empty target", StatusCreated, ""},
		{"unknown never retries", StatusUnknown, StatusProcessing},
		{"terminal success", StatusSucceeded, StatusFailed},
		{"terminal failure", StatusFailed, StatusSucceeded},
		{"no self loop", StatusCreated, StatusCreated},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateTransition(c.from, c.to); !errors.Is(err, ErrIllegalTransition) {
				t.Fatalf("accepted %q -> %q", c.from, c.to)
			}
		})
	}
}

func TestStatusHelpers(t *testing.T) {
	for _, status := range Statuses {
		parsed, err := ParseStatus(string(status))
		if err != nil || parsed != status {
			t.Errorf("parse %q: %v", status, err)
		}
		wantTerminal := status == StatusSucceeded || status == StatusFailed || status == StatusCanceled
		if status.Terminal() != wantTerminal {
			t.Errorf("terminal %q", status)
		}
		if status.Terminal() && len(legalTransitions[status]) != 0 {
			t.Errorf("terminal %q has outgoing transitions", status)
		}
	}
	for _, value := range []string{"", "CREATED", "pending"} {
		if _, err := ParseStatus(value); err == nil {
			t.Errorf("parsed %q", value)
		}
	}
}
