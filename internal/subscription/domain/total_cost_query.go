package domain

// TotalCostQuery asks for the total cost of the subscriptions that match a
// filter, over a calculation period.
//
// The period is required, so the query has a constructor and its fields are
// private: a zero period cannot reach a TotalCostReader.
type TotalCostQuery struct {
	filter Filter
	period CalculationPeriod
}

// NewTotalCostQuery returns the query, or ErrInvalidMonth if period is the
// zero value, as a zero month would give. A zero filter is valid and matches
// every subscription.
func NewTotalCostQuery(filter Filter, period CalculationPeriod) (TotalCostQuery, error) {
	if period == (CalculationPeriod{}) {
		return TotalCostQuery{}, ErrInvalidMonth
	}
	return TotalCostQuery{filter: filter, period: period}, nil
}

// Filter returns the filter the subscriptions must match.
func (q TotalCostQuery) Filter() Filter { return q.filter }

// Period returns the calculation period.
func (q TotalCostQuery) Period() CalculationPeriod { return q.period }
