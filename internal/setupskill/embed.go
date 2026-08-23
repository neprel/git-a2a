// Package setupskill exposes the portable git-a2a skill bundled with the CLI.
package setupskill

import "embed"

// Files contains the thin repository-installed skill. Full progressively disclosed references
// remain in skills/git-a2a and in the npm/site distributions; setup points to those canonical
// sources rather than copying a documentation snapshot into every consumer repository.
//
//go:embed thin/* thin/references/*
var Files embed.FS
