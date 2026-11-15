package pgtest_test

import (
	"strings"
	"testing"

	"github.com/sultanrasulov/subscription-service/internal/platform/postgres/pgtest"
)

// The tests must run on the image the stack runs, read from compose.yaml, the
// single source of that version.
func TestImage_ReadsComposeFile(t *testing.T) {
	t.Parallel()

	img, err := pgtest.Image()
	if err != nil {
		t.Fatalf("Image() error = %v", err)
	}
	if !strings.HasPrefix(img, "postgres:17.") {
		t.Errorf("Image() = %q, want a postgres:17.x image", img)
	}
}
