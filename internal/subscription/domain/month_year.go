package domain

import "time"

// The range of years every month must fall in. Online subscriptions do not
// predate 2000, so earlier years are placeholders such as 01-1970 (the Unix
// epoch) or typos such as 0225, and so are years after 2100, such as 2205. The
// range also caps a subscription at 1 212 months, which bounds its total cost.
const (
	MinYear = 2000
	MaxYear = 2100
)

// MonthYear is a calendar month, the only granularity the service works at.
//
// The zero value is not a valid month; NewMonthYear is the only way to get
// one. Values are ordered with Before and compare equal with ==.
type MonthYear struct {
	// Two fields rather than one month index, so the zero value has month 0
	// and fails as an invalid month.
	year  int
	month time.Month
}

// NewMonthYear returns the given month of the given year. It returns
// ErrInvalidMonth for a month outside January to December, and otherwise
// ErrYearOutOfRange for a year outside MinYear to MaxYear.
func NewMonthYear(year int, month time.Month) (MonthYear, error) {
	if month < time.January || month > time.December {
		return MonthYear{}, ErrInvalidMonth
	}
	if year < MinYear || year > MaxYear {
		return MonthYear{}, ErrYearOutOfRange
	}
	return MonthYear{year: year, month: month}, nil
}

// Year returns the year, from MinYear to MaxYear.
func (m MonthYear) Year() int { return m.year }

// Month returns the month of the year.
func (m MonthYear) Month() time.Month { return m.month }

// Before reports whether m is an earlier month than other.
func (m MonthYear) Before(other MonthYear) bool {
	if m.year != other.year {
		return m.year < other.year
	}
	return m.month < other.month
}
