// Package web embeds the frontend build, produced before go build by make build.
package web

import (
	"embed"
	"io/fs"
)

// Embedding the directory (not dist/*) permits a clean source checkout: the
// marker keeps the directory present; production builds replace its contents.
//
//go:embed all:dist
var files embed.FS

func Assets() fs.FS { f, _ := fs.Sub(files, "dist"); return f }
