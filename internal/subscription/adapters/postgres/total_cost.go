package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// The reader implements the whole port.
var _ domain.TotalCostReader = (*TotalCostReader)(nil)

// TotalCostReader reads the total cost of subscriptions from the
// subscriptions table. The database computes the sum in one query, so no
// subscription is loaded into memory.
type TotalCostReader struct {
	pool *pgxpool.Pool
}

// NewTotalCostReader returns a reader that runs its query on pool.
func NewTotalCostReader(pool *pgxpool.Pool) *TotalCostReader {
	return &TotalCostReader{pool: pool}
}

// TotalCost returns the total cost of the subscriptions that match q's filter
// over its calculation period, or 0 if none overlaps it.
func (r *TotalCostReader) TotalCost(ctx context.Context, q domain.TotalCostQuery) (int64, error) {
	query, args := totalCostQuery(q)
	var total int64
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("read total cost: %w", err)
	}
	return total, nil
}

// totalCostQuery returns the query for q's total cost, and its arguments: the
// first and the last month of the period as $1 and $2, then the filter's.
//
// Only subscriptions that overlap the period are read: one that starts after
// its last month or ends before its first adds nothing, and keeping it out of
// the sum means a month count is never negative. Each one adds its price
// times the months from the later of its start and the period's first month
// to the earlier of its end and the period's last month, both inclusive; an
// open-ended one counts until the period's last month.
//
// The month count is an integer, so the price is widened to bigint before it
// is multiplied: the largest price times two months already overflows
// integer. SUM over bigint is numeric, so it cannot overflow; the cast back to
// bigint fails with an error instead of losing precision if the total does not
// fit. COALESCE turns the NULL that SUM gives over no rows into 0.
func totalCostQuery(q domain.TotalCostQuery) (string, []any) {
	clause, args := where(q.Filter(),
		[]any{firstDay(q.Period().From()), firstDay(q.Period().To())},
		"start_date <= $2",
		"(end_date IS NULL OR end_date >= $1)",
	)
	months := fmt.Sprintf("%s - %s + 1",
		monthIndex("LEAST(COALESCE(end_date, $2), $2)"),
		monthIndex("GREATEST(start_date, $1)"),
	)
	return fmt.Sprintf(`SELECT COALESCE(SUM(price::bigint * (%s)), 0)::bigint FROM subscriptions%s`, months, clause), args
}

// monthIndex returns an integer expression that numbers the month of the date
// expr evaluates to, so that the difference of two indexes counts the months
// between them across years. EXTRACT returns numeric, hence the casts.
func monthIndex(expr string) string {
	return fmt.Sprintf("(EXTRACT(YEAR FROM %[1]s)::integer * 12 + EXTRACT(MONTH FROM %[1]s)::integer)", expr)
}
