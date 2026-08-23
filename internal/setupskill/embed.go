// Package setupskill exposes the portable git-a2a skill bundled with the CLI.
package setupskill

import "embed"

// Files contains SKILL.md and its progressively disclosed references.
//
//go:embed files/* files/references/*
var Files embed.FS
