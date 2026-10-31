// Package migrations embeds reviewed SQL release migrations.
package migrations

import "embed"

// Files contains migration bodies; runtime connection settings are not SQL migrations.
//
//go:embed *.sql
var Files embed.FS
