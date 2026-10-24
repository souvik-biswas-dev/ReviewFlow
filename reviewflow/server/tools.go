//go:build tools

// Package tools pins the gqlgen code generator in go.mod so that
// `go run github.com/99designs/gqlgen generate` always uses the same version.
package tools

import (
	_ "github.com/99designs/gqlgen"
	_ "github.com/99designs/gqlgen/graphql/introspection"
)
