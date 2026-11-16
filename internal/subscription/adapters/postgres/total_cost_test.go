package postgres_test

import (
	"testing"
	"time"

	"github.com/sultanrasulov/subscription-service/internal/subscription/adapters/postgres"
	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
	"github.com/sultanrasulov/subscription-service/internal/subscription/domain/domaintest"
)

// Each case stores one subscription for a user of its own and reads its total
// by that user, so one database serves every case and each case also checks
// the user filter.
func TestTotalCostReader_Cases(t *testing.T) {
	t.Parallel()

	repo, pool := newRepository(t)
	reader := postgres.NewTotalCostReader(pool)
	cases := domaintest.TotalCostCases(t)
	users := make([]domain.UserID, len(cases))
	for i, c := range cases {
		users[i] = userID(t)
		s, err := domain.NewSubscription(subscriptionID(t), users[i], serviceName(t, "Yandex Plus"), c.Price, c.SubscriptionPeriod)
		if err != nil {
			t.Fatalf("NewSubscription() error = %v", err)
		}
		if err := repo.Create(t.Context(), s); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	for i, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()

			got := totalCost(t, reader, domain.Filter{UserID: users[i]}, c.CalculationPeriod)
			if got != c.Want {
				t.Errorf("TotalCost() = %d, want %d", got, c.Want)
			}
		})
	}
}

func TestTotalCostReader_Filters(t *testing.T) {
	t.Parallel()

	// Over 2025: Alice's Яндекс Плюс adds 3 × 400 and her Netflix 2 × 599;
	// Bob's Яндекс Плюс adds 12 × 250.
	repo, pool := newRepository(t)
	reader := postgres.NewTotalCostReader(pool)
	alice, bob := userID(t), userID(t)
	store := func(user domain.UserID, name string, rubles int64, period domain.SubscriptionPeriod) {
		t.Helper()
		s, err := domain.NewSubscription(subscriptionID(t), user, serviceName(t, name), price(t, rubles), period)
		if err != nil {
			t.Fatalf("NewSubscription() error = %v", err)
		}
		if err := repo.Create(t.Context(), s); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}
	store(alice, "Яндекс Плюс", 400, closedPeriod(t, month(t, 2025, time.March), month(t, 2025, time.May)))
	store(alice, "Netflix", 599, openPeriod(t, month(t, 2025, time.November)))
	store(bob, "Яндекс Плюс", 250, closedPeriod(t, month(t, 2024, time.January), month(t, 2026, time.December)))
	year2025 := calculationPeriod(t, month(t, 2025, time.January), month(t, 2025, time.December))

	for _, tc := range []struct {
		name   string
		filter domain.Filter
		want   int64
	}{
		{"no filter sums every user", domain.Filter{}, 3*400 + 2*599 + 12*250},
		{"several subscriptions of one user", domain.Filter{UserID: alice}, 3*400 + 2*599},
		{"Cyrillic service in another case", domain.Filter{ServiceName: serviceName(t, "ЯНДЕКС ПЛЮС")}, 3*400 + 12*250},
		{"user and service", domain.Filter{UserID: bob, ServiceName: serviceName(t, "яндекс плюс")}, 12 * 250},
		{"user without subscriptions", domain.Filter{UserID: userID(t)}, 0},
		{"service nobody has", domain.Filter{ServiceName: serviceName(t, "Kinopoisk")}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := totalCost(t, reader, tc.filter, year2025); got != tc.want {
				t.Errorf("TotalCost() = %d, want %d", got, tc.want)
			}
		})
	}
}

// With no rows the sum is NULL in SQL, and the total must still be 0.
func TestTotalCostReader_NoRows(t *testing.T) {
	t.Parallel()

	_, pool := newRepository(t)
	reader := postgres.NewTotalCostReader(pool)
	period := calculationPeriod(t, month(t, 2025, time.January), month(t, 2025, time.December))
	if got := totalCost(t, reader, domain.Filter{}, period); got != 0 {
		t.Errorf("TotalCost() = %d, want 0", got)
	}
}

func totalCost(t *testing.T, reader *postgres.TotalCostReader, f domain.Filter, period domain.CalculationPeriod) int64 {
	t.Helper()
	q, err := domain.NewTotalCostQuery(f, period)
	if err != nil {
		t.Fatalf("NewTotalCostQuery() error = %v", err)
	}
	got, err := reader.TotalCost(t.Context(), q)
	if err != nil {
		t.Fatalf("TotalCost() error = %v", err)
	}
	return got
}

func calculationPeriod(t *testing.T, from, to domain.MonthYear) domain.CalculationPeriod {
	t.Helper()
	p, err := domain.NewCalculationPeriod(from, to)
	if err != nil {
		t.Fatalf("NewCalculationPeriod() error = %v", err)
	}
	return p
}
