package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

func TestNewTotalCostQuery_KeepsFilterAndPeriod(t *testing.T) {
	t.Parallel()

	period := mustCalculationPeriod(t, mustMonthYear(t, 2025, time.January), mustMonthYear(t, 2025, time.December))
	for name, filter := range map[string]domain.Filter{
		"no filter":    {},
		"user only":    {UserID: mustUserID(t, uuid.MustParse("60601fee-2bf1-4721-ae6f-7636e79a0cba"))},
		"service only": {ServiceName: mustServiceName(t, "Yandex Plus")},
		"user and service": {
			UserID:      mustUserID(t, uuid.MustParse("60601fee-2bf1-4721-ae6f-7636e79a0cba")),
			ServiceName: mustServiceName(t, "Yandex Plus"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			q, err := domain.NewTotalCostQuery(filter, period)
			if err != nil {
				t.Fatalf("NewTotalCostQuery() error = %v", err)
			}
			if q.Filter() != filter {
				t.Errorf("Filter() = %+v, want %+v", q.Filter(), filter)
			}
			if q.Period() != period {
				t.Errorf("Period() = %+v, want %+v", q.Period(), period)
			}
		})
	}
}

// The calculation period is required. Its zero value has not been through its
// constructor, so the query rejects it with the error a zero month gives.
func TestNewTotalCostQuery_RejectsZeroPeriod(t *testing.T) {
	t.Parallel()

	// A filter that is set, so a query that kept it would not be the zero value.
	filter := domain.Filter{ServiceName: mustServiceName(t, "Yandex Plus")}
	q, err := domain.NewTotalCostQuery(filter, domain.CalculationPeriod{})
	if !errors.Is(err, domain.ErrInvalidMonth) {
		t.Fatalf("NewTotalCostQuery() error = %v, want %v", err, domain.ErrInvalidMonth)
	}
	if q != (domain.TotalCostQuery{}) {
		t.Errorf("NewTotalCostQuery() = %+v with an error, want the zero value", q)
	}
}

func mustCalculationPeriod(t *testing.T, from, to domain.MonthYear) domain.CalculationPeriod {
	t.Helper()

	p, err := domain.NewCalculationPeriod(from, to)
	if err != nil {
		t.Fatalf("NewCalculationPeriod() error = %v", err)
	}
	return p
}
