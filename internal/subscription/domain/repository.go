package domain

import "context"

// Repository stores and loads subscriptions. It is the only way to store or
// load one; the database adapter implements it.
//
// Every method returning a Subscription builds it with NewSubscription, so a
// stored record that breaks an invariant is an error rather than a loaded
// subscription.
type Repository interface {
	// Create stores a new subscription.
	Create(ctx context.Context, s Subscription) error

	// Get returns the subscription with the given ID, or an error that wraps
	// ErrNotFound if there is none.
	Get(ctx context.Context, id SubscriptionID) (Subscription, error)

	// Update stores s over the subscription with the same ID, or returns an
	// error that wraps ErrNotFound if there is none.
	Update(ctx context.Context, s Subscription) error

	// Delete removes the subscription with the given ID, or returns an error
	// that wraps ErrNotFound if there is none.
	Delete(ctx context.Context, id SubscriptionID) error

	// List returns the requested page of the subscriptions that match the
	// query's filter, ordered by ID, and total, the number of all of them
	// regardless of the page.
	List(ctx context.Context, q ListQuery) (items []Subscription, total int, err error)
}
