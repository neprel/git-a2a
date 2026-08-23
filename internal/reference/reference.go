package reference

import _ "embed"

// Manifest is the generated field reference embedded into the CLI.
//
//go:embed manifest-reference.md
var Manifest string
