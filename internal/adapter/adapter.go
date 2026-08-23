package adapter

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/neprel/git-a2a/internal/manifest"
)

// VendorSourcePath returns the consumer-root-relative directory that contains an export.
// Copy vendors already materialise dependency.path as their root; submodules retain it.
func VendorSourcePath(exp Export, locked Locked) string {
	if locked.Vendor == nil {
		return ""
	}
	parts := []string{locked.Vendor.Path}
	if locked.Vendor.Mode == "submodule" && locked.Path != "" && locked.Path != "." {
		parts = append(parts, locked.Path)
	}
	if exp.Path != "" && exp.Path != "." {
		parts = append(parts, exp.Path)
	}
	return filepath.ToSlash(filepath.Join(parts...))
}

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
