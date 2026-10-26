// Package charters carries the example charters a new installation starts with.
package charters

import "embed"

// Examples holds examples/*.yaml.
//
//go:embed examples/*.yaml
var Examples embed.FS
