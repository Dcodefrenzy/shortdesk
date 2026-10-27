// Package shortapps holds the apps every phone starts with, and the small library
// (sdk.js) every app's page uses to reach its data. Each app is a folder: app.json
// (what it is and what it keeps) and web/ (its page). See README.md.
package shortapps

import (
	"embed"
	"io/fs"
)

//go:embed */app.json */web
var files embed.FS

// SDK is shortapps.js, served next to every app's page.
//
//go:embed sdk.js
var SDK []byte

// Builtin returns the built-in apps: <id>/app.json, <id>/web/…
func Builtin() fs.FS { return files }
