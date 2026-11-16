package domain

import "context"

// TotalCostReader reads the total cost of subscriptions. The database adapter
// implements it, and the database computes the sum in one query instead of
// loading every subscription.
type TotalCostReader interface {
	// TotalCost returns, in whole rubles, the total cost of the subscriptions
	// that match the query's filter over its calculation period, or 0 if none
	// matches.
	//
	// Each subscription adds its price times the number of months its period
	// overlaps the calculation period, both ends inclusive. An open-ended
	// subscription counts until the last month of the calculation period.
	TotalCost(ctx context.Context, q TotalCostQuery) (int64, error)
}
