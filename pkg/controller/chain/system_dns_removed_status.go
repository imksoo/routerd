// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"fmt"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/state"
)

// Retire public status only after the owning supervisor acknowledged removal.
func (c SystemdUnitController) cleanupRemovedSystemdResourceStatuses(kind string, unitName func(string) string) error {
	if c.DryRun {
		return nil
	}
	store, ok := c.Store.(interface {
		state.ObjectStatusLister
		state.ObjectDeleteStore
	})
	if !ok {
		return fmt.Errorf("%s status cleanup store unavailable", kind)
	}
	statuses, err := store.ListObjectStatuses()
	if err != nil {
		return err
	}
	declared := c.DeclaredRouter
	if declared == nil {
		declared = c.Router
	}
	wanted := map[string]bool{}
	for _, resource := range declared.Spec.Resources {
		if resource.Kind == kind {
			wanted[resource.Metadata.Name] = true
		}
	}
	removed := map[string]bool{}
	for _, object := range statuses {
		if object.APIVersion == api.SystemAPIVersion && object.Kind == "ServiceUnit" && object.Status["phase"] == "Removed" && object.Status["dryRun"] != true {
			removed[object.Name] = true
		}
	}
	for _, object := range statuses {
		if object.APIVersion != api.NetAPIVersion || object.Kind != kind || wanted[object.Name] || !removed[unitName(object.Name)] {
			continue
		}
		// The service owner publishes Removed only after disable --now,
		// artifact removal and daemon-reload succeed. Keep that acknowledgement
		// so a failed status deletion can retry even after the file is gone.
		// Missing/failed acknowledgements do not authorize status removal.
		if err := store.DeleteObject(object.APIVersion, object.Kind, object.Name); err != nil {
			return err
		}
	}
	return nil
}
