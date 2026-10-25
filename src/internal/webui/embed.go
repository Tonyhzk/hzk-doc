package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var files embed.FS

func Files() (fs.FS, error) {
	return fs.Sub(files, "dist")
}
