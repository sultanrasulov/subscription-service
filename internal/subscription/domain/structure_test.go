package domain_test

import (
	"reflect"
	"testing"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// Value objects and the aggregate keep their state private and hold it by
// value. Then nothing outside the package can change a value after its
// constructor validated it, and a copy never shares state with the original.
// A pointer field would break both: whoever kept the pointer could change the
// value later and bypass its invariants.
func TestTypes_HoldPrivateValuesOnly(t *testing.T) {
	t.Parallel()

	for _, v := range []any{
		domain.MonthYear{},
		domain.Price{},
		domain.UserID{},
		domain.ServiceName{},
		domain.SubscriptionPeriod{},
		domain.CalculationPeriod{},
		domain.Subscription{},
	} {
		typ := reflect.TypeOf(v)
		t.Run(typ.Name(), func(t *testing.T) {
			t.Parallel()

			checkPrivateValues(t, typ, typ.Name())
		})
	}
}

// checkPrivateValues walks typ through its struct fields and array elements
// and reports every exported field and every kind that refers to shared state.
func checkPrivateValues(t *testing.T, typ reflect.Type, path string) {
	t.Helper()

	switch typ.Kind() {
	case reflect.Pointer, reflect.UnsafePointer, reflect.Slice, reflect.Map,
		reflect.Chan, reflect.Func, reflect.Interface:
		t.Errorf("%s has kind %s, which shares state between copies", path, typ.Kind())
	case reflect.Array:
		checkPrivateValues(t, typ.Elem(), path+"[]")
	case reflect.Struct:
		for i := range typ.NumField() {
			field := typ.Field(i)
			fieldPath := path + "." + field.Name
			if field.IsExported() {
				t.Errorf("%s is exported, so it can be changed without validation", fieldPath)
			}
			checkPrivateValues(t, field.Type, fieldPath)
		}
	default:
		// Booleans, numbers and strings are copied with the value.
	}
}
