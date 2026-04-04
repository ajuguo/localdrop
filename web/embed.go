package web

import "embed"

// Dist contains the built frontend assets. A placeholder index is tracked so
// the backend can compile before the first production build runs.
//
//go:embed all:dist
var Dist embed.FS
