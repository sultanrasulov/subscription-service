package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

func TestNewSubscriptionPeriod_Accepts(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		start, end domain.MonthYear
	}{
		{"several months", mustMonthYear(t, 2025, time.March), mustMonthYear(t, 2025, time.May)},
		{"across a year boundary", mustMonthYear(t, 2024, time.October), mustMonthYear(t, 2025, time.February)},
		{"one month", mustMonthYear(t, 2025, time.July), mustMonthYear(t, 2025, time.July)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p, err := domain.NewSubscriptionPeriod(tc.start, tc.end)
			if err != nil {
				t.Fatalf("NewSubscriptionPeriod() error = %v", err)
			}
			if p.Start() != tc.start {
				t.Errorf("Start() = %+v, want %+v", p.Start(), tc.start)
			}
			if end, ok := p.End(); !ok || end != tc.end {
				t.Errorf("End() = %+v, %v, want %+v, true", end, ok, tc.end)
			}
		})
	}
}

func TestNewSubscriptionPeriod_Rejects(t *testing.T) {
	t.Parallel()

	july := mustMonthYear(t, 2025, time.July)
	for _, tc := range []struct {
		name       string
		start, end domain.MonthYear
		want       error
	}{
		{"end before start", july, mustMonthYear(t, 2025, time.June), domain.ErrEndBeforeStart},
		{"end before start across a year boundary", mustMonthYear(t, 2025, time.January), mustMonthYear(t, 2024, time.December), domain.ErrEndBeforeStart},
		{"zero start", domain.MonthYear{}, july, domain.ErrInvalidMonth},
		{"zero end", july, domain.MonthYear{}, domain.ErrInvalidMonth},
		{"zero start and end", domain.MonthYear{}, domain.MonthYear{}, domain.ErrInvalidMonth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p, err := domain.NewSubscriptionPeriod(tc.start, tc.end)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewSubscriptionPeriod() error = %v, want %v", err, tc.want)
			}
			if p != (domain.SubscriptionPeriod{}) {
				t.Errorf("NewSubscriptionPeriod() = %+v with an error, want the zero value", p)
			}
		})
	}
}

func TestNewOpenEndedSubscriptionPeriod(t *testing.T) {
	t.Parallel()

	start := mustMonthYear(t, 2025, time.November)
	p, err := domain.NewOpenEndedSubscriptionPeriod(start)
	if err != nil {
		t.Fatalf("NewOpenEndedSubscriptionPeriod() error = %v", err)
	}
	if p.Start() != start {
		t.Errorf("Start() = %+v, want %+v", p.Start(), start)
	}
	if end, ok := p.End(); ok || end != (domain.MonthYear{}) {
		t.Errorf("End() = %+v, %v, want the zero value, false", end, ok)
	}
}

func TestNewOpenEndedSubscriptionPeriod_RejectsZeroStart(t *testing.T) {
	t.Parallel()

	p, err := domain.NewOpenEndedSubscriptionPeriod(domain.MonthYear{})
	if !errors.Is(err, domain.ErrInvalidMonth) {
		t.Fatalf("NewOpenEndedSubscriptionPeriod() error = %v, want %v", err, domain.ErrInvalidMonth)
	}
	if p != (domain.SubscriptionPeriod{}) {
		t.Errorf("NewOpenEndedSubscriptionPeriod() = %+v with an error, want the zero value", p)
	}
}

func TestSubscriptionPeriod_ComparesByValue(t *testing.T) {
	t.Parallel()

	july, august := mustMonthYear(t, 2025, time.July), mustMonthYear(t, 2025, time.August)
	closed := mustSubscriptionPeriod(t, july, august)
	open := mustOpenEndedSubscriptionPeriod(t, july)

	if closed != mustSubscriptionPeriod(t, july, august) {
		t.Error("two equal closed periods are not equal")
	}
	if open != mustOpenEndedSubscriptionPeriod(t, july) {
		t.Error("two equal open-ended periods are not equal")
	}
	if closed == open {
		t.Error("a closed period equals an open-ended one with the same start")
	}
	if closed == mustSubscriptionPeriod(t, july, july) {
		t.Error("periods with different ends are equal")
	}
}

// mustSubscriptionPeriod builds a closed period the test relies on being valid.
func mustSubscriptionPeriod(t *testing.T, start, end domain.MonthYear) domain.SubscriptionPeriod {
	t.Helper()

	p, err := domain.NewSubscriptionPeriod(start, end)
	if err != nil {
		t.Fatalf("NewSubscriptionPeriod() error = %v", err)
	}
	return p
}

// mustOpenEndedSubscriptionPeriod builds an open-ended period the test relies
// on being valid.
func mustOpenEndedSubscriptionPeriod(t *testing.T, start domain.MonthYear) domain.SubscriptionPeriod {
	t.Helper()

	p, err := domain.NewOpenEndedSubscriptionPeriod(start)
	if err != nil {
		t.Fatalf("NewOpenEndedSubscriptionPeriod() error = %v", err)
	}
	return p
}
