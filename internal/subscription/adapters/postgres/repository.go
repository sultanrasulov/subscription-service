// Package postgres is the driven adapter that stores subscriptions in
// PostgreSQL. It implements the domain's ports with hand-written SQL on pgx,
// and it is the only code that talks to the database.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// Repository stores subscriptions in the subscriptions table.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository returns a repository that runs its queries on pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// columns are the stored fields of a subscription, in the order scan reads
// them.
const columns = `id, user_id, service_name, price, start_date, end_date`

// Create stores a new subscription.
func (r *Repository) Create(ctx context.Context, s domain.Subscription) error {
	start, end := dates(s.Period())
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO subscriptions (`+columns+`) VALUES ($1, $2, $3, $4, $5, $6)`,
		s.ID().UUID(), s.UserID().UUID(), s.ServiceName().String(), s.Price().Rubles(), start, end,
	); err != nil {
		return fmt.Errorf("create subscription %s: %w", s.ID().UUID(), err)
	}
	return nil
}

// Get returns the subscription with the given ID.
func (r *Repository) Get(ctx context.Context, id domain.SubscriptionID) (domain.Subscription, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+columns+` FROM subscriptions WHERE id = $1`, id.UUID())
	s, err := scan(row)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("get subscription %s: %w", id.UUID(), err)
	}
	return s, nil
}

// Update stores s over the subscription with the same ID. It writes only what
// the aggregate can change, so the stored user is left as it is.
func (r *Repository) Update(ctx context.Context, s domain.Subscription) error {
	start, end := dates(s.Period())
	tag, err := r.pool.Exec(ctx,
		`UPDATE subscriptions
		SET service_name = $2, price = $3, start_date = $4, end_date = $5, updated_at = now()
		WHERE id = $1`,
		s.ID().UUID(), s.ServiceName().String(), s.Price().Rubles(), start, end,
	)
	if err != nil {
		return fmt.Errorf("update subscription %s: %w", s.ID().UUID(), err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update subscription %s: %w", s.ID().UUID(), domain.ErrNotFound)
	}
	return nil
}

// Delete removes the subscription with the given ID and returns it, read from
// the deleted row itself, so nothing can change it in between.
//
// The delete runs in a transaction that rolls back when the row cannot be
// built into a subscription. Otherwise the row would already be gone while
// Delete reported an error, and an error must mean that nothing changed.
func (r *Repository) Delete(ctx context.Context, id domain.SubscriptionID) (domain.Subscription, error) {
	var deleted domain.Subscription
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		s, err := scan(tx.QueryRow(ctx, `DELETE FROM subscriptions WHERE id = $1 RETURNING `+columns, id.UUID()))
		deleted = s
		return err
	})
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("delete subscription %s: %w", id.UUID(), err)
	}
	return deleted, nil
}

// scan reads one row of columns and builds the subscription through the
// domain's constructors, so a row that breaks an invariant is an error, not a
// loaded subscription. No row is domain.ErrNotFound.
func scan(row pgx.Row) (domain.Subscription, error) {
	var (
		id, user uuid.UUID
		name     string
		rubles   int64
		start    time.Time
		end      *time.Time
	)
	if err := row.Scan(&id, &user, &name, &rubles, &start, &end); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Subscription{}, domain.ErrNotFound
		}
		return domain.Subscription{}, err
	}

	s, err := build(id, user, name, rubles, start, end)
	if err != nil {
		// The ID names the row: a list fails on it without saying which one.
		return domain.Subscription{}, fmt.Errorf("invalid stored row %s: %w", id, err)
	}
	return s, nil
}

func build(id, user uuid.UUID, name string, rubles int64, start time.Time, end *time.Time) (domain.Subscription, error) {
	subscriptionID, err := domain.NewSubscriptionID(id)
	if err != nil {
		return domain.Subscription{}, err
	}
	userID, err := domain.NewUserID(user)
	if err != nil {
		return domain.Subscription{}, err
	}
	serviceName, err := domain.NewServiceName(name)
	if err != nil {
		return domain.Subscription{}, err
	}
	price, err := domain.NewPrice(rubles)
	if err != nil {
		return domain.Subscription{}, err
	}
	period, err := periodOf(start, end)
	if err != nil {
		return domain.Subscription{}, err
	}
	return domain.NewSubscription(subscriptionID, userID, serviceName, price, period)
}

func periodOf(start time.Time, end *time.Time) (domain.SubscriptionPeriod, error) {
	from, err := domain.NewMonthYear(start.Year(), start.Month())
	if err != nil {
		return domain.SubscriptionPeriod{}, err
	}
	if end == nil {
		return domain.NewOpenEndedSubscriptionPeriod(from)
	}
	to, err := domain.NewMonthYear(end.Year(), end.Month())
	if err != nil {
		return domain.SubscriptionPeriod{}, err
	}
	return domain.NewSubscriptionPeriod(from, to)
}

// dates returns a period as the stored dates: the 1st of each month, and no
// end date for an open-ended period. pgx stores a date from the value's own
// year, month and day, so its time zone cannot move it; UTC matches the
// values pgx returns when it reads a date.
func dates(p domain.SubscriptionPeriod) (time.Time, *time.Time) {
	start := firstDay(p.Start())
	end, ok := p.End()
	if !ok {
		return start, nil
	}
	last := firstDay(end)
	return start, &last
}

func firstDay(m domain.MonthYear) time.Time {
	return time.Date(m.Year(), m.Month(), 1, 0, 0, 0, 0, time.UTC)
}
