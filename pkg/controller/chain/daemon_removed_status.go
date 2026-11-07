// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"fmt"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/state"
)

// Called only after the supervisor has acknowledged all obsolete child stops,
// removed their ownership records, and joined obsolete event readers. Address
// and route receipts belong to their native controllers, not this cleanup.
func (r *Runner) cleanupRemovedDaemonStatuses() error {
	declared := r.currentRouter()
	if declared == nil || r.Store == nil {
		return nil
	}
	store, ok := r.Store.(interface {
		state.ObjectStatusLister
		state.ObjectDeleteStore
	})
	if !ok {
		return fmt.Errorf("daemon status cleanup store unavailable")
	}
	statuses, err := store.ListObjectStatuses()
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, resource := range declared.Spec.Resources {
		wanted[resource.Kind+"/"+resource.Metadata.Name] = true
	}
	for _, object := range statuses {
		if object.APIVersion != api.NetAPIVersion || wanted[object.Kind+"/"+object.Name] {
			continue
		}
		if object.Kind != "DHCPv6PrefixDelegation" && !(object.Kind == "DNSResolver" && r.Opts.SuperviseDNSResolvers) {
			continue
		}
		if err := store.DeleteObject(object.APIVersion, object.Kind, object.Name); err != nil {
			return err
		}
	}
	return nil
}
