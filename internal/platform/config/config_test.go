package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/sultanrasulov/subscription-service/internal/platform/config"
)

// base returns the smallest valid environment: only the required variables.
func base() map[string]string {
	return map[string]string{
		"POSTGRES_HOST":     "db",
		"POSTGRES_USER":     "app",
		"POSTGRES_PASSWORD": "secret",
		"POSTGRES_DB":       "subscriptions",
	}
}

// with returns base() with the given name/value pairs set on top.
func with(kv ...string) map[string]string {
	env := base()
	for i := 0; i+1 < len(kv); i += 2 {
		env[kv[i]] = kv[i+1]
	}
	return env
}

// invalid is one rejected variable value and the text its error must contain.
type invalid struct {
	variable string
	value    string
	want     string
}

// checkRejected loads base() with each case's variable set, and checks that
// the error names the variable and contains the case's expected text.
func checkRejected(t *testing.T, cases []invalid) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.variable+"="+tc.value, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(with(tc.variable, tc.value))
			if err == nil {
				t.Fatalf("Load() error = nil, want an error naming %s", tc.variable)
			}
			if !strings.Contains(err.Error(), tc.variable+":") {
				t.Errorf("error does not name %s:\n%v", tc.variable, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not contain %q:\n%v", tc.want, err)
			}
		})
	}
}

func TestLoad_ReportsEveryProblemAtOnce(t *testing.T) {
	t.Parallel()

	_, err := config.Load(map[string]string{
		"HTTP_READ_TIMEOUT": "abc",
		"POSTGRES_PORT":     "x",
	})
	if err == nil {
		t.Fatal("Load() error = nil, want an error")
	}
	for _, name := range []string{
		"POSTGRES_HOST", "POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB",
		"HTTP_READ_TIMEOUT", "POSTGRES_PORT",
	} {
		if !strings.Contains(err.Error(), name+":") {
			t.Errorf("error does not name %s:\n%v", name, err)
		}
	}
}

func TestLoad_ReportsEveryValidationProblemAtOnce(t *testing.T) {
	t.Parallel()

	_, err := config.Load(with(
		"HTTP_ADDR", "8080",
		"POSTGRES_SSLMODE", "on",
	))
	if err == nil {
		t.Fatal("Load() error = nil, want an error")
	}
	for _, name := range []string{"HTTP_ADDR", "POSTGRES_SSLMODE"} {
		if !strings.Contains(err.Error(), name+":") {
			t.Errorf("error does not name %s:\n%v", name, err)
		}
	}
}

func TestLoad_ErrorNeverContainsThePassword(t *testing.T) {
	t.Parallel()

	_, err := config.Load(with(
		"POSTGRES_PASSWORD", "supersecret",
		"LOG_LEVEL", "verbose",
		"POSTGRES_SSLMODE", "on",
		"HTTP_ADDR", "8080",
	))
	if err == nil {
		t.Fatal("Load() error = nil, want an error")
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Errorf("error leaks the password:\n%v", err)
	}
}

// The env library reads the process environment when given a nil map, which
// would make Load depend on whatever the test runner or CI happens to export.
// t.Setenv rules out t.Parallel for this test.
func TestLoad_NilEnvironIsEmpty(t *testing.T) {
	t.Setenv("POSTGRES_HOST", "from-process")

	_, err := config.Load(nil)
	if err == nil || !strings.Contains(err.Error(), "POSTGRES_HOST:") {
		t.Errorf("Load(nil) error = %v, want POSTGRES_HOST reported missing", err)
	}
}

// Load names a variable in a parse error through one flat map from field name
// to variable name, so a field name repeated in two sections would make the
// error name the wrong variable.
func TestConfig_FieldNamesAreUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]string)
	cfg := reflect.TypeFor[config.Config]()
	for i := range cfg.NumField() {
		section := cfg.Field(i)
		for j := range section.Type.NumField() {
			name := section.Type.Field(j).Name
			if other, ok := seen[name]; ok {
				t.Errorf("field %s is in both %s and %s", name, other, section.Name)
			}
			seen[name] = section.Name
		}
	}
}
