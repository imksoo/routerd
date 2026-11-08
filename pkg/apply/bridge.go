// SPDX-License-Identifier: BSD-3-Clause

package apply

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/imksoo/routerd/pkg/api"
)

// ExtraBridgeInterfaces returns Linux bridges not required by the router configuration.
func ExtraBridgeInterfaces(router *api.Router) ([]string, error) {
	retained := map[string]bool{}
	keep := func(name string) {
		if name != "" {
			retained[api.ResolveInterfaceIfName(router, name)] = true
		}
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "Bridge":
			spec, err := resource.BridgeSpec()
			if err != nil {
				return nil, err
			}
			retained[defaultString(spec.IfName, resource.Metadata.Name)] = true
		case "Interface":
			spec, err := resource.InterfaceSpec()
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
				keep(member.Capture.Interface)
				for _, source := range member.OwnershipDiscovery.Sources {
					keep(source.Interface)
					keep(source.Bridge)
				}
			}
		}
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list Bridge interfaces: %w", err)
	}
	var names []string
	for _, iface := range interfaces {
		if retained[iface.Name] {
			continue
		}
		_, err := os.Stat(filepath.Join("/sys/class/net", iface.Name, "bridge"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("observe Bridge %s: %w", iface.Name, err)
		}
		names = append(names, iface.Name)
	}
	return names, nil
}
