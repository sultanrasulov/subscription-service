// Package docs embeds the generated Swagger specification.
//
// The specification is produced by `make swagger` and is not committed.
// On a clean clone the directory holds only a placeholder, so the service
// still builds and serves a 404 for the specification (design §9).
package docs

import "embed"

// Swagger holds the swagger directory. Its root contains swagger.json once
// the specification has been generated.
//
// The all: prefix is required: without it go:embed ignores dotfiles, and a
// directory holding only .gitkeep would fail to compile.
//
//go:embed all:swagger
var Swagger embed.FS
