package domain

// ListQuery asks for one page of the subscriptions that match Filter, ordered
// by ID.
//
// Limit and Offset are not checked here. The page is a property of the
// request, not of the model, so the code that reads the request validates it.
type ListQuery struct {
	Filter Filter
	Limit  int
	Offset int
}
