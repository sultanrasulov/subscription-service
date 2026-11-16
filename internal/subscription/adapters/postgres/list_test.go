package postgres_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sultanrasulov/subscription-service/internal/subscription/adapters/postgres"
	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

func TestRepository_ListFilters(t *testing.T) {
	t.Parallel()

	repo, _ := newRepository(t)
	alice, bob := userID(t), userID(t)
	period := openPeriod(t, month(t, 2025, time.January))
	aliceYandex := create(t, repo, alice, "Yandex Plus", period)
	aliceNetflix := create(t, repo, alice, "Netflix", period)
	bobYandex := create(t, repo, bob, "Yandex Plus", period)

	for _, tc := range []struct {
		name   string
		filter domain.Filter
		want   []domain.Subscription
	}{
		{"no filter", domain.Filter{}, []domain.Subscription{aliceYandex, aliceNetflix, bobYandex}},
		{"user", domain.Filter{UserID: alice}, []domain.Subscription{aliceYandex, aliceNetflix}},
		{"service, in another case", domain.Filter{ServiceName: serviceName(t, "yandex PLUS")}, []domain.Subscription{aliceYandex, bobYandex}},
		{"user and service", domain.Filter{UserID: bob, ServiceName: serviceName(t, "Yandex Plus")}, []domain.Subscription{bobYandex}},
		{"user without subscriptions", domain.Filter{UserID: userID(t)}, nil},
		{"service nobody has", domain.Filter{ServiceName: serviceName(t, "Kinopoisk")}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			items, total, err := repo.List(t.Context(), domain.ListQuery{Filter: tc.filter, Limit: 100})
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			want := byID(tc.want)
			if !slices.Equal(items, want) {
				t.Errorf("List() items = %v, want %v", ids(items), ids(want))
			}
			if total != len(want) {
				t.Errorf("List() total = %d, want %d", total, len(want))
			}
			if items == nil {
				t.Error("List() items = nil, want an empty slice")
			}
		})
	}
}

// Lower-casing Cyrillic depends on the collation; the test database uses the C
// locale, where only an explicit pg_c_utf8 on both sides gets it right.
func TestRepository_ListMatchesCyrillicNamesInAnyCase(t *testing.T) {
	t.Parallel()

	repo, _ := newRepository(t)
	s := create(t, repo, userID(t), "Яндекс Плюс", openPeriod(t, month(t, 2025, time.January)))

	items, total, err := repo.List(t.Context(), domain.ListQuery{
		Filter: domain.Filter{ServiceName: serviceName(t, "ЯНДЕКС ПЛЮС")},
		Limit:  100,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || !slices.Equal(items, []domain.Subscription{s}) {
		t.Errorf("List() = %v, total %d; want %v, total 1", ids(items), total, ids([]domain.Subscription{s}))
	}
}

// On a table of a few rows the planner prefers a sequential scan anyway, so
// the test forbids it for its own transaction and then checks two things: the
// plan uses the index on the lower-cased name, and the row is found. Neither is
// enough alone: with the collation on one side only, the index is still used
// and nothing matches.
func TestRepository_NameFilterUsesTheIndex(t *testing.T) {
	t.Parallel()

	repo, pool := newRepository(t)
	create(t, repo, userID(t), "Яндекс Плюс", openPeriod(t, month(t, 2025, time.January)))
	query, args := postgres.CountQuery(domain.Filter{ServiceName: serviceName(t, "ЯНДЕКС ПЛЮС")})

	err := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL enable_seqscan = off`); err != nil {
			return err
		}
		var plan []byte
		if err := tx.QueryRow(t.Context(), `EXPLAIN (FORMAT JSON) `+query, args...).Scan(&plan); err != nil {
			return err
		}
		if !json.Valid(plan) || !bytes.Contains(plan, []byte(`"Index Name": "subscriptions_service_name_lower_idx"`)) {
			t.Errorf("plan does not use subscriptions_service_name_lower_idx:\n%s", plan)
		}
		var count int
		if err := tx.QueryRow(t.Context(), query, args...).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			t.Errorf("count = %d, want 1", count)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("run the query: %v", err)
	}
}

func TestRepository_ListPagesInIDOrder(t *testing.T) {
	t.Parallel()

	repo, _ := newRepository(t)
	user := userID(t)
	var all []domain.Subscription
	for _, name := range []string{"Netflix", "Yandex Plus", "Kinopoisk", "Okko", "Ivi"} {
		s, err := domain.NewSubscription(subscriptionID(t), user, serviceName(t, name), price(t, 400), openPeriod(t, month(t, 2025, time.January)))
		if err != nil {
			t.Fatalf("NewSubscription() error = %v", err)
		}
		all = append(all, s)
	}
	want := byID(all)
	// Stored in reverse ID order, so the rows' physical order is not the
	// answer: only ORDER BY id gives it.
	for _, s := range slices.Backward(want) {
		if err := repo.Create(t.Context(), s); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	var got []domain.Subscription
	for _, page := range []struct{ offset, size int }{{0, 2}, {2, 2}, {4, 1}, {6, 0}} {
		items, total, err := repo.List(t.Context(), domain.ListQuery{Filter: domain.Filter{UserID: user}, Limit: 2, Offset: page.offset})
		if err != nil {
			t.Fatalf("List(offset %d) error = %v", page.offset, err)
		}
		if len(items) != page.size {
			t.Errorf("List(offset %d) returned %d items, want %d", page.offset, len(items), page.size)
		}
		// The total counts every match, also on a page past the end.
		if total != len(all) {
			t.Errorf("List(offset %d) total = %d, want %d", page.offset, total, len(all))
		}
		got = append(got, items...)
	}
	if !slices.Equal(got, want) {
		t.Errorf("pages = %v, want %v", ids(got), ids(want))
	}
}

// One invalid row fails the whole page, so the error must name the row:
// without its ID it could not be found in the table.
func TestRepository_ListRejectsAnInvalidRow(t *testing.T) {
	t.Parallel()

	repo, pool := newRepository(t)
	id := insertInvalidRow(t, pool)

	_, _, err := repo.List(t.Context(), domain.ListQuery{Limit: 100})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("List() error = %v, want one that wraps %v", err, domain.ErrInvalid)
	}
	if !strings.Contains(err.Error(), id.UUID().String()) {
		t.Errorf("List() error = %q, want it to name the row %s", err, id.UUID())
	}
}

// create stores a subscription with the given user, name and period.
func create(t *testing.T, repo *postgres.Repository, user domain.UserID, name string, period domain.SubscriptionPeriod) domain.Subscription {
	t.Helper()
	s, err := domain.NewSubscription(subscriptionID(t), user, serviceName(t, name), price(t, 400), period)
	if err != nil {
		t.Fatalf("NewSubscription() error = %v", err)
	}
	if err := repo.Create(t.Context(), s); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return s
}

// byID returns the subscriptions in the order the database sorts their IDs:
// byte by byte, as bytes.Compare does.
func byID(subscriptions []domain.Subscription) []domain.Subscription {
	sorted := slices.Clone(subscriptions)
	slices.SortFunc(sorted, func(a, b domain.Subscription) int {
		ida, idb := a.ID().UUID(), b.ID().UUID()
		return bytes.Compare(ida[:], idb[:])
	})
	if sorted == nil {
		return []domain.Subscription{}
	}
	return sorted
}

func ids(subscriptions []domain.Subscription) string {
	var b strings.Builder
	for i, s := range subscriptions {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(s.ServiceName().String())
	}
	return "[" + b.String() + "]"
}
