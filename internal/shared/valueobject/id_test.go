package valueobject_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/sultanrasulov/subscription-service/internal/shared/valueobject"
)

// testKind is the kind of the identifiers these tests build.
type testKind struct{}

type testID = valueobject.ID[testKind]

func TestNewID_Accepts(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]uuid.UUID{
		"version 4": uuid.MustParse("60601fee-2bf1-4721-ae6f-7636e79a0cba"),
		"version 7": uuid.MustParse("017f22e2-79b0-7cc3-98c4-dc0c0c07398f"),
		"max":       uuid.Max,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			id, err := valueobject.NewID[testKind](value)
			if err != nil {
				t.Fatalf("NewID(%s) error = %v", value, err)
			}
			if id.UUID() != value {
				t.Errorf("NewID(%s).UUID() = %s", value, id.UUID())
			}
			if id.IsZero() {
				t.Errorf("NewID(%s).IsZero() = true, want false", value)
			}
		})
	}
}

func TestNewID_RejectsNil(t *testing.T) {
	t.Parallel()

	id, err := valueobject.NewID[testKind](uuid.Nil)
	if !errors.Is(err, valueobject.ErrNilID) {
		t.Fatalf("NewID(uuid.Nil) error = %v, want %v", err, valueobject.ErrNilID)
	}
	if id != (testID{}) {
		t.Errorf("NewID(uuid.Nil) = %+v with an error, want the zero value", id)
	}
}

func TestID_ZeroValueIsZero(t *testing.T) {
	t.Parallel()

	if !(testID{}).IsZero() {
		t.Error("ID{}.IsZero() = false, want true")
	}
}

func TestID_ComparesByValue(t *testing.T) {
	t.Parallel()

	value := uuid.MustParse("60601fee-2bf1-4721-ae6f-7636e79a0cba")
	a, errA := valueobject.NewID[testKind](value)
	b, errB := valueobject.NewID[testKind](value)
	other, errOther := valueobject.NewID[testKind](uuid.Max)
	if err := errors.Join(errA, errB, errOther); err != nil {
		t.Fatalf("NewID() error = %v", err)
	}

	if a != b {
		t.Error("two IDs of the same UUID are not equal")
	}
	if a == other {
		t.Error("IDs of different UUIDs are equal")
	}
}
