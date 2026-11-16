package postgres_test

import (
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sultanrasulov/subscription-service/internal/platform/config"
	platformpostgres "github.com/sultanrasulov/subscription-service/internal/platform/postgres"
	"github.com/sultanrasulov/subscription-service/internal/platform/postgres/pgtest"
	"github.com/sultanrasulov/subscription-service/internal/subscription/adapters/postgres"
	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
	"github.com/sultanrasulov/subscription-service/migrations"
)

// server is the test container, started once for the package.
var server config.Postgres

func TestMain(m *testing.M) {
	os.Exit(pgtest.Main(m, &server))
}

// newRepository returns a repository on a fresh, migrated database, and its
// pool, for the checks that read the table directly.
func newRepository(t *testing.T) (*postgres.Repository, *pgxpool.Pool) {
	t.Helper()

	cfg := pgtest.NewDatabase(t, server, "UTF8")
	pool, err := platformpostgres.NewPool(t.Context(), cfg)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if err := platformpostgres.Migrate(t.Context(), pool, migrations.FS, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return postgres.NewRepository(pool), pool
}

func TestRepository_CreateThenGet(t *testing.T) {
	t.Parallel()

	repo, _ := newRepository(t)
	for _, tc := range []struct {
		name   string
		price  int64
		period domain.SubscriptionPeriod
	}{
		{"Yandex Plus", 400, closedPeriod(t, month(t, 2025, time.July), month(t, 2025, time.December))},
		{"Netflix", 599, openPeriod(t, month(t, 2025, time.November))},
		{"Kinopoisk", 0, closedPeriod(t, month(t, 2026, time.January), month(t, 2026, time.January))},
		{"Яндекс Плюс", 299, openPeriod(t, month(t, 2025, time.March))},
		{"Boundaries", domain.MaxPrice, closedPeriod(t, month(t, 2000, time.January), month(t, 2100, time.December))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			want := newSubscription(t, tc.name, tc.price, tc.period)
			if err := repo.Create(t.Context(), want); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			got, err := repo.Get(t.Context(), want.ID())
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if got != want {
				t.Errorf("Get() = %+v, want %+v", got, want)
			}
		})
	}
}

// IDs are generated, so a duplicate means a bug; it must fail, not overwrite.
func TestRepository_CreateRejectsADuplicateID(t *testing.T) {
	t.Parallel()

	repo, _ := newRepository(t)
	s := newSubscription(t, "Netflix", 599, openPeriod(t, month(t, 2025, time.November)))
	if err := repo.Create(t.Context(), s); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	if err := repo.Create(t.Context(), s); err == nil {
		t.Error("second Create() with the same ID error = nil, want an error")
	}
}

func TestRepository_ReportsNotFound(t *testing.T) {
	t.Parallel()

	repo, _ := newRepository(t)
	missing := newSubscription(t, "Netflix", 599, openPeriod(t, month(t, 2025, time.November)))

	_, getErr := repo.Get(t.Context(), missing.ID())
	updateErr := repo.Update(t.Context(), missing)
	_, deleteErr := repo.Delete(t.Context(), missing.ID())
	for op, err := range map[string]error{"Get": getErr, "Update": updateErr, "Delete": deleteErr} {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s() error = %v, want %v", op, err, domain.ErrNotFound)
		}
	}
}

func TestRepository_UpdateReplacesTheStoredSubscription(t *testing.T) {
	t.Parallel()

	repo, pool := newRepository(t)
	s := newSubscription(t, "Yandex Plus", 400, closedPeriod(t, month(t, 2025, time.July), month(t, 2025, time.December)))
	if err := repo.Create(t.Context(), s); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	before := updatedAt(t, pool, s.ID())

	if err := s.Update(serviceName(t, "Yandex Plus Multi"), price(t, 649), openPeriod(t, month(t, 2025, time.July))); err != nil {
		t.Fatalf("Subscription.Update() error = %v", err)
	}
	if err := repo.Update(t.Context(), s); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, err := repo.Get(t.Context(), s.ID())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != s {
		t.Errorf("Get() after Update() = %+v, want %+v", got, s)
	}
	if after := updatedAt(t, pool, s.ID()); !after.After(before) {
		t.Errorf("updated_at = %v after Update(), want later than %v", after, before)
	}
}

// The user of a subscription never changes. The aggregate cannot express a
// change, so the test builds a subscription with the same ID and another user,
// and the statement must still leave the stored user alone.
func TestRepository_UpdateNeverChangesTheUser(t *testing.T) {
	t.Parallel()

	repo, _ := newRepository(t)
	s := newSubscription(t, "Netflix", 599, openPeriod(t, month(t, 2025, time.November)))
	if err := repo.Create(t.Context(), s); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	other, err := domain.NewSubscription(s.ID(), userID(t), s.ServiceName(), s.Price(), s.Period())
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
	if got.UserID() != s.UserID() {
		t.Errorf("user after Update() = %s, want %s", got.UserID().UUID(), s.UserID().UUID())
	}
}

func TestRepository_DeleteReturnsTheDeletedSubscription(t *testing.T) {
	t.Parallel()

	repo, _ := newRepository(t)
	s := newSubscription(t, "Kinopoisk", 299, closedPeriod(t, month(t, 2025, time.January), month(t, 2025, time.June)))
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

// A row that bypassed the domain and breaks one of its rules fails to load,
// rather than loading as an invalid subscription. The error keeps the domain's
// chain; the HTTP adapter decides what status it means.
func TestRepository_GetRejectsAnInvalidRow(t *testing.T) {
	t.Parallel()

	repo, pool := newRepository(t)
	id := insertInvalidRow(t, pool)

	_, err := repo.Get(t.Context(), id)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Get() error = %v, want one that wraps %v", err, domain.ErrInvalid)
	}
	if !strings.Contains(err.Error(), id.UUID().String()) {
		t.Errorf("Get() error = %q, want it to name the row %s", err, id.UUID())
	}
}

// Delete builds the subscription from the row it removes. When that fails,
// the delete must be undone: an error has to mean that nothing changed.
func TestRepository_DeleteKeepsAnInvalidRow(t *testing.T) {
	t.Parallel()

	repo, pool := newRepository(t)
	id := insertInvalidRow(t, pool)

	if _, err := repo.Delete(t.Context(), id); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Delete() error = %v, want one that wraps %v", err, domain.ErrInvalid)
	}
	var rows int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM subscriptions WHERE id = $1`, id.UUID()).Scan(&rows); err != nil {
		t.Fatalf("count the rows: %v", err)
	}
	if rows != 1 {
		t.Errorf("rows with the ID after a failed Delete() = %d, want 1", rows)
	}
}

// insertInvalidRow stores, past the domain, a row that every CHECK accepts but
// the domain rejects: its name is only a no-break space, which normalizes to
// an empty name.
func insertInvalidRow(t *testing.T, pool *pgxpool.Pool) domain.SubscriptionID {
	t.Helper()
	id := subscriptionID(t)
	if _, err := pool.Exec(t.Context(), `INSERT INTO subscriptions
		(id, user_id, service_name, price, start_date)
		VALUES ($1, gen_random_uuid(), $2, 400, '2025-07-01')`, id.UUID(), "\u00a0"); err != nil {
		t.Fatalf("insert the invalid row: %v", err)
	}
	return id
}

func updatedAt(t *testing.T, pool *pgxpool.Pool, id domain.SubscriptionID) time.Time {
	t.Helper()
	var at time.Time
	if err := pool.QueryRow(t.Context(), `SELECT updated_at FROM subscriptions WHERE id = $1`, id.UUID()).Scan(&at); err != nil {
		t.Fatalf("read updated_at: %v", err)
	}
	return at
}

func newSubscription(t *testing.T, name string, rubles int64, period domain.SubscriptionPeriod) domain.Subscription {
	t.Helper()
	s, err := domain.NewSubscription(subscriptionID(t), userID(t), serviceName(t, name), price(t, rubles), period)
	if err != nil {
		t.Fatalf("NewSubscription() error = %v", err)
	}
	return s
}

func subscriptionID(t *testing.T) domain.SubscriptionID {
	t.Helper()
	id, err := domain.NewSubscriptionID(uuid.Must(uuid.NewV7()))
	if err != nil {
		t.Fatalf("NewSubscriptionID() error = %v", err)
	}
	return id
}

func userID(t *testing.T) domain.UserID {
	t.Helper()
	id, err := domain.NewUserID(uuid.New())
	if err != nil {
		t.Fatalf("NewUserID() error = %v", err)
	}
	return id
}

func serviceName(t *testing.T, raw string) domain.ServiceName {
	t.Helper()
	n, err := domain.NewServiceName(raw)
	if err != nil {
		t.Fatalf("NewServiceName(%q) error = %v", raw, err)
	}
	return n
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

func closedPeriod(t *testing.T, start, end domain.MonthYear) domain.SubscriptionPeriod {
	t.Helper()
	p, err := domain.NewSubscriptionPeriod(start, end)
	if err != nil {
		t.Fatalf("NewSubscriptionPeriod() error = %v", err)
	}
	return p
}

func openPeriod(t *testing.T, start domain.MonthYear) domain.SubscriptionPeriod {
	t.Helper()
	p, err := domain.NewOpenEndedSubscriptionPeriod(start)
	if err != nil {
		t.Fatalf("NewOpenEndedSubscriptionPeriod() error = %v", err)
	}
	return p
}
