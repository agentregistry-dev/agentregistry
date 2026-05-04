package builtin

import "embed"

// FS holds every in-tree plugin directory. Each subdirectory must contain a plugin.yaml.
//
//go:embed all:*
var FS embed.FS
