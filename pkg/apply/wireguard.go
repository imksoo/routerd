// SPDX-License-Identifier: BSD-3-Clause

package apply

import (
	"github.com/imksoo/routerd/pkg/api"
)

// ConfiguredWireGuardInterfaces returns interface names required by the configuration.
func ConfiguredWireGuardInterfaces(router *api.Router) (map[string]bool, error) {
	retained := map[string]bool{}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "Interface":
			spec, err := resource.InterfaceSpec()
			if err != nil {
				return nil, err
			}
			retained[defaultString(spec.IfName, resource.Metadata.Name)] = true
		case "WireGuardInterface":
			spec, err := resource.WireGuardInterfaceSpec()
			if err != nil {
				return nil, err
			}
			retained[defaultString(spec.IfName, resource.Metadata.Name)] = true
		case "MobilityPool":
			spec, err := resource.MobilityPoolSpec()
			if err != nil {
				return nil, err
			}
			for _, member := range spec.Members {
				if member.Capture.Interface != "" {
					retained[api.ResolveInterfaceIfName(router, member.Capture.Interface)] = true
				}
				for _, source := range member.OwnershipDiscovery.Sources {
					if source.Interface != "" {
						retained[api.ResolveInterfaceIfName(router, source.Interface)] = true
					}
				}
			}
		}
	}
	return retained, nil
}
