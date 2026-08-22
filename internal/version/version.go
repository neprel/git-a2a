// Package version owns the release version embedded in every build.
package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var source string

// Current returns the release version without a leading v.
func Current() string { return strings.TrimSpace(source) }
