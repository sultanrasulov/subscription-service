package domain

import (
	"github.com/google/uuid"

	"github.com/sultanrasulov/subscription-service/internal/shared/valueobject"
)

// userIDKind tells user IDs apart from every other identifier.
type userIDKind struct{}

// UserID identifies the user a subscription belongs to. Users are not checked
// for existence; the ID is taken as given.
//
// Its kind is its own, so a user ID cannot be passed where a subscription ID is
// expected. The zero value is not a valid user ID; IsZero reports it, and a
// filter uses it to mean "any user".
type UserID = valueobject.ID[userIDKind]

// NewUserID returns the user ID, or ErrNilUserID for the nil UUID.
func NewUserID(id uuid.UUID) (UserID, error) {
	// The nil UUID is the only value NewID rejects.
	u, err := valueobject.NewID[userIDKind](id)
	if err != nil {
		return UserID{}, ErrNilUserID
	}
	return u, nil
}
