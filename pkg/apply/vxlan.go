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

// ExtraVXLANInterfaces lists VXLAN links outside the declared and effective configurations.
func ExtraVXLANInterfaces(ctx context.Context, routers ...*api.Router) (map[string]bool, []string, error) {
	retained := map[string]bool{}
	for _, router := range routers {
		if router == nil {
			continue
		}
		aliases := interfaceAliases(router)
		for _, resource := range router.Spec.Resources {
			switch resource.Kind {
			case "Interface":
				spec, err := resource.InterfaceSpec()
				if err != nil {
					return nil, nil, err
				}
				retained[defaultString(spec.IfName, resource.Metadata.Name)] = true
			case "VXLANTunnel":
				spec, err := resource.VXLANTunnelSpec()
				if err != nil {
					return nil, nil, err
				}
				retained[defaultString(spec.IfName, resource.Metadata.Name)] = true
			case "VXLANSegment":
				spec, err := resource.VXLANSegmentSpec()
				if err != nil {
					return nil, nil, err
				}
				retained[defaultString(spec.IfName, resource.Metadata.Name)] = true
			case "MobilityPool":
				spec, err := resource.MobilityPoolSpec()
				if err != nil {
					return nil, nil, err
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
	out, err := exec.CommandContext(ctx, "ip", "-j", "-d", "link", "show").CombinedOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("list VXLAN links: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var links []struct {
		IfName string `json:"ifname"`
		Info   struct {
			Kind string `json:"info_kind"`
		} `json:"linkinfo"`
	}
	if err := json.Unmarshal(out, &links); err != nil {
		return nil, nil, fmt.Errorf("decode VXLAN links: %w", err)
	}
	var extra []string
	for _, link := range links {
		if link.Info.Kind == "vxlan" && link.IfName != "" && !retained[link.IfName] {
			extra = append(extra, link.IfName)
		}
	}
	return retained, extra, nil
}
