// Package event owns the durable outbox: enqueueing commands inside a caller's
// transaction, compare-and-swap claims with fresh unpredictable lease tokens,
// token-checked renewal/completion, and the in-process Dispatcher that runs a
// handler per event type. It must not import payment-domain packages; domain
// handlers import it instead.
package event
