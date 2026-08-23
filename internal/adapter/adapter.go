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
type Finding struct{ File, Entry, Want, Got string }

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
	Wire(ctx context.Context, root string, dep Dependency, exp Export, locked Locked) (Change, error)
	Unwire(ctx context.Context, root string, dep Dependency, exp Export) (Change, error)
	Refresh(ctx context.Context, root string, dep Dependency, exp Export, locked Locked) error
	Drift(ctx context.Context, root string, dep Dependency, exp Export, locked Locked) ([]Finding, error)
}
