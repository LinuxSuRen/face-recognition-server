// Package httpui embeds the built Vue application so the server binary can
// serve its own UI.
//
// The real bundle lands here during release builds (see the Dockerfile and
// CI, which copy web/dist into this directory). A placeholder index.html is
// committed so plain `go build` works without a frontend toolchain.
package httpui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// FS returns the bundle rooted at its index.html, or nil when the build
// carries no UI at all.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil
	}
	return sub
}

// Index returns the SPA entry point for client-route fallbacks.
func Index() ([]byte, error) {
	return embedded.ReadFile("dist/index.html")
}
