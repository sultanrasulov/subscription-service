package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// The repository implements the whole port.
var _ domain.Repository = (*Repository)(nil)

// List returns the page of subscriptions that match the query's filter,
// ordered by ID, and the number of all of them.
//
// The total comes from a separate count with the same filter, not from a
// window function over the page: past the last page no row comes back, and
// the total would be lost with it.
func (r *Repository) List(ctx context.Context, q domain.ListQuery) ([]domain.Subscription, int, error) {
	query, args := listQuery(q)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list subscriptions: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Subscription, error) {
		return scan(row)
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list subscriptions: %w", err)
	}

	var total int
	query, args = countQuery(q.Filter)
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count subscriptions: %w", err)
	}
	return items, total, nil
}

// listQuery returns the query for one page of the subscriptions that match
// q's filter, and its arguments.
func listQuery(q domain.ListQuery) (string, []any) {
	clause, args := where(q.Filter, nil)
	args = append(args, q.Limit, q.Offset)
	return fmt.Sprintf(`SELECT %s FROM subscriptions%s ORDER BY id LIMIT $%d OFFSET $%d`,
		columns, clause, len(args)-1, len(args)), args
}

// countQuery returns the query that counts the subscriptions matching f, and
// its arguments.
func countQuery(f domain.Filter) (string, []any) {
	clause, args := where(f, nil)
	return `SELECT count(*) FROM subscriptions` + clause, args
}

// where turns a filter into a WHERE clause and its arguments; list and total
// cost share it, so they always select the same subscriptions.
//
// A caller with conditions of its own passes them with the arguments they
// refer to; the filter's conditions follow them and number their parameters
// after those arguments.
//
// A condition is added only for a filter that is set. The alternative, "$1 IS
// NULL OR user_id = $1", can keep the planner from using an index once a
// prepared statement switches to a generic plan. Values are always bound
// parameters, never part of the text.
//
// The name is lower-cased under pg_c_utf8 on both sides. The column's collation
// covers only the column's side; the parameter would otherwise be lower-cased
// under the database's default, which leaves Cyrillic unchanged in a C-locale
// database.
func where(f domain.Filter, args []any, conditions ...string) (string, []any) {
	if !f.UserID.IsZero() {
		args = append(args, f.UserID.UUID())
		conditions = append(conditions, fmt.Sprintf("user_id = $%d", len(args)))
	}
	if !f.ServiceName.IsZero() {
		args = append(args, f.ServiceName.String())
		conditions = append(conditions, fmt.Sprintf("lower(service_name) = lower($%d COLLATE pg_c_utf8)", len(args)))
	}
	if len(conditions) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}
