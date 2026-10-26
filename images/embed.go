// Package images carries the build recipes (Dockerfiles and their files) for the images
// Shortdesk needs, so a single shortdesk binary can build them on any machine.
package images

import "embed"

// FS holds desk/ (the desk image) and vault-cli/ (the Bitwarden CLI helper).
//
//go:embed all:desk all:vault-cli
var FS embed.FS
