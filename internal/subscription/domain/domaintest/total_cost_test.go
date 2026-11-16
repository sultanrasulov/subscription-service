package domaintest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// Each case stores one subscription for a user of its own and reads its total
// by that user, as the database adapter's test does with the same cases.
func TestTotalCostReader_Cases(t *testing.T) {
	t.Parallel()

	store := NewRepository()
	reader := NewTotalCostReader(store)
	cases := TotalCostCases(t)
	users := make([]domain.UserID, len(cases))
	for i, c := range cases {
		users[i] = userID(t)
		store.add(t, users[i], "Yandex Plus", c.Price, c.SubscriptionPeriod)
	}

	for i, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()

			if got := totalCost(t, reader, domain.Filter{UserID: users[i]}, c.CalculationPeriod); got != c.Want {
				t.Errorf("TotalCost() = %d, want %d", got, c.Want)
			}
		})
	}
}

func TestTotalCostReader_Filters(t *testing.T) {
	t.Parallel()

	// Over 2025: Alice's Yandex Plus adds 3 × 400 and her Netflix 2 × 599;
	// Bob's Yandex Plus adds 12 × 250.
	store := NewRepository()
	reader := NewTotalCostReader(store)
	alice, bob := userID(t), userID(t)
	store.add(t, alice, "Yandex Plus", price(t, 400), closed(t, month(t, 2025, time.March), month(t, 2025, time.May)))
	store.add(t, alice, "Netflix", price(t, 599), open(t, month(t, 2025, time.November)))
	store.add(t, bob, "Yandex Plus", price(t, 250), closed(t, month(t, 2024, time.January), month(t, 2026, time.December)))
	year2025 := calculation(t, month(t, 2025, time.January), month(t, 2025, time.December))

	for _, tc := range []struct {
		name   string
		filter domain.Filter
		want   int64
	}{
		{"no filter sums every user", domain.Filter{}, 3*400 + 2*599 + 12*250},
		{"several subscriptions of one user", domain.Filter{UserID: alice}, 3*400 + 2*599},
		{"service in another case", domain.Filter{ServiceName: serviceName(t, "YANDEX plus")}, 3*400 + 12*250},
		{"user and service", domain.Filter{UserID: bob, ServiceName: serviceName(t, "yandex plus")}, 12 * 250},
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

func TestTotalCostReader_NoRows(t *testing.T) {
	t.Parallel()

	reader := NewTotalCostReader(NewRepository())
	period := calculation(t, month(t, 2025, time.January), month(t, 2025, time.December))
	if got := totalCost(t, reader, domain.Filter{}, period); got != 0 {
		t.Errorf("TotalCost() = %d, want 0", got)
	}
}

// The reader and the repository are one store, as they are one database, so
// the store's Fail and Block act on the reader too.
func TestTotalCostReader_FailsAndBlocksWithTheStore(t *testing.T) {
	t.Parallel()

	period := calculation(t, month(t, 2025, time.January), month(t, 2025, time.December))
	q, err := domain.NewTotalCostQuery(domain.Filter{}, period)
	if err != nil {
		t.Fatalf("NewTotalCostQuery() error = %v", err)
	}

	failing := NewRepository()
	boom := errors.New("boom")
	failing.Fail(boom)
	if _, err := NewTotalCostReader(failing).TotalCost(t.Context(), q); !errors.Is(err, boom) {
		t.Errorf("TotalCost() error = %v, want %v", err, boom)
	}

	blocked := NewRepository()
	blocked.Block()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := NewTotalCostReader(blocked).TotalCost(ctx, q); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("TotalCost() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

// add stores a subscription with the given user, name, price and period.
func (r *Repository) add(t *testing.T, user domain.UserID, name string, p domain.Price, period domain.SubscriptionPeriod) {
	t.Helper()
	id, err := domain.NewSubscriptionID(uuid.Must(uuid.NewV7()))
	if err != nil {
		t.Fatalf("NewSubscriptionID() error = %v", err)
	}
	s, err := domain.NewSubscription(id, user, serviceName(t, name), p, period)
	if err != nil {
		t.Fatalf("NewSubscription() error = %v", err)
	}
	if err := r.Create(t.Context(), s); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
}

func totalCost(t *testing.T, reader *TotalCostReader, f domain.Filter, period domain.CalculationPeriod) int64 {
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

func price(t *testing.T, rubles int64) domain.Price {
	t.Helper()
	p, err := domain.NewPrice(rubles)
	if err != nil {
		t.Fatalf("NewPrice(%d) error = %v", rubles, err)
	}
	return p
}

func month(t *testing.T, year int, m time.Month) domain.MonthYear {
	t.Helper()
	my, err := domain.NewMonthYear(year, m)
	if err != nil {
		t.Fatalf("NewMonthYear(%d, %d) error = %v", year, m, err)
	}
	return my
}

func closed(t *testing.T, start, end domain.MonthYear) domain.SubscriptionPeriod {
	t.Helper()
	p, err := domain.NewSubscriptionPeriod(start, end)
	if err != nil {
		t.Fatalf("NewSubscriptionPeriod() error = %v", err)
	}
	return p
}

func open(t *testing.T, start domain.MonthYear) domain.SubscriptionPeriod {
	t.Helper()
	p, err := domain.NewOpenEndedSubscriptionPeriod(start)
	if err != nil {
		t.Fatalf("NewOpenEndedSubscriptionPeriod() error = %v", err)
	}
	return p
}

func calculation(t *testing.T, from, to domain.MonthYear) domain.CalculationPeriod {
	t.Helper()
	p, err := domain.NewCalculationPeriod(from, to)
	if err != nil {
		t.Fatalf("NewCalculationPeriod() error = %v", err)
	}
	return p
}
