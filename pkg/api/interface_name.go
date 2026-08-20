// SPDX-License-Identifier: BSD-3-Clause

package api

// ResolveInterfaceIfName returns the kernel interface name for a managed
// interface resource, or name unchanged when it is already a kernel name or
// does not name a managed interface. Keeping this API-level mapping shared
// prevents controllers from resolving the same logical alias differently.
func ResolveInterfaceIfName(router *Router, name string) string {
	if router == nil {
		return name
	}
	for _, resource := range router.Spec.Resources {
		if resource.Metadata.Name != name {
			continue
		}
		switch resource.Kind {
		case "Interface":
			spec, err := resource.InterfaceSpec()
			if err == nil && spec.IfName != "" {
				return spec.IfName
			}
		case "Bridge":
			spec, err := resource.BridgeSpec()
			if err == nil && spec.IfName != "" {
				return spec.IfName
			}
		case "VXLANSegment":
			spec, err := resource.VXLANSegmentSpec()
			if err == nil && spec.IfName != "" {
				return spec.IfName
			}
		case "WireGuardInterface":
			spec, err := resource.WireGuardInterfaceSpec()
			if err == nil && spec.IfName != "" {
				return spec.IfName
			}
		case "PPPoESession":
			spec, err := resource.PPPoESessionSpec()
			if err == nil {
				if spec.IfName != "" {
					return spec.IfName
				}
				return "ppp-" + resource.Metadata.Name
			}
		case "DSLiteTunnel":
			spec, err := resource.DSLiteTunnelSpec()
			if err == nil {
				if spec.TunnelName != "" {
					return spec.TunnelName
				}
				return resource.Metadata.Name
			}
		}
	}
	return name
}
