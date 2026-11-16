package domain

import "math"

// MaxPrice is the highest monthly price in rubles: the largest value the
// database's integer column holds.
const MaxPrice = math.MaxInt32

// Price is a monthly price in whole rubles, from 0 to MaxPrice. There are no
// kopecks.
//
// A price of 0 is valid, for example a free trial, so the zero value is a
// price of 0 rubles.
type Price struct {
	rubles int32
}

// NewPrice returns a price of the given number of rubles, or
// ErrPriceOutOfRange if it is below 0 or above MaxPrice.
//
// It takes an int64, so any integer a caller parsed is checked here and an
// out-of-range value is reported as such instead of overflowing on the way
// in.
func NewPrice(rubles int64) (Price, error) {
	if rubles < 0 || rubles > MaxPrice {
		return Price{}, ErrPriceOutOfRange
	}
	return Price{rubles: int32(rubles)}, nil
}

// Rubles returns the price in whole rubles.
func (p Price) Rubles() int64 { return int64(p.rubles) }
