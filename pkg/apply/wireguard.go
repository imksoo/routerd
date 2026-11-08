// SPDX-License-Identifier: BSD-3-Clause

package apply

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

// ConfiguredWireGuardInterfaces returns interface names required by the configuration.
func ConfiguredWireGuardInterfaces(router *api.Router) (map[string]bool, error) {
	retained := map[string]bool{}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "Interface", "WireGuardInterface":
			retained[api.ResolveInterfaceIfName(router, resource.Metadata.Name)] = true
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

// ExtraWireGuardInterfaces lists Linux WireGuard links not present in the effective configuration.
func ExtraWireGuardInterfaces(ctx context.Context, router *api.Router) ([]string, error) {
	retained, err := ConfiguredWireGuardInterfaces(router)
	if err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, "ip", "-j", "-d", "link", "show").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list WireGuard links: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var links []struct {
		IfName string `json:"ifname"`
		Info   struct {
			Kind string `json:"info_kind"`
		} `json:"linkinfo"`
	}
	if err := json.Unmarshal(out, &links); err != nil {
		return nil, fmt.Errorf("decode WireGuard links: %w", err)
	}
	var extra []string
	for _, link := range links {
		if link.Info.Kind == "wireguard" && link.IfName != "" && !retained[link.IfName] {
			extra = append(extra, link.IfName)
		}
	}
	return extra, nil
}
