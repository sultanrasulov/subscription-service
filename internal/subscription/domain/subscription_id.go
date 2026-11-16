package domain

import (
	"github.com/google/uuid"

	"github.com/sultanrasulov/subscription-service/internal/shared/valueobject"
)

// subscriptionIDKind tells subscription IDs apart from every other identifier.
type subscriptionIDKind struct{}

// SubscriptionID identifies a Subscription. The caller generates it; the
// domain never does, because it does not read the clock.
//
// Its kind is its own, so a subscription ID cannot be passed where a user ID is
// expected. The zero value is not a valid subscription ID.
type SubscriptionID = valueobject.ID[subscriptionIDKind]

// NewSubscriptionID returns the subscription ID, or ErrNilSubscriptionID for
// the nil UUID.
func NewSubscriptionID(id uuid.UUID) (SubscriptionID, error) {
	// The nil UUID is the only value NewID rejects.
	s, err := valueobject.NewID[subscriptionIDKind](id)
	if err != nil {
		return SubscriptionID{}, ErrNilSubscriptionID
	}
	return s, nil
}
