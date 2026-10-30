// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import "github.com/imksoo/routerd/pkg/api"

// Zone rows describe configuration publication; they carry no native receipt.
// The supervisor owns resolver processes and their stopped-resource status.
func (c Controller) cleanupRemovedZones() error {
	declared := c.DeclaredRouter
	if declared == nil {
		declared = c.Router
	}
	wanted := map[string]bool{}
	for _, resource := range declared.Spec.Resources {
		if resource.Kind == "DNSZone" {
			wanted[resource.Metadata.Name] = true
		}
	}
	statuses, err := c.Store.ListObjectStatuses()
	if err != nil {
		return err
	}
	for _, object := range statuses {
		if object.APIVersion != api.NetAPIVersion || object.Kind != "DNSZone" || wanted[object.Name] {
			continue
		}
		if err := c.Store.DeleteObject(object.APIVersion, object.Kind, object.Name); err != nil {
			return err
		}
	}
	return nil
}
