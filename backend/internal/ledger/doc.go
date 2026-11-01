// Package ledger posts balanced, immutable double-entry journals inside the
// caller's database transaction, creates reversal journals, and verifies ledger
// invariants. It never opens or commits its own transaction: callers combine a
// posting with the payment transition, audit record and outbox completion and
// roll everything back when any step returns an error.
package ledger
