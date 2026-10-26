// Package webui embeds the built dashboard (web/out, copied here by `make web`), so the
// shortdesk binary serves it without any files next to it.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the dashboard's files.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
