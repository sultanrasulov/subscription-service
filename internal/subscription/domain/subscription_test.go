package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// fields is everything NewSubscription takes, so a test can start from a
// valid set and break one field at a time.
type fields struct {
	id     uuid.UUID
	userID domain.UserID
	name   domain.ServiceName
	price  domain.Price
	period domain.SubscriptionPeriod
}

func validFields(t *testing.T) fields {
	t.Helper()

	return fields{
		id:     uuid.MustParse("01963b4e-7c1a-7d3e-9f2b-1a2b3c4d5e6f"),
		userID: mustUserID(t, uuid.MustParse("60601fee-2bf1-4721-ae6f-7636e79a0cba")),
		name:   mustServiceName(t, "Yandex Plus"),
		price:  mustPrice(t, 400),
		period: mustSubscriptionPeriod(t, mustMonthYear(t, 2025, time.July), mustMonthYear(t, 2025, time.December)),
	}
}

func (f fields) newSubscription() (domain.Subscription, error) {
	return domain.NewSubscription(f.id, f.userID, f.name, f.price, f.period)
}

func mustSubscription(t *testing.T, f fields) domain.Subscription {
	t.Helper()

	s, err := f.newSubscription()
	if err != nil {
		t.Fatalf("NewSubscription() error = %v", err)
	}
	return s
}

func TestNewSubscription_KeepsEveryField(t *testing.T) {
	t.Parallel()

	f := validFields(t)
	s := mustSubscription(t, f)

	if s.ID() != f.id {
		t.Errorf("ID() = %s, want %s", s.ID(), f.id)
	}
	if s.UserID() != f.userID {
		t.Errorf("UserID() = %s, want %s", s.UserID().UUID(), f.userID.UUID())
	}
	if s.ServiceName() != f.name {
		t.Errorf("ServiceName() = %q, want %q", s.ServiceName(), f.name)
	}
	if s.Price() != f.price {
		t.Errorf("Price() = %d, want %d", s.Price().Rubles(), f.price.Rubles())
	}
	if s.Period() != f.period {
		t.Errorf("Period() = %+v, want %+v", s.Period(), f.period)
	}
}

// A free trial is a subscription with a price of 0.
func TestNewSubscription_AcceptsZeroPrice(t *testing.T) {
	t.Parallel()

	f := validFields(t)
	f.price = domain.Price{}
	s := mustSubscription(t, f)

	if s.Price().Rubles() != 0 {
		t.Errorf("Price() = %d, want 0", s.Price().Rubles())
	}
}

// A zero value object has not been through its constructor. The aggregate
// rejects it with the error its constructor gives the equivalent input, and
// checks the fields in order, so the first invalid one is reported.
func TestNewSubscription_RejectsZeroFields(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		spoil func(*fields)
		want  error
	}{
		{"nil ID", func(f *fields) { f.id = uuid.Nil }, domain.ErrNilSubscriptionID},
		{"zero user ID", func(f *fields) { f.userID = domain.UserID{} }, domain.ErrNilUserID},
		{"zero service name", func(f *fields) { f.name = domain.ServiceName{} }, domain.ErrEmptyServiceName},
		{"zero period", func(f *fields) { f.period = domain.SubscriptionPeriod{} }, domain.ErrInvalidMonth},
		{"every field", func(f *fields) { *f = fields{} }, domain.ErrNilSubscriptionID},
		{"user ID before service name", func(f *fields) {
			f.userID, f.name = domain.UserID{}, domain.ServiceName{}
		}, domain.ErrNilUserID},
		{"service name before period", func(f *fields) {
			f.name, f.period = domain.ServiceName{}, domain.SubscriptionPeriod{}
		}, domain.ErrEmptyServiceName},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := validFields(t)
			tc.spoil(&f)
			s, err := f.newSubscription()
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewSubscription() error = %v, want %v", err, tc.want)
			}
			if s != (domain.Subscription{}) {
				t.Errorf("NewSubscription() = %+v with an error, want the zero value", s)
			}
		})
	}
}

func TestSubscription_UpdateReplacesNamePriceAndPeriod(t *testing.T) {
	t.Parallel()

	f := validFields(t)
	s := mustSubscription(t, f)
	name := mustServiceName(t, "Kinopoisk")
	price := mustPrice(t, 0)
	period := mustSubscriptionPeriod(t, mustMonthYear(t, 2026, time.January), mustMonthYear(t, 2026, time.March))

	if err := s.Update(name, price, period); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if s.ServiceName() != name || s.Price() != price || s.Period() != period {
		t.Errorf("after Update() = %q, %d, %+v, want %q, %d, %+v",
			s.ServiceName(), s.Price().Rubles(), s.Period(), name, price.Rubles(), period)
	}
	if s.ID() != f.id || s.UserID() != f.userID {
		t.Errorf("Update() changed the identity: ID %s, user %s, want %s, %s",
			s.ID(), s.UserID().UUID(), f.id, f.userID.UUID())
	}
}

func TestSubscription_UpdateToOpenEnded(t *testing.T) {
	t.Parallel()

	f := validFields(t)
	s := mustSubscription(t, f)
	open := mustOpenEndedSubscriptionPeriod(t, f.period.Start())

	if err := s.Update(f.name, f.price, open); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if _, ok := s.Period().End(); ok {
		t.Error("after Update() with an open-ended period, Period().End() reports an end")
	}
}

// Update checks every argument before it changes anything, so a rejected
// update leaves the subscription exactly as it was, even when the earlier
// arguments were valid.
func TestSubscription_RejectedUpdateChangesNothing(t *testing.T) {
	t.Parallel()

	name := mustServiceName(t, "Kinopoisk")
	price := mustPrice(t, 299)
	period := mustOpenEndedSubscriptionPeriod(t, mustMonthYear(t, 2026, time.January))
	for _, tc := range []struct {
		name   string
		update func(*domain.Subscription) error
		want   error
	}{
		{"zero service name", func(s *domain.Subscription) error {
			return s.Update(domain.ServiceName{}, price, period)
		}, domain.ErrEmptyServiceName},
		{"zero period", func(s *domain.Subscription) error {
			return s.Update(name, price, domain.SubscriptionPeriod{})
		}, domain.ErrInvalidMonth},
		{"service name before period", func(s *domain.Subscription) error {
			return s.Update(domain.ServiceName{}, price, domain.SubscriptionPeriod{})
		}, domain.ErrEmptyServiceName},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := mustSubscription(t, validFields(t))
			before := s
			if err := tc.update(&s); !errors.Is(err, tc.want) {
				t.Fatalf("Update() error = %v, want %v", err, tc.want)
			}
			if s != before {
				t.Errorf("after a rejected Update() = %+v, want %+v", s, before)
			}
		})
	}
}

// The aggregate is a value: updating a copy, such as one a repository handed
// out, leaves the original untouched.
func TestSubscription_CopyIsIndependent(t *testing.T) {
	t.Parallel()

	original := mustSubscription(t, validFields(t))
	before := original
	copied := original
	if err := copied.Update(mustServiceName(t, "Kinopoisk"), mustPrice(t, 299), original.Period()); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if original != before {
		t.Errorf("updating a copy changed the original to %+v", original)
	}
}

func mustUserID(t *testing.T, id uuid.UUID) domain.UserID {
	t.Helper()

	u, err := domain.NewUserID(id)
	if err != nil {
		t.Fatalf("NewUserID(%s) error = %v", id, err)
	}
	return u
}

func mustServiceName(t *testing.T, raw string) domain.ServiceName {
	t.Helper()

	n, err := domain.NewServiceName(raw)
	if err != nil {
		t.Fatalf("NewServiceName(%q) error = %v", raw, err)
	}
	return n
}

func mustPrice(t *testing.T, rubles int64) domain.Price {
	t.Helper()

	p, err := domain.NewPrice(rubles)
	if err != nil {
		t.Fatalf("NewPrice(%d) error = %v", rubles, err)
	}
	return p
}
