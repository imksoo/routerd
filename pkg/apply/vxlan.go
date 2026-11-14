// SPDX-License-Identifier: BSD-3-Clause

package apply

import (
	"github.com/imksoo/routerd/pkg/api"
)

// ConfiguredVXLANInterfaces lists interfaces needed by the configurations.
func ConfiguredVXLANInterfaces(routers ...*api.Router) (map[string]bool, error) {
	retained := map[string]bool{}
	for _, router := range routers {
		if router == nil {
			continue
		}
		aliases, err := interfaceAliases(router)
		if err != nil {
			return nil, err
		}
		for _, resource := range router.Spec.Resources {
			switch resource.Kind {
			case "Interface":
				spec, err := resource.InterfaceSpec()
				if err != nil {
					return nil, err
				}
				retained[defaultString(spec.IfName, resource.Metadata.Name)] = true
			case "VXLANTunnel":
				spec, err := resource.VXLANTunnelSpec()
				if err != nil {
					return nil, err
				}
				retained[defaultString(spec.IfName, resource.Metadata.Name)] = true
			case "VXLANSegment":
				spec, err := resource.VXLANSegmentSpec()
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
						retained[defaultString(aliases[member.Capture.Interface], member.Capture.Interface)] = true
					}
					for _, source := range member.OwnershipDiscovery.Sources {
						if source.Interface != "" {
							retained[defaultString(aliases[source.Interface], source.Interface)] = true
						}
					}
				}
			}
		}
	}
	return retained, nil
}
