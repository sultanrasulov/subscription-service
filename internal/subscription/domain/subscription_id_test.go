package domain_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

func TestNewSubscriptionID_Accepts(t *testing.T) {
	t.Parallel()

	for name, id := range map[string]uuid.UUID{
		"version 4": uuid.MustParse("60601fee-2bf1-4721-ae6f-7636e79a0cba"),
		"version 7": uuid.MustParse("017f22e2-79b0-7cc3-98c4-dc0c0c07398f"),
		"max":       uuid.Max,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s, err := domain.NewSubscriptionID(id)
			if err != nil {
				t.Fatalf("NewSubscriptionID(%s) error = %v", id, err)
			}
			if s.UUID() != id {
				t.Errorf("NewSubscriptionID(%s).UUID() = %s", id, s.UUID())
			}
			if s.IsZero() {
				t.Errorf("NewSubscriptionID(%s).IsZero() = true, want false", id)
			}
		})
	}
}

func TestNewSubscriptionID_RejectsNil(t *testing.T) {
	t.Parallel()

	s, err := domain.NewSubscriptionID(uuid.Nil)
	if !errors.Is(err, domain.ErrNilSubscriptionID) {
		t.Fatalf("NewSubscriptionID(uuid.Nil) error = %v, want %v", err, domain.ErrNilSubscriptionID)
	}
	if !s.IsZero() {
		t.Errorf("NewSubscriptionID(uuid.Nil) = %+v with an error, want the zero value", s)
	}
}

// Each identifier has a kind of its own. Declaring a new identifier with the
// marker of an existing one would compile, and would let one ID be passed as
// the other; this catches that copy-and-paste slip.
func TestIdentifiers_HaveDistinctKinds(t *testing.T) {
	t.Parallel()

	if reflect.TypeFor[domain.UserID]() == reflect.TypeFor[domain.SubscriptionID]() {
		t.Error("UserID and SubscriptionID are the same type")
	}
}

func mustSubscriptionID(t *testing.T, id uuid.UUID) domain.SubscriptionID {
	t.Helper()

	s, err := domain.NewSubscriptionID(id)
	if err != nil {
		t.Fatalf("NewSubscriptionID(%s) error = %v", id, err)
	}
	return s
}
