// Package valueobject holds the value objects that every bounded context may
// use.
//
// A value object belongs here only if it is immutable, is validated by its
// constructor, compares by value, and means the same in every context.
// Serialization does not belong here: adapters convert values explicitly. A
// helper that is not a value object does not belong here either; it lives with
// its caller.
//
// The package depends only on the standard library and a UUID library.
package valueobject
