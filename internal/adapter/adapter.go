package adapter

import (
	"context"

	"github.com/neprel/git-a2a/internal/manifest"
)

type Variant string
type Dependency = manifest.Dependency
type Export = manifest.Export
type Locked = manifest.LockedDependency

type Change struct {
	File, Entry string
	Changed     bool
}
type Finding struct{ File, Entry, Want, Got string }

type Adapter interface {
	Ecosystem() string
	Detect(root string) (bool, Variant, error)
	Wire(ctx context.Context, root string, dep Dependency, exp Export, locked Locked) (Change, error)
	Unwire(ctx context.Context, root string, dep Dependency, exp Export) (Change, error)
	Refresh(ctx context.Context, root string, dep Dependency, exp Export, locked Locked) error
	Drift(ctx context.Context, root string, dep Dependency, exp Export, locked Locked) ([]Finding, error)
}
