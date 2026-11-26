package migrations

import "embed"

// Files embeds the versioned MySQL business schema.
//
//go:embed mysql/*.sql
var Files embed.FS
