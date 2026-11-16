package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

func TestNewServiceName_Normalizes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"already normal", "Yandex Plus", "Yandex Plus"},
		{"leading and trailing spaces", "  Yandex Plus  ", "Yandex Plus"},
		{"repeated spaces", "Yandex   Plus", "Yandex Plus"},
		{"tab", "Yandex\tPlus", "Yandex Plus"},
		{"newlines", "\nYandex\r\nPlus\n", "Yandex Plus"},
		{"no-break space U+00A0", "Yandex\u00a0Plus", "Yandex Plus"},
		{"ideographic space U+3000", "Yandex\u3000Plus", "Yandex Plus"},
		{"next line U+0085", "Yandex\u0085Plus", "Yandex Plus"},
		{"case is preserved", "yAnDeX pLuS", "yAnDeX pLuS"},
		{"Cyrillic", "  Яндекс   Плюс ", "Яндекс Плюс"},
		// A format character, neither whitespace nor control.
		{"zero-width space U+200B is kept", "Yandex\u200bPlus", "Yandex\u200bPlus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			n, err := domain.NewServiceName(tc.raw)
			if err != nil {
				t.Fatalf("NewServiceName(%q) error = %v", tc.raw, err)
			}
			if n.String() != tc.want {
				t.Errorf("NewServiceName(%q) = %q, want %q", tc.raw, n.String(), tc.want)
			}
			if n.IsZero() {
				t.Errorf("NewServiceName(%q).IsZero() = true, want false", tc.raw)
			}
		})
	}
}

// The length is counted in characters (runes), not bytes, after normalization.
func TestNewServiceName_Length(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
		want error
	}{
		{"100 ASCII characters", strings.Repeat("a", 100), nil},
		{"101 ASCII characters", strings.Repeat("a", 101), domain.ErrServiceNameTooLong},
		{"100 Cyrillic characters, 200 bytes", strings.Repeat("я", 100), nil},
		{"101 Cyrillic characters", strings.Repeat("я", 101), domain.ErrServiceNameTooLong},
		{"100 characters after trimming", "  " + strings.Repeat("a", 100) + "  ", nil},
		{"100 characters after collapsing", strings.Repeat("a", 49) + "     " + strings.Repeat("b", 50), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			n, err := domain.NewServiceName(tc.raw)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewServiceName(%d bytes) error = %v, want %v", len(tc.raw), err, tc.want)
			}
			if err != nil && n != (domain.ServiceName{}) {
				t.Errorf("NewServiceName(%d bytes) = %q with an error, want the zero value", len(tc.raw), n.String())
			}
		})
	}
}

func TestNewServiceName_Rejects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
		want error
	}{
		{"empty", "", domain.ErrEmptyServiceName},
		{"whitespace only", " \t\n\u00a0\u3000", domain.ErrEmptyServiceName},
		{"NUL", "Yandex\x00Plus", domain.ErrServiceNameInvalidCharacters},
		{"escape", "Yandex\x1bPlus", domain.ErrServiceNameInvalidCharacters},
		{"delete", "Yandex\x7fPlus", domain.ErrServiceNameInvalidCharacters},
		{"C1 control U+009B", "Yandex\u009bPlus", domain.ErrServiceNameInvalidCharacters},
		{"invalid UTF-8 byte", "Yandex\xffPlus", domain.ErrServiceNameInvalidCharacters},
		{"truncated UTF-8 sequence", "Яндекс\xd0", domain.ErrServiceNameInvalidCharacters},
		{"invalid characters before length", strings.Repeat("a", 101) + "\x00", domain.ErrServiceNameInvalidCharacters},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			n, err := domain.NewServiceName(tc.raw)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewServiceName(%q) error = %v, want %v", tc.raw, err, tc.want)
			}
			if n != (domain.ServiceName{}) {
				t.Errorf("NewServiceName(%q) = %q with an error, want the zero value", tc.raw, n.String())
			}
		})
	}
}

func TestServiceName_ZeroValueIsZero(t *testing.T) {
	t.Parallel()

	if !(domain.ServiceName{}).IsZero() {
		t.Error("ServiceName{}.IsZero() = false, want true")
	}
}
