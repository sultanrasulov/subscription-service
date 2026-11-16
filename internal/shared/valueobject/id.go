package valueobject

import (
	"errors"

	"github.com/google/uuid"
)

// ErrNilID reports the nil UUID given as an identifier.
var ErrNilID = errors.New("id must not be the nil UUID")

// ID identifies an entity of kind K.
//
// K is a marker type that only tells identifiers apart: an ID of one kind
// cannot be used where another kind is expected, so a user ID cannot be passed
// as a subscription ID. The package that owns a kind keeps its marker
// unexported and names the identifier with an alias, for example:
//
//	type userIDKind struct{}
//
//	type UserID = valueobject.ID[userIDKind]
//
// The zero value is not a valid identifier; IsZero reports it. IDs compare
// equal with == when they have the same kind and the same UUID.
type ID[K any] struct {
	value uuid.UUID
}

// NewID returns the identifier of kind K for value, or ErrNilID for the nil
// UUID.
//
// It checks the value itself instead of leaving that to the package that owns
// K: Go can infer K from an assignment, so code elsewhere can still call NewID
// for that kind, and the check keeps even that path from making a nil
// identifier.
func NewID[K any](value uuid.UUID) (ID[K], error) {
	if value == uuid.Nil {
		return ID[K]{}, ErrNilID
	}
	return ID[K]{value: value}, nil
}

// UUID returns the identifier as a UUID.
func (id ID[K]) UUID() uuid.UUID { return id.value }

// IsZero reports whether id is the zero value, which is not a valid
// identifier.
func (id ID[K]) IsZero() bool { return id.value == uuid.Nil }
