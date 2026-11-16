// Package domaintest holds in-memory fakes of the domain's ports, for the
// tests of the layers above the domain. They keep the ports' contracts the
// way the database adapter does, so a test over a fake means the same as one
// over the database.
package domaintest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// Repository is an in-memory domain.Repository. It is safe for concurrent
// use. It stores subscriptions by value, so nothing a caller holds can change
// what it stores.
type Repository struct {
	mu            sync.Mutex
	subscriptions map[domain.SubscriptionID]domain.Subscription
	err           error
	blocked       bool
}

// The fake implements the whole port.
var _ domain.Repository = (*Repository)(nil)

// NewRepository returns an empty repository.
func NewRepository() *Repository {
	return &Repository{subscriptions: make(map[domain.SubscriptionID]domain.Subscription)}
}

// Fail makes every later call return err, as a failing database would.
func (r *Repository) Fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

// Block makes every later call wait until its context ends and return the
// context's error, as a hung query would.
func (r *Repository) Block() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blocked = true
}

// begin applies Fail and Block, and otherwise locks the repository for the
// call; the caller unlocks it.
func (r *Repository) begin(ctx context.Context) error {
	r.mu.Lock()
	blocked, err := r.blocked, r.err
	if blocked || err != nil {
		r.mu.Unlock()
	}
	if blocked {
		<-ctx.Done()
		return ctx.Err()
	}
	return err
}

// Create stores a new subscription. A second subscription with the same ID is
// an error, as the database's primary key makes it.
func (r *Repository) Create(ctx context.Context, s domain.Subscription) error {
	if err := r.begin(ctx); err != nil {
		return err
	}
	defer r.mu.Unlock()

	if _, ok := r.subscriptions[s.ID()]; ok {
		return fmt.Errorf("create subscription %s: %w", s.ID().UUID(), errDuplicateID)
	}
	r.subscriptions[s.ID()] = s
	return nil
}

var errDuplicateID = errors.New("a subscription with this ID exists")

// Get returns the subscription with the given ID.
func (r *Repository) Get(ctx context.Context, id domain.SubscriptionID) (domain.Subscription, error) {
	if err := r.begin(ctx); err != nil {
		return domain.Subscription{}, err
	}
	defer r.mu.Unlock()

	s, ok := r.subscriptions[id]
	if !ok {
		return domain.Subscription{}, fmt.Errorf("get subscription %s: %w", id.UUID(), domain.ErrNotFound)
	}
	return s, nil
}

// Update stores s over the subscription with the same ID. Like the database
// adapter, it keeps the stored user: the user of a subscription never
// changes.
func (r *Repository) Update(ctx context.Context, s domain.Subscription) error {
	if err := r.begin(ctx); err != nil {
		return err
	}
	defer r.mu.Unlock()

	stored, ok := r.subscriptions[s.ID()]
	if !ok {
		return fmt.Errorf("update subscription %s: %w", s.ID().UUID(), domain.ErrNotFound)
	}
	updated, err := domain.NewSubscription(s.ID(), stored.UserID(), s.ServiceName(), s.Price(), s.Period())
	if err != nil {
		return fmt.Errorf("update subscription %s: %w", s.ID().UUID(), err)
	}
	r.subscriptions[s.ID()] = updated
	return nil
}

// Delete removes the subscription with the given ID and returns it.
func (r *Repository) Delete(ctx context.Context, id domain.SubscriptionID) (domain.Subscription, error) {
	if err := r.begin(ctx); err != nil {
		return domain.Subscription{}, err
	}
	defer r.mu.Unlock()

	s, ok := r.subscriptions[id]
	if !ok {
		return domain.Subscription{}, fmt.Errorf("delete subscription %s: %w", id.UUID(), domain.ErrNotFound)
	}
	delete(r.subscriptions, id)
	return s, nil
}

// List returns the page of subscriptions that match the query's filter,
// ordered by ID byte by byte as the database orders UUIDs, and the number of
// all of them. Names match with strings.EqualFold, which is close to, but not
// the same as, the database's lower() under pg_c_utf8; the database adapter's
// own tests cover that.
func (r *Repository) List(ctx context.Context, q domain.ListQuery) ([]domain.Subscription, int, error) {
	if err := r.begin(ctx); err != nil {
		return nil, 0, err
	}
	defer r.mu.Unlock()

	// The database rejects these with an error, so the fake does too.
	if q.Limit < 0 || q.Offset < 0 {
		return nil, 0, fmt.Errorf("list subscriptions: limit %d and offset %d must not be negative", q.Limit, q.Offset)
	}

	var matches []domain.Subscription
	for _, s := range r.subscriptions {
		if matchesFilter(s, q.Filter) {
			matches = append(matches, s)
		}
	}
	slices.SortFunc(matches, func(a, b domain.Subscription) int {
		ida, idb := a.ID().UUID(), b.ID().UUID()
		return bytes.Compare(ida[:], idb[:])
	})

	start := min(q.Offset, len(matches))
	end := min(start+q.Limit, len(matches))
	return append([]domain.Subscription{}, matches[start:end]...), len(matches), nil
}

func matchesFilter(s domain.Subscription, f domain.Filter) bool {
	if !f.UserID.IsZero() && s.UserID() != f.UserID {
		return false
	}
	if !f.ServiceName.IsZero() && !strings.EqualFold(s.ServiceName().String(), f.ServiceName.String()) {
		return false
	}
	return true
}
