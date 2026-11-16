package domain

// SubscriptionPeriod is when a subscription is active: from its start month
// to its end month, both inclusive, or with no end month at all, in which
// case the subscription is open-ended.
//
// It is held entirely by value, so once a constructor has checked that the
// end is not before the start, nothing can change either month. Two periods
// compare equal with == when they have the same start and the same end, or
// are both open-ended. The zero value is not a valid period.
type SubscriptionPeriod struct {
	start MonthYear
	// The zero value means "no end month". It cannot be mistaken for a real
	// month, because the zero MonthYear is invalid.
	end MonthYear
}

// NewSubscriptionPeriod returns the period from start to end, both inclusive.
// It returns ErrInvalidMonth if either month is the zero value, and
// ErrEndBeforeStart if end is before start. Start and end may be the same
// month.
func NewSubscriptionPeriod(start, end MonthYear) (SubscriptionPeriod, error) {
	if start.isZero() || end.isZero() {
		return SubscriptionPeriod{}, ErrInvalidMonth
	}
	if end.Before(start) {
		return SubscriptionPeriod{}, ErrEndBeforeStart
	}
	return SubscriptionPeriod{start: start, end: end}, nil
}

// NewOpenEndedSubscriptionPeriod returns the period from start with no end
// month. It returns ErrInvalidMonth if start is the zero value.
func NewOpenEndedSubscriptionPeriod(start MonthYear) (SubscriptionPeriod, error) {
	if start.isZero() {
		return SubscriptionPeriod{}, ErrInvalidMonth
	}
	return SubscriptionPeriod{start: start}, nil
}

// Start returns the first month of the period.
func (p SubscriptionPeriod) Start() MonthYear { return p.start }

// End returns the last month of the period and true, or the zero MonthYear
// and false if the period is open-ended.
func (p SubscriptionPeriod) End() (MonthYear, bool) { return p.end, !p.end.isZero() }
