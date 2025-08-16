package web

import (
	"embed"
	"html/template"
	"io/fs"
)

// Embedded webroot assets - included at build time
var (
	// HTML Templates
	//go:embed webroot/templates/*.html
	templatesFS embed.FS

	// Static assets (CSS, JS, images)
	//go:embed webroot/static/*
	staticFS embed.FS
)

// GetTemplates loads and parses embedded HTML templates
func GetTemplates() (*template.Template, error) {
	return template.ParseFS(templatesFS, "webroot/templates/*.html")
}

// GetStaticFS returns the embedded static file system
func GetStaticFS() fs.FS {
	staticSubFS, err := fs.Sub(staticFS, "webroot/static")
	if err != nil {
		panic("failed to create static subdirectory: " + err.Error())
	}
	return staticSubFS
}
