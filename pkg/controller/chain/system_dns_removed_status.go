// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"fmt"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/state"
)

func (c SystemdUnitController) cleanupRemovedDNSResolverStatuses() error {
	store, ok := c.Store.(interface {
		state.ObjectStatusLister
		state.ObjectDeleteStore
	})
	if !ok {
		return fmt.Errorf("DNS resolver status cleanup store unavailable")
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
		if resource.Kind == "DNSResolver" {
			wanted[resource.Metadata.Name] = true
		}
	}
	removed := map[string]bool{}
	for _, object := range statuses {
		if object.APIVersion == api.SystemAPIVersion && object.Kind == "ServiceUnit" && object.Status["phase"] == "Removed" {
			removed[object.Name] = true
		}
	}
	for _, object := range statuses {
		if object.APIVersion != api.NetAPIVersion || object.Kind != "DNSResolver" || wanted[object.Name] || !removed[dnsResolverUnitName(object.Name)] {
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
