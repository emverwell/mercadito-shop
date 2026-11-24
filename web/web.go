// Package web embeds the storefront's templates and static assets.
package web

import "embed"

// FS holds templates/ and static/.
//
//go:embed templates/*.html static/*
var FS embed.FS
