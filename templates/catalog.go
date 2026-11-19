// Package templates embeds the provisional specialty definitions used by the initial catalog.
package templates

import "embed"

//go:embed *.provisional.json
var Provisional embed.FS
