package payment

import (
	"errors"
	"fmt"
)

// Status is the logical payment state; flows.md owns the diagram.
type Status string

const (
	StatusCreated    Status = "created"
	StatusProcessing Status = "processing"
	StatusSucceeded  Status = "succeeded"
	StatusFailed     Status = "failed"
	StatusCanceled   Status = "canceled"
	StatusUnknown    Status = "unknown"
)

// Statuses lists the vocabulary in lifecycle order for validation and docs.
var Statuses = []Status{StatusCreated, StatusProcessing, StatusSucceeded, StatusFailed, StatusCanceled, StatusUnknown}

// ErrIllegalTransition reports a transition outside the documented graph.
var ErrIllegalTransition = errors.New("illegal payment status transition")

// legalTransitions is the complete graph. An unknown outcome may only resolve
// to a terminal state with original-provider evidence; it never returns to
// processing, which would invite a second submission.
var legalTransitions = map[Status][]Status{
	StatusCreated:    {StatusProcessing, StatusCanceled},
	StatusProcessing: {StatusSucceeded, StatusFailed, StatusUnknown},
	StatusUnknown:    {StatusSucceeded, StatusFailed},
}

// Valid reports whether s belongs to the status vocabulary.
func (s Status) Valid() bool {
	for _, candidate := range Statuses {
		if s == candidate {
			return true
		}
	}
	return false
}

// Terminal reports whether no further transition is permitted.
func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusCanceled
}

// ParseStatus validates an external status value, such as a list filter.
func ParseStatus(value string) (Status, error) {
	status := Status(value)
	if !status.Valid() {
		return "", fmt.Errorf("unknown payment status %q", value)
	}
	return status, nil
}

// ValidateTransition is pure: it checks the graph only. The empty from status
// represents "no prior state" and is legal only for the initial created audit.
func ValidateTransition(from, to Status) error {
	if !to.Valid() || from != "" && !from.Valid() {
		return fmt.Errorf("%w: %q -> %q", ErrIllegalTransition, from, to)
	}
	if from == "" {
		if to == StatusCreated {
			return nil
		}
		return fmt.Errorf("%w: initial status must be created, got %q", ErrIllegalTransition, to)
	}
	for _, next := range legalTransitions[from] {
		if next == to {
			return nil
		}
	}
	return fmt.Errorf("%w: %q -> %q", ErrIllegalTransition, from, to)
}
