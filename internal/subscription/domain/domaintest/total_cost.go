package domaintest

import (
	"context"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// TotalCostReader is an in-memory domain.TotalCostReader over the
// subscriptions of a Repository, as the database adapter reads the table the
// repository writes.
type TotalCostReader struct {
	store *Repository
}

// The fake implements the whole port.
var _ domain.TotalCostReader = (*TotalCostReader)(nil)

// NewTotalCostReader returns a reader over store's subscriptions.
func NewTotalCostReader(store *Repository) *TotalCostReader {
	return &TotalCostReader{store: store}
}

// TotalCost returns the total cost of the subscriptions that match q's filter
// over its calculation period, or 0 if none overlaps it: each adds its price
// times the months its period shares with the calculation period, both ends
// inclusive, and an open-ended one counts until the period's last month. It
// goes through the store's Fail and Block, as one database serves both ports.
func (r *TotalCostReader) TotalCost(ctx context.Context, q domain.TotalCostQuery) (int64, error) {
	if err := r.store.begin(ctx); err != nil {
		return 0, err
	}
	defer r.store.mu.Unlock()

	from, to := monthIndex(q.Period().From()), monthIndex(q.Period().To())
	var total int64
	for _, s := range r.store.subscriptions {
		if !matchesFilter(s, q.Filter()) {
			continue
		}
		first := max(monthIndex(s.Period().Start()), from)
		last := to
		if end, ok := s.Period().End(); ok {
			last = min(monthIndex(end), to)
		}
		if first <= last {
			total += s.Price().Rubles() * int64(last-first+1)
		}
	}
	return total, nil
}

// monthIndex numbers a month, so that the difference of two indexes counts
// the months between them across years.
func monthIndex(m domain.MonthYear) int {
	return m.Year()*12 + int(m.Month())
}
