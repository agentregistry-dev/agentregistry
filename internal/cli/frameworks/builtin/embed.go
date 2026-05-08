package builtin

import "embed"

// FS holds every in-tree framework directory. Each subdirectory must contain a framework.yaml.
//
//go:embed all:*
var FS embed.FS
