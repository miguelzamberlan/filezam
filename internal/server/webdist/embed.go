// Package webdist embeds the built frontend (web/dist copied here at build time).
package webdist

import "embed"

// FS contains the Vite build output under dist/.
//
//go:embed all:dist
var FS embed.FS
