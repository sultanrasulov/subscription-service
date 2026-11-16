package domain

// CalculationPeriod is the range of months the total cost is calculated over,
// from its first to its last month, both inclusive.
//
// Unlike a SubscriptionPeriod, it always has both ends. Keeping the two types
// apart means a calculation period without an end cannot exist, so no caller
// has to check for one. The zero value is not a valid period.
type CalculationPeriod struct {
	from MonthYear
	to   MonthYear
}

// NewCalculationPeriod returns the period from from to to, both inclusive. It
// returns ErrInvalidMonth if either month is the zero value, and
// ErrInvertedCalculationPeriod if to is before from. From and to may be the
// same month.
func NewCalculationPeriod(from, to MonthYear) (CalculationPeriod, error) {
	if from.isZero() || to.isZero() {
		return CalculationPeriod{}, ErrInvalidMonth
	}
	if to.Before(from) {
		return CalculationPeriod{}, ErrInvertedCalculationPeriod
	}
	return CalculationPeriod{from: from, to: to}, nil
}

// From returns the first month of the period.
func (p CalculationPeriod) From() MonthYear { return p.from }

// To returns the last month of the period.
func (p CalculationPeriod) To() MonthYear { return p.to }
