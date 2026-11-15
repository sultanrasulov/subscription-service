package domain

import "github.com/google/uuid"

// UserID identifies the user a subscription belongs to. Users are not checked
// for existence; the ID is taken as given.
//
// It is a type of its own rather than a bare UUID, so a user ID cannot be
// passed where a subscription ID is expected. The zero value is not a valid
// user ID; IsZero reports it.
type UserID struct {
	id uuid.UUID
}

// NewUserID returns the user ID, or ErrNilUserID for the nil UUID.
func NewUserID(id uuid.UUID) (UserID, error) {
	if id == uuid.Nil {
		return UserID{}, ErrNilUserID
	}
	return UserID{id: id}, nil
}

// UUID returns the user ID as a UUID.
func (u UserID) UUID() uuid.UUID { return u.id }

// IsZero reports whether u is the zero value, which is not a valid user ID.
// A filter uses it to mean "any user".
func (u UserID) IsZero() bool { return u.id == uuid.Nil }
