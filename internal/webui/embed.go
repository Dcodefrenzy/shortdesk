// Package webui embeds the built dashboard (web/out, copied here by `make web`), so the
// shortdesk binary serves it without any files next to it.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// placeholder is served by builds made without the dashboard.
//
//go:embed placeholder
var placeholder embed.FS

// FS returns the dashboard's files, or a page explaining how to build them.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	if _, err := fs.Stat(sub, "index.html"); err == nil {
		return sub
	}
	sub, err = fs.Sub(placeholder, "placeholder")
	if err != nil {
		panic(err)
	}
	return sub
}
