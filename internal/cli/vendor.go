package cli

import (
	"fmt"

	"github.com/neprel/git-a2a/internal/manifest"
	vendortransport "github.com/neprel/git-a2a/internal/vendor"
)

func (a *App) applyVendorTransition(root string, own *manifest.Manifest, oldDep *manifest.Dependency, nextDep manifest.Dependency, oldEntry *manifest.LockedDependency, nextEntry *manifest.LockedDependency, force bool) (func() error, error) {
	manager := vendortransport.Manager{Runner: a.runner()}
	oldVendored := oldDep != nil && oldDep.Vendor != nil && oldEntry != nil && oldEntry.Vendor != nil
	nextVendored := nextDep.Vendor != nil
	changedRepresentation := oldVendored && nextVendored && (vendortransport.Mode(*oldDep) != vendortransport.Mode(nextDep) || oldEntry.Vendor.Path != vendortransport.Path(own, nextDep))

	restoreOld := func() error {
		if !oldVendored {
			return nil
		}
		_, err := manager.ApplyTransition(a.context(), root, own, *oldDep, *oldEntry, nil, true)
		return err
	}
	removeNext := func() error {
		if !nextVendored || nextEntry.Vendor == nil {
			return nil
		}
		return manager.Remove(a.context(), root, nextDep, *nextEntry, true)
	}
	rollback := func() error {
		var first error
		if err := removeNext(); err != nil {
			first = err
		}
		if err := restoreOld(); err != nil && first == nil {
			first = err
		}
		return first
	}

	if oldVendored && (!nextVendored || changedRepresentation) {
		if err := manager.Remove(a.context(), root, *oldDep, *oldEntry, force); err != nil {
			return nil, err
		}
	}
	if !nextVendored {
		nextEntry.Vendor = nil
		return rollback, nil
	}
	var previous *manifest.LockedDependency
	if oldVendored && !changedRepresentation {
		previous = oldEntry
	}
	lockedVendor, err := manager.ApplyTransition(a.context(), root, own, nextDep, *nextEntry, previous, force)
	if err != nil {
		if restoreErr := restoreOld(); restoreErr != nil {
			return nil, fmt.Errorf("%w; restoring previous vendor failed: %v", err, restoreErr)
		}
		return nil, err
	}
	nextEntry.Vendor = lockedVendor
	return rollback, nil
}
