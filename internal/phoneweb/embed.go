// Package phoneweb embeds the built phone (web built with SHORTDESK_BUILD=phone, copied
// here by `make web`), which the Phone Desk serves on the main desk.
package phoneweb

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

//go:embed placeholder
var placeholder embed.FS

// FS returns the phone's files, or a page explaining how to build them.
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
