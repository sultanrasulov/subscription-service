package domain_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// mustMonthYear builds a month the test relies on being valid.
func mustMonthYear(t *testing.T, year int, month time.Month) domain.MonthYear {
	t.Helper()

	m, err := domain.NewMonthYear(year, month)
	if err != nil {
		t.Fatalf("NewMonthYear(%d, %d) error = %v", year, month, err)
	}
	return m
}

func TestNewMonthYear_Accepts(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		year  int
		month time.Month
	}{
		{2000, time.January},
		{2025, time.July},
		{2100, time.December},
	} {
		t.Run(fmt.Sprintf("%02d-%d", tc.month, tc.year), func(t *testing.T) {
			t.Parallel()

			m, err := domain.NewMonthYear(tc.year, tc.month)
			if err != nil {
				t.Fatalf("NewMonthYear() error = %v", err)
			}
			if m.Year() != tc.year || m.Month() != tc.month {
				t.Errorf("NewMonthYear() = %d-%d, want %d-%d", m.Year(), m.Month(), tc.year, tc.month)
			}
		})
	}
}

func TestNewMonthYear_Rejects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		year  int
		month time.Month
		want  error
	}{
		{"month -1", 2025, -1, domain.ErrInvalidMonth},
		{"month 0", 2025, 0, domain.ErrInvalidMonth},
		{"month 13", 2025, 13, domain.ErrInvalidMonth},
		{"year 1999", 1999, time.December, domain.ErrYearOutOfRange},
		{"year 2101", 2101, time.January, domain.ErrYearOutOfRange},
		{"year 0", 0, time.January, domain.ErrYearOutOfRange},
		// The month is checked first, so the zero value reports the month.
		{"month and year", 0, 0, domain.ErrInvalidMonth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m, err := domain.NewMonthYear(tc.year, tc.month)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewMonthYear(%d, %d) error = %v, want %v", tc.year, tc.month, err, tc.want)
			}
			if m != (domain.MonthYear{}) {
				t.Errorf("NewMonthYear(%d, %d) = %+v with an error, want the zero value", tc.year, tc.month, m)
			}
		})
	}
}

func TestMonthYear_Before(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		a, b domain.MonthYear
		want bool
	}{
		{"across a year boundary", mustMonthYear(t, 2024, time.December), mustMonthYear(t, 2025, time.January), true},
		{"across a year boundary, reversed", mustMonthYear(t, 2025, time.January), mustMonthYear(t, 2024, time.December), false},
		{"within a year", mustMonthYear(t, 2025, time.January), mustMonthYear(t, 2025, time.February), true},
		{"within a year, reversed", mustMonthYear(t, 2025, time.February), mustMonthYear(t, 2025, time.January), false},
		{"equal", mustMonthYear(t, 2025, time.July), mustMonthYear(t, 2025, time.July), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.a.Before(tc.b); got != tc.want {
				t.Errorf("%02d-%d.Before(%02d-%d) = %v, want %v",
					tc.a.Month(), tc.a.Year(), tc.b.Month(), tc.b.Year(), got, tc.want)
			}
		})
	}
}

func TestMonthYear_ComparesByValue(t *testing.T) {
	t.Parallel()

	july, sameJuly := mustMonthYear(t, 2025, time.July), mustMonthYear(t, 2025, time.July)
	if july != sameJuly {
		t.Error("two 07-2025 values are not equal")
	}
	if july == mustMonthYear(t, 2025, time.August) {
		t.Error("07-2025 equals 08-2025")
	}
}
