package web

import (
	"embed"
)

// StaticFS embeds the Vue build output. During development, the dist/
// directory may be empty — run `npm run build` in web/ to populate it.
//
//go:embed all:dist
var StaticFS embed.FS
