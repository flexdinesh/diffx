// Package assets embeds only the staged desktop frontend.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var built embed.FS

func FS() fs.FS {
	assets, err := fs.Sub(built, "dist")
	if err != nil {
		panic(err)
	}
	return assets
}
