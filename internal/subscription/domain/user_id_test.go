package domain_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

func TestNewUserID_Accepts(t *testing.T) {
	t.Parallel()

	for name, id := range map[string]uuid.UUID{
		"version 4": uuid.MustParse("60601fee-2bf1-4721-ae6f-7636e79a0cba"),
		"version 7": uuid.MustParse("017f22e2-79b0-7cc3-98c4-dc0c0c07398f"),
		"max":       uuid.Max,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			u, err := domain.NewUserID(id)
			if err != nil {
				t.Fatalf("NewUserID(%s) error = %v", id, err)
			}
			if u.UUID() != id {
				t.Errorf("NewUserID(%s).UUID() = %s", id, u.UUID())
			}
			if u.IsZero() {
				t.Errorf("NewUserID(%s).IsZero() = true, want false", id)
			}
		})
	}
}

func TestNewUserID_RejectsNil(t *testing.T) {
	t.Parallel()

	u, err := domain.NewUserID(uuid.Nil)
	if !errors.Is(err, domain.ErrNilUserID) {
		t.Fatalf("NewUserID(uuid.Nil) error = %v, want %v", err, domain.ErrNilUserID)
	}
	if !u.IsZero() {
		t.Errorf("NewUserID(uuid.Nil) = %+v with an error, want the zero value", u)
	}
}

func TestUserID_ZeroValueIsZero(t *testing.T) {
	t.Parallel()

	if !(domain.UserID{}).IsZero() {
		t.Error("UserID{}.IsZero() = false, want true")
	}
}
