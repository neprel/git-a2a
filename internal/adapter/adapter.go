package adapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/neprel/git-a2a/internal/manifest"
)

type Variant string
type Dependency = manifest.Dependency
type Export = manifest.Export
type Locked = manifest.LockedDependency

type Change struct {
	File, Entry, Warning string
	Changed              bool
}
type Finding struct {
	File, Entry, Want, Got string
	// Repairable distinguishes manager-owned lock/materialization drift from
	// declaration drift that may be a user edit. Pull may converge repairable
	// findings; it must stop before overwriting non-repairable findings.
	Repairable bool
}

type NotWirableError struct{ Reason string }

func (e NotWirableError) Error() string { return e.Reason }
func NotWirable(reason string) error    { return NotWirableError{Reason: reason} }
func IsNotWirable(err error) bool {
	var target NotWirableError
	return errors.As(err, &target)
}
func NotWirableReason(err error) string {
	var target NotWirableError
	if errors.As(err, &target) {
		return target.Reason
	}
	return fmt.Sprint(err)
}

type Adapter interface {
	Ecosystem() string
	Detect(root string) (bool, Variant, error)
	// Capability is a read-only check that rejects only source shapes this
	// concrete variant cannot represent. Tool and network checks are separate.
	Capability(root string, dep Dependency, exp Export) error
	// Pull edits the native declaration and makes the dependency usable locally.
	// It must also repair missing materialization when the declaration is current.
	Pull(ctx context.Context, root string, dep Dependency, exp Export, locked Locked) (Change, error)
	// Remove converges the native declaration, lock and project-local installed state.
	Remove(ctx context.Context, root string, dep Dependency, exp Export, locked Locked) (Change, error)
	// Inspect is local and read-only.
	Inspect(ctx context.Context, root string, dep Dependency, exp Export, locked Locked) ([]Finding, error)
}
