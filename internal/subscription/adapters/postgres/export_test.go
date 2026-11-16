package postgres

// CountQuery exposes the count query to the external tests, which check its
// plan against the real database.
var CountQuery = countQuery
