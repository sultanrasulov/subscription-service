// Package domain is the subscription model: its value objects, the
// Subscription aggregate, the queries, and the ports that adapters implement.
//
// The invariants and validation rules live here, so they can be tested
// without any infrastructure. The one exception is the total cost: the
// database computes it, and the domain only defines the port that reads it.
// The package depends only on the standard library and a UUID library. It
// knows nothing about HTTP, JSON or SQL, never reads the clock and never logs.
package domain
