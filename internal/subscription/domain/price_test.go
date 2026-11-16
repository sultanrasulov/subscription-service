package domain_test

import (
	"errors"
	"math"
	"testing"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

func TestNewPrice_Accepts(t *testing.T) {
	t.Parallel()

	for name, rubles := range map[string]int64{
		"zero":    0,
		"typical": 400,
		"maximum": 2_147_483_647,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p, err := domain.NewPrice(rubles)
			if err != nil {
				t.Fatalf("NewPrice(%d) error = %v", rubles, err)
			}
			if p.Rubles() != rubles {
				t.Errorf("NewPrice(%d).Rubles() = %d", rubles, p.Rubles())
			}
		})
	}
}

func TestNewPrice_Rejects(t *testing.T) {
	t.Parallel()

	for name, rubles := range map[string]int64{
		"minimum int64":     math.MinInt64,
		"negative":          -1,
		"above the maximum": 2_147_483_648,
		"maximum int64":     math.MaxInt64,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p, err := domain.NewPrice(rubles)
			if !errors.Is(err, domain.ErrPriceOutOfRange) {
				t.Fatalf("NewPrice(%d) error = %v, want %v", rubles, err, domain.ErrPriceOutOfRange)
			}
			if p != (domain.Price{}) {
				t.Errorf("NewPrice(%d) = %+v with an error, want the zero value", rubles, p)
			}
		})
	}
}

// A zero price is valid, so the zero value must be exactly a price of 0.
func TestPrice_ZeroValueIsZeroRubles(t *testing.T) {
	t.Parallel()

	p, err := domain.NewPrice(0)
	if err != nil {
		t.Fatalf("NewPrice(0) error = %v", err)
	}
	if p != (domain.Price{}) {
		t.Errorf("NewPrice(0) = %+v, want the zero value", p)
	}
}
