package assets

import "embed"

//go:embed VERSION web/*.html dictionaries/*.json
var Files embed.FS
