package application_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sultanrasulov/subscription-service/internal/subscription/application"
	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
	"github.com/sultanrasulov/subscription-service/internal/subscription/domain/domaintest"
)

// fixture is a service over a fresh fake, with its log records in memory.
type fixture struct {
	repo *domaintest.Repository
	svc  *application.Service
	logs *recorder
}

func newFixture() fixture {
	logs := &recorder{}
	repo := domaintest.NewRepository()
	return fixture{repo: repo, svc: application.NewService(repo, slog.New(logs)), logs: logs}
}

// recorder is a slog.Handler that keeps every record.
type recorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func (r *recorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *recorder) Handle(_ context.Context, record slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, record.Clone())
	return nil
}

func (r *recorder) WithAttrs([]slog.Attr) slog.Handler { return r }

func (r *recorder) WithGroup(string) slog.Handler { return r }

func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = nil
}

// events returns the records with the given message, each as its level and
// attributes.
func (f fixture) events(msg string) []map[string]string {
	f.logs.mu.Lock()
	defer f.logs.mu.Unlock()
	var found []map[string]string
	for _, record := range f.logs.records {
		if record.Message != msg {
			continue
		}
		e := map[string]string{"level": record.Level.String()}
		record.Attrs(func(a slog.Attr) bool {
			e[a.Key] = a.Value.String()
			return true
		})
		found = append(found, e)
	}
	return found
}

// wantOneEvent checks that msg was logged exactly once, at INFO, with the
// subscription's ID and user ID.
func (f fixture) wantOneEvent(t *testing.T, msg string, s domain.Subscription) {
	t.Helper()
	events := f.events(msg)
	if len(events) != 1 {
		t.Fatalf("%q logged %d times, want once", msg, len(events))
	}
	e := events[0]
	if e["level"] != "INFO" || e["subscription_id"] != s.ID().UUID().String() || e["user_id"] != s.UserID().UUID().String() {
		t.Errorf("%q = %v, want INFO with subscription_id %s and user_id %s",
			msg, e, s.ID().UUID(), s.UserID().UUID())
	}
}

func TestService_Create(t *testing.T) {
	t.Parallel()

	f := newFixture()
	user, name, price, period := userID(t), serviceName(t, "Yandex Plus"), price(t, 400), openPeriod(t, 2025, time.July)

	s, err := f.svc.Create(t.Context(), user, name, price, period)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if v := s.ID().UUID().Version(); v != 7 {
		t.Errorf("ID version = %d, want 7", v)
	}
	if s.UserID() != user || s.ServiceName() != name || s.Price() != price || s.Period() != period {
		t.Errorf("Create() = %+v, want the given user, name, price and period", s)
	}
	stored, err := f.repo.Get(t.Context(), s.ID())
	if err != nil || stored != s {
		t.Errorf("stored = %+v, %v; want %+v", stored, err, s)
	}
	f.wantOneEvent(t, "subscription created", s)
}

// Each ID comes from the random source, which cannot fail on the operating
// systems Go supports today. If it ever does, Create must report it, not
// panic, and store nothing.
//
// Not parallel: uuid.SetRand replaces a package-level source that has no
// synchronization, and the parallel tests of this package only resume once the
// sequential ones are done.
func TestService_CreateReportsAFailedIDGeneration(t *testing.T) {
	// The inputs first: building a user ID needs the random source too.
	f := newFixture()
	user, name, price, period := userID(t), serviceName(t, "Netflix"), price(t, 599), openPeriod(t, 2025, time.July)

	uuid.SetRand(failingReader{})
	t.Cleanup(func() { uuid.SetRand(nil) })

	// The cause, not some later symptom such as a nil ID, so the log tells
	// what went wrong.
	if _, err := f.svc.Create(t.Context(), user, name, price, period); !errors.Is(err, errNoRandomness) {
		t.Fatalf("Create() error = %v, want one that wraps %v", err, errNoRandomness)
	}
	if _, total, err := f.repo.List(t.Context(), domain.ListQuery{Limit: 10}); err != nil || total != 0 {
		t.Errorf("stored subscriptions = %d, %v; want none", total, err)
	}
	if events := f.events("subscription created"); len(events) != 0 {
		t.Errorf("logged %d creations, want none", len(events))
	}
}

func TestService_Get(t *testing.T) {
	t.Parallel()

	f := newFixture()
	s := f.created(t)

	got, err := f.svc.Get(t.Context(), s.ID())
	if err != nil || got != s {
		t.Errorf("Get() = %+v, %v; want %+v", got, err, s)
	}
	if _, err := f.svc.Get(t.Context(), subscriptionID(t)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get() of a missing subscription error = %v, want %v", err, domain.ErrNotFound)
	}
}

func TestService_Update(t *testing.T) {
	t.Parallel()

	f := newFixture()
	s := f.created(t)
	name, price, period := serviceName(t, "Kinopoisk"), price(t, 0), closedPeriod(t, 2026, time.January, 2026, time.March)

	got, err := f.svc.Update(t.Context(), s.ID(), name, price, period)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if got.ID() != s.ID() || got.UserID() != s.UserID() || got.ServiceName() != name || got.Price() != price || got.Period() != period {
		t.Errorf("Update() = %+v, want the same ID and user with the new name, price and period", got)
	}
	if stored, err := f.repo.Get(t.Context(), s.ID()); err != nil || stored != got {
		t.Errorf("stored = %+v, %v; want %+v", stored, err, got)
	}
	f.wantOneEvent(t, "subscription updated", got)
}

func TestService_UpdateOfAMissingSubscription(t *testing.T) {
	t.Parallel()

	f := newFixture()
	_, err := f.svc.Update(t.Context(), subscriptionID(t), serviceName(t, "Okko"), price(t, 199), openPeriod(t, 2025, time.July))
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Update() error = %v, want %v", err, domain.ErrNotFound)
	}
	if events := f.events("subscription updated"); len(events) != 0 {
		t.Errorf("logged %d updates, want none", len(events))
	}
}

// deletedMeanwhile deletes each subscription just before it is saved, as a
// concurrent delete between the load and the save of an update would.
type deletedMeanwhile struct {
	*domaintest.Repository
}

func (r deletedMeanwhile) Update(ctx context.Context, s domain.Subscription) error {
	if _, err := r.Delete(ctx, s.ID()); err != nil {
		return err
	}
	return r.Repository.Update(ctx, s)
}

// The load succeeds and the save fails: the failure reaches the caller as not
// found, and no event says the update happened.
func TestService_UpdateOfASubscriptionDeletedMeanwhile(t *testing.T) {
	t.Parallel()

	f := newFixture()
	s := f.created(t)
	svc := application.NewService(deletedMeanwhile{f.repo}, slog.New(f.logs))

	_, err := svc.Update(t.Context(), s.ID(), serviceName(t, "Okko"), price(t, 199), openPeriod(t, 2025, time.July))
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Update() error = %v, want %v", err, domain.ErrNotFound)
	}
	if events := f.events("subscription updated"); len(events) != 0 {
		t.Errorf("logged %d updates, want none", len(events))
	}
}

// A rejected update changes nothing, in memory or in the store.
func TestService_UpdateRejectedByTheAggregate(t *testing.T) {
	t.Parallel()

	f := newFixture()
	s := f.created(t)

	_, err := f.svc.Update(t.Context(), s.ID(), domain.ServiceName{}, price(t, 199), openPeriod(t, 2025, time.July))
	if !errors.Is(err, domain.ErrEmptyServiceName) {
		t.Fatalf("Update() error = %v, want %v", err, domain.ErrEmptyServiceName)
	}
	if stored, err := f.repo.Get(t.Context(), s.ID()); err != nil || stored != s {
		t.Errorf("stored = %+v, %v; want it unchanged, %+v", stored, err, s)
	}
	if events := f.events("subscription updated"); len(events) != 0 {
		t.Errorf("logged %d updates, want none", len(events))
	}
}

func TestService_Delete(t *testing.T) {
	t.Parallel()

	f := newFixture()
	s := f.created(t)

	if err := f.svc.Delete(t.Context(), s.ID()); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := f.repo.Get(t.Context(), s.ID()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get() after Delete() error = %v, want %v", err, domain.ErrNotFound)
	}
	f.wantOneEvent(t, "subscription deleted", s)
}

func TestService_DeleteOfAMissingSubscription(t *testing.T) {
	t.Parallel()

	f := newFixture()
	if err := f.svc.Delete(t.Context(), subscriptionID(t)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Delete() error = %v, want %v", err, domain.ErrNotFound)
	}
	if events := f.events("subscription deleted"); len(events) != 0 {
		t.Errorf("logged %d deletions, want none", len(events))
	}
}

func TestService_List(t *testing.T) {
	t.Parallel()

	f := newFixture()
	alice := userID(t)
	for _, name := range []string{"Netflix", "Okko"} {
		if _, err := f.svc.Create(t.Context(), alice, serviceName(t, name), price(t, 400), openPeriod(t, 2025, time.July)); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}
	f.created(t) // another user's

	items, total, err := f.svc.List(t.Context(), domain.ListQuery{Filter: domain.Filter{UserID: alice}, Limit: 1})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || items[0].UserID() != alice || total != 2 {
		t.Errorf("List() = %d items, total %d; want 1 of alice's, total 2", len(items), total)
	}
}

// The service passes the store's failures on, wrapped, so the HTTP adapter can
// still recognize them.
func TestService_PassesStoreFailuresOn(t *testing.T) {
	t.Parallel()

	f := newFixture()
	s := f.created(t)
	boom := errors.New("boom")
	f.repo.Fail(boom)

	_, createErr := f.svc.Create(t.Context(), userID(t), serviceName(t, "Okko"), price(t, 199), openPeriod(t, 2025, time.July))
	_, getErr := f.svc.Get(t.Context(), s.ID())
	_, updateErr := f.svc.Update(t.Context(), s.ID(), s.ServiceName(), s.Price(), s.Period())
	_, _, listErr := f.svc.List(t.Context(), domain.ListQuery{Limit: 10})
	for op, err := range map[string]error{
		"Create": createErr, "Get": getErr, "Update": updateErr,
		"Delete": f.svc.Delete(t.Context(), s.ID()), "List": listErr,
	} {
		if !errors.Is(err, boom) {
			t.Errorf("%s() error = %v, want %v", op, err, boom)
		}
	}
	// An event says that something happened; nothing did.
	for _, msg := range []string{"subscription created", "subscription updated", "subscription deleted"} {
		if events := f.events(msg); len(events) != 0 {
			t.Errorf("%q logged %d times after failures, want none", msg, len(events))
		}
	}
}

func TestService_PassesAnExpiredDeadlineOn(t *testing.T) {
	t.Parallel()

	f := newFixture()
	f.repo.Block()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	if _, err := f.svc.Get(ctx, subscriptionID(t)); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Get() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

// created stores a subscription through the service and clears the log, so a
// test sees only its own events.
func (f fixture) created(t *testing.T) domain.Subscription {
	t.Helper()
	s, err := f.svc.Create(t.Context(), userID(t), serviceName(t, "Yandex Plus"), price(t, 400), openPeriod(t, 2025, time.July))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	f.logs.reset()
	return s
}

var errNoRandomness = errors.New("no randomness")

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errNoRandomness }

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
		t.Fatalf("NewMonthYear() error = %v", err)
	}
	return my
}

func openPeriod(t *testing.T, year int, m time.Month) domain.SubscriptionPeriod {
	t.Helper()
	p, err := domain.NewOpenEndedSubscriptionPeriod(month(t, year, m))
	if err != nil {
		t.Fatalf("NewOpenEndedSubscriptionPeriod() error = %v", err)
	}
	return p
}

func closedPeriod(t *testing.T, fromYear int, from time.Month, toYear int, to time.Month) domain.SubscriptionPeriod {
	t.Helper()
	p, err := domain.NewSubscriptionPeriod(month(t, fromYear, from), month(t, toYear, to))
	if err != nil {
		t.Fatalf("NewSubscriptionPeriod() error = %v", err)
	}
	return p
}
