// Package webassets exposes web resources embedded in the application binary.
package webassets

import "embed"

// Files contains server-rendered templates and static assets.
//
//go:embed templates/*.html static/*
var Files embed.FS
