package domaintest

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// The fake stands in for the database adapter in the tests of the layers
// above the domain, so it keeps the port's contract the same way.

func TestRepository_CreateThenGet(t *testing.T) {
	t.Parallel()

	repo := NewRepository()
	s := subscription(t, userID(t), "Netflix")
	if err := repo.Create(t.Context(), s); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	got, err := repo.Get(t.Context(), s.ID())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != s {
		t.Errorf("Get() = %+v, want %+v", got, s)
	}
}

func TestRepository_CreateRejectsADuplicateID(t *testing.T) {
	t.Parallel()

	repo := NewRepository()
	s := subscription(t, userID(t), "Netflix")
	if err := repo.Create(t.Context(), s); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	if err := repo.Create(t.Context(), s); err == nil {
		t.Error("second Create() with the same ID error = nil, want an error")
	}
}

func TestRepository_ReportsNotFound(t *testing.T) {
	t.Parallel()

	repo := NewRepository()
	missing := subscription(t, userID(t), "Netflix")

	_, getErr := repo.Get(t.Context(), missing.ID())
	updateErr := repo.Update(t.Context(), missing)
	_, deleteErr := repo.Delete(t.Context(), missing.ID())
	for op, err := range map[string]error{"Get": getErr, "Update": updateErr, "Delete": deleteErr} {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s() error = %v, want %v", op, err, domain.ErrNotFound)
		}
	}
}

func TestRepository_UpdateKeepsTheStoredUser(t *testing.T) {
	t.Parallel()

	repo := NewRepository()
	s := subscription(t, userID(t), "Netflix")
	if err := repo.Create(t.Context(), s); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	other, err := domain.NewSubscription(s.ID(), userID(t), serviceName(t, "Kinopoisk"), s.Price(), s.Period())
	if err != nil {
		t.Fatalf("NewSubscription() error = %v", err)
	}
	if err := repo.Update(t.Context(), other); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, err := repo.Get(t.Context(), s.ID())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.UserID() != s.UserID() || got.ServiceName() != other.ServiceName() {
		t.Errorf("after Update() user %s, name %q; want user %s, name %q",
			got.UserID().UUID(), got.ServiceName(), s.UserID().UUID(), other.ServiceName())
	}
}

func TestRepository_DeleteReturnsTheDeletedSubscription(t *testing.T) {
	t.Parallel()

	repo := NewRepository()
	s := subscription(t, userID(t), "Netflix")
	if err := repo.Create(t.Context(), s); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	deleted, err := repo.Delete(t.Context(), s.ID())
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if deleted != s {
		t.Errorf("Delete() = %+v, want %+v", deleted, s)
	}
	if _, err := repo.Get(t.Context(), s.ID()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get() after Delete() error = %v, want %v", err, domain.ErrNotFound)
	}
}

func TestRepository_ListFiltersOrdersAndPages(t *testing.T) {
	t.Parallel()

	repo := NewRepository()
	alice, bob := userID(t), userID(t)
	all := []domain.Subscription{
		subscription(t, alice, "Yandex Plus"),
		subscription(t, alice, "Netflix"),
		subscription(t, bob, "Yandex Plus"),
	}
	// Stored in reverse ID order, so only sorting gives the ID order.
	for _, s := range slices.Backward(byID(all)) {
		if err := repo.Create(t.Context(), s); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	for _, tc := range []struct {
		name      string
		query     domain.ListQuery
		wantItems []domain.Subscription
		wantTotal int
	}{
		{"everything", domain.ListQuery{Limit: 10}, byID(all), 3},
		{"user", domain.ListQuery{Filter: domain.Filter{UserID: alice}, Limit: 10}, byID(all[:2]), 2},
		{"service in another case", domain.ListQuery{Filter: domain.Filter{ServiceName: serviceName(t, "YANDEX plus")}, Limit: 10},
			byID([]domain.Subscription{all[0], all[2]}), 2},
		{"user and service", domain.ListQuery{Filter: domain.Filter{UserID: bob, ServiceName: serviceName(t, "Yandex Plus")}, Limit: 10},
			all[2:], 1},
		{"a page", domain.ListQuery{Limit: 1, Offset: 1}, byID(all)[1:2], 3},
		{"past the end", domain.ListQuery{Limit: 10, Offset: 5}, []domain.Subscription{}, 3},
		{"no match", domain.ListQuery{Filter: domain.Filter{UserID: userID(t)}, Limit: 10}, []domain.Subscription{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			items, total, err := repo.List(t.Context(), tc.query)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if !slices.Equal(items, tc.wantItems) || total != tc.wantTotal {
				t.Errorf("List() = %d items, total %d; want %d items, total %d",
					len(items), total, len(tc.wantItems), tc.wantTotal)
			}
			if items == nil {
				t.Error("List() items = nil, want an empty slice")
			}
		})
	}
}

// The database rejects a negative LIMIT or OFFSET with an error; so does the
// fake, rather than panicking on a slice bound.
func TestRepository_ListRejectsANegativePage(t *testing.T) {
	t.Parallel()

	repo := NewRepository()
	if err := repo.Create(t.Context(), subscription(t, userID(t), "Netflix")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	for name, q := range map[string]domain.ListQuery{
		"negative limit":  {Limit: -1},
		"negative offset": {Limit: 10, Offset: -1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, _, err := repo.List(t.Context(), q); err == nil {
				t.Errorf("List(%+v) error = nil, want an error", q)
			}
		})
	}
}

func TestRepository_Fail(t *testing.T) {
	t.Parallel()

	repo := NewRepository()
	s := subscription(t, userID(t), "Netflix")
	if err := repo.Create(t.Context(), s); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	boom := errors.New("boom")
	repo.Fail(boom)

	_, getErr := repo.Get(t.Context(), s.ID())
	_, _, listErr := repo.List(t.Context(), domain.ListQuery{Limit: 10})
	for op, err := range map[string]error{
		"Create": repo.Create(t.Context(), subscription(t, userID(t), "Okko")),
		"Get":    getErr,
		"Update": repo.Update(t.Context(), s),
		"List":   listErr,
	} {
		if !errors.Is(err, boom) {
			t.Errorf("%s() error = %v, want %v", op, err, boom)
		}
	}
	if _, err := repo.Delete(t.Context(), s.ID()); !errors.Is(err, boom) {
		t.Errorf("Delete() error = %v, want %v", err, boom)
	}
}

func TestRepository_Block(t *testing.T) {
	t.Parallel()

	repo := NewRepository()
	repo.Block()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	s := subscription(t, userID(t), "Netflix")
	if _, err := repo.Get(ctx, s.ID()); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Get() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func subscription(t *testing.T, user domain.UserID, name string) domain.Subscription {
	t.Helper()
	id, err := domain.NewSubscriptionID(uuid.Must(uuid.NewV7()))
	if err != nil {
		t.Fatalf("NewSubscriptionID() error = %v", err)
	}
	price, err := domain.NewPrice(400)
	if err != nil {
		t.Fatalf("NewPrice() error = %v", err)
	}
	start, err := domain.NewMonthYear(2025, time.January)
	if err != nil {
		t.Fatalf("NewMonthYear() error = %v", err)
	}
	period, err := domain.NewOpenEndedSubscriptionPeriod(start)
	if err != nil {
		t.Fatalf("NewOpenEndedSubscriptionPeriod() error = %v", err)
	}
	s, err := domain.NewSubscription(id, user, serviceName(t, name), price, period)
	if err != nil {
		t.Fatalf("NewSubscription() error = %v", err)
	}
	return s
}

func userID(t *testing.T) domain.UserID {
	t.Helper()
	u, err := domain.NewUserID(uuid.New())
	if err != nil {
		t.Fatalf("NewUserID() error = %v", err)
	}
	return u
}

func serviceName(t *testing.T, raw string) domain.ServiceName {
	t.Helper()
	n, err := domain.NewServiceName(raw)
	if err != nil {
		t.Fatalf("NewServiceName(%q) error = %v", raw, err)
	}
	return n
}

func byID(subscriptions []domain.Subscription) []domain.Subscription {
	sorted := slices.Clone(subscriptions)
	slices.SortFunc(sorted, func(a, b domain.Subscription) int {
		ida, idb := a.ID().UUID(), b.ID().UUID()
		return bytes.Compare(ida[:], idb[:])
	})
	return sorted
}
