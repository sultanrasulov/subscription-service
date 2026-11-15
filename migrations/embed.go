// Package migrations embeds the versioned SQL migrations.
//
// Each file wraps its statements in BEGIN … COMMIT, so a failed migration
// leaves no half-changed schema behind.
package migrations

import "embed"

// FS holds the migration files at its root.
//
//go:embed *.sql
var FS embed.FS
