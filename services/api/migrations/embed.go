package migrations

import "embed"

// Files contains schema only, never fixtures.
//
//go:embed *.sql
var Files embed.FS
