package domaintest

import (
	"testing"
	"time"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// TotalCostCase is one subscription and the total cost it adds over a
// calculation period.
type TotalCostCase struct {
	Name               string
	Price              domain.Price
	SubscriptionPeriod domain.SubscriptionPeriod
	CalculationPeriod  domain.CalculationPeriod
	Want               int64
}

// TotalCostCases returns the cases of the total-cost rule: each end of both
// periods inclusive, an open-ended subscription counted until the end of the
// calculation period, and no overlap adding nothing. Every implementation of
// domain.TotalCostReader runs them, the fake and the database alike, so the
// two cannot disagree on any of them.
//
// Each Want is written as months × price, never through the rule itself, or
// the cases would repeat the logic they check.
func TotalCostCases(t testing.TB) []TotalCostCase {
	t.Helper()

	month := func(year int, m time.Month) domain.MonthYear {
		t.Helper()
		my, err := domain.NewMonthYear(year, m)
		if err != nil {
			t.Fatalf("NewMonthYear(%d, %d) error = %v", year, m, err)
		}
		return my
	}
	closed := func(start, end domain.MonthYear) domain.SubscriptionPeriod {
		t.Helper()
		p, err := domain.NewSubscriptionPeriod(start, end)
		if err != nil {
			t.Fatalf("NewSubscriptionPeriod() error = %v", err)
		}
		return p
	}
	open := func(start domain.MonthYear) domain.SubscriptionPeriod {
		t.Helper()
		p, err := domain.NewOpenEndedSubscriptionPeriod(start)
		if err != nil {
			t.Fatalf("NewOpenEndedSubscriptionPeriod() error = %v", err)
		}
		return p
	}
	calculation := func(from, to domain.MonthYear) domain.CalculationPeriod {
		t.Helper()
		p, err := domain.NewCalculationPeriod(from, to)
		if err != nil {
			t.Fatalf("NewCalculationPeriod() error = %v", err)
		}
		return p
	}
	price := func(rubles int64) domain.Price {
		t.Helper()
		p, err := domain.NewPrice(rubles)
		if err != nil {
			t.Fatalf("NewPrice(%d) error = %v", rubles, err)
		}
		return p
	}

	year2025 := calculation(month(2025, time.January), month(2025, time.December))
	rubles400 := price(400)

	return []TotalCostCase{
		{"inside", rubles400, closed(month(2025, time.March), month(2025, time.May)), year2025, 3 * 400},
		{"overlapping the start, across a year", rubles400, closed(month(2024, time.October), month(2025, time.February)), year2025, 2 * 400},
		{"overlapping the end", rubles400, closed(month(2025, time.November), month(2026, time.March)), year2025, 2 * 400},
		{"covering", rubles400, closed(month(2020, time.January), month(2030, time.December)), year2025, 12 * 400},
		{"open-ended, starting inside", rubles400, open(month(2025, time.November)), year2025, 2 * 400},
		{"open-ended, starting before from", rubles400, open(month(2024, time.June)), year2025, 12 * 400},
		{"open-ended, starting after to", rubles400, open(month(2026, time.January)), year2025, 0},
		{"touching from", rubles400, closed(month(2024, time.June), month(2025, time.January)), year2025, 1 * 400},
		{"touching to", rubles400, closed(month(2025, time.December), month(2026, time.March)), year2025, 1 * 400},
		// The disjoint periods are two months or more away from the
		// calculation period: one month away, the month count comes out 0
		// even without the overlap check, so these would not test it.
		{"disjoint, before", rubles400, closed(month(2024, time.January), month(2024, time.October)), year2025, 0},
		{"disjoint, after", rubles400, closed(month(2026, time.March), month(2026, time.April)), year2025, 0},
		{"a one-month subscription", rubles400, closed(month(2025, time.July), month(2025, time.July)), year2025, 1 * 400},
		{
			"a one-month calculation period", rubles400,
			closed(month(2025, time.January), month(2025, time.December)),
			calculation(month(2025, time.July), month(2025, time.July)), 1 * 400,
		},
		{"price 0", price(0), closed(month(2025, time.March), month(2025, time.May)), year2025, 0},
		{
			// 2 602 750 180 164: past the 32-bit range many times over, so a
			// product of 32-bit integers would overflow.
			"the largest price over every month", price(domain.MaxPrice),
			closed(month(domain.MinYear, time.January), month(domain.MaxYear, time.December)),
			calculation(month(domain.MinYear, time.January), month(domain.MaxYear, time.December)),
			1212 * domain.MaxPrice,
		},
	}
}
