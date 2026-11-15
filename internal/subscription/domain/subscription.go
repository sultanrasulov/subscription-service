package domain

import "github.com/google/uuid"

// Subscription is one record of a user paying a monthly price for a service
// during a subscription period. It is the only aggregate: its state changes
// only through NewSubscription and Update.
//
// It is passed by value. Every field is a value, so a copy, such as one a
// repository returns, never shares state with the original, and there is no
// nil subscription. The methods use pointer receivers because Update changes
// the subscription.
type Subscription struct {
	id     uuid.UUID
	userID UserID
	name   ServiceName
	price  Price
	period SubscriptionPeriod
}

// NewSubscription returns a subscription with the given fields. The caller
// supplies the ID. The same constructor loads a stored subscription, so a
// stored row that breaks an invariant fails here instead of loading.
//
// The value objects are valid when they come from their constructors; only
// their zero values can be invalid. The fields are checked in order, and the
// first invalid one is reported with the error its constructor gives the
// equivalent input: ErrNilSubscriptionID, ErrNilUserID, ErrEmptyServiceName or
// ErrInvalidMonth. A zero price is valid.
func NewSubscription(id uuid.UUID, userID UserID, name ServiceName, price Price, period SubscriptionPeriod) (Subscription, error) {
	if id == uuid.Nil {
		return Subscription{}, ErrNilSubscriptionID
	}
	if userID.IsZero() {
		return Subscription{}, ErrNilUserID
	}
	if err := checkNameAndPeriod(name, period); err != nil {
		return Subscription{}, err
	}
	return Subscription{id: id, userID: userID, name: name, price: price, period: period}, nil
}

// ID returns the subscription's ID.
func (s *Subscription) ID() uuid.UUID { return s.id }

// UserID returns the user the subscription belongs to.
func (s *Subscription) UserID() UserID { return s.userID }

// ServiceName returns the name of the subscribed service.
func (s *Subscription) ServiceName() ServiceName { return s.name }

// Price returns the monthly price.
func (s *Subscription) Price() Price { return s.price }

// Period returns when the subscription is active.
func (s *Subscription) Period() SubscriptionPeriod { return s.period }

// Update replaces the service name, the price and the period: a full
// replacement, so an open-ended period makes the subscription open-ended. The
// ID and the user never change.
//
// It rejects the same zero values as NewSubscription, and it checks every
// argument before it changes anything, so a rejected update leaves the
// subscription as it was.
func (s *Subscription) Update(name ServiceName, price Price, period SubscriptionPeriod) error {
	if err := checkNameAndPeriod(name, period); err != nil {
		return err
	}
	s.name, s.price, s.period = name, price, period
	return nil
}

// checkNameAndPeriod rejects an invalid zero value among the fields Update
// replaces. The price has none: a zero price is valid.
func checkNameAndPeriod(name ServiceName, period SubscriptionPeriod) error {
	if name.IsZero() {
		return ErrEmptyServiceName
	}
	if period == (SubscriptionPeriod{}) {
		return ErrInvalidMonth
	}
	return nil
}
