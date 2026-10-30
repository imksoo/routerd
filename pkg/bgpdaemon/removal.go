// SPDX-License-Identifier: BSD-3-Clause

package bgpdaemon

import (
	"os"
	"path/filepath"
)

// RemovalPending is the controller-owned pending operation, separate from the
// live daemon's applied-state writer. Its presence fences restoration during a
// process or controller restart before cleanup completes.
func RemovalPending(statePath string) (bool, error) {
	_, err := os.Stat(statePath + ".remove-pending")
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func BeginRemoval(statePath string) error {
	if err := os.MkdirAll(filepath.Dir(statePath), 0755); err != nil {
		return err
	}
	file, err := os.OpenFile(statePath+".remove-pending", os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return syncRemovalDirectory(statePath)
}

// CompleteRemoval runs only after acknowledged process and FIB cleanup. Sync
// removal of the old restart state before removing its protective intent.
func CompleteRemoval(statePath string) error {
	if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := syncRemovalDirectory(statePath); err != nil {
		return err
	}
	if err := os.Remove(statePath + ".remove-pending"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncRemovalDirectory(statePath)
}

func syncRemovalDirectory(statePath string) error {
	dir, err := os.Open(filepath.Dir(statePath))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
