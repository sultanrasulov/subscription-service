package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

func TestNewCalculationPeriod_Accepts(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		from, to domain.MonthYear
	}{
		{"a year", mustMonthYear(t, 2025, time.January), mustMonthYear(t, 2025, time.December)},
		{"across a year boundary", mustMonthYear(t, 2024, time.December), mustMonthYear(t, 2025, time.January)},
		{"one month", mustMonthYear(t, 2025, time.July), mustMonthYear(t, 2025, time.July)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p, err := domain.NewCalculationPeriod(tc.from, tc.to)
			if err != nil {
				t.Fatalf("NewCalculationPeriod() error = %v", err)
			}
			if p.From() != tc.from || p.To() != tc.to {
				t.Errorf("NewCalculationPeriod() = %+v…%+v, want %+v…%+v", p.From(), p.To(), tc.from, tc.to)
			}
		})
	}
}

func TestNewCalculationPeriod_Rejects(t *testing.T) {
	t.Parallel()

	july := mustMonthYear(t, 2025, time.July)
	for _, tc := range []struct {
		name     string
		from, to domain.MonthYear
		want     error
	}{
		{"to before from", july, mustMonthYear(t, 2025, time.June), domain.ErrInvertedCalculationPeriod},
		{"to before from across a year boundary", mustMonthYear(t, 2025, time.January), mustMonthYear(t, 2024, time.December), domain.ErrInvertedCalculationPeriod},
		{"zero from", domain.MonthYear{}, july, domain.ErrInvalidMonth},
		{"zero to", july, domain.MonthYear{}, domain.ErrInvalidMonth},
		{"zero from and to", domain.MonthYear{}, domain.MonthYear{}, domain.ErrInvalidMonth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p, err := domain.NewCalculationPeriod(tc.from, tc.to)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewCalculationPeriod() error = %v, want %v", err, tc.want)
			}
			if p != (domain.CalculationPeriod{}) {
				t.Errorf("NewCalculationPeriod() = %+v with an error, want the zero value", p)
			}
		})
	}
}
