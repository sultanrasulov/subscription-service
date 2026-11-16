package domain

// Filter selects the subscriptions that a list or a total cost covers. List
// and total cost read it the same way.
//
// A field that is set must match, and the set fields combine with AND. A zero
// field means no filter on it; it cannot be mistaken for a real value, because
// the zero UserID and the zero ServiceName are not valid. The zero Filter
// therefore matches every subscription. The service name matches
// case-insensitively, where subscriptions are stored.
type Filter struct {
	UserID      UserID
	ServiceName ServiceName
}
