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

// ExtraDSLiteInterfaces returns Linux ipip6 links not declared by the router.
func ExtraDSLiteInterfaces(ctx context.Context, routers ...*api.Router) ([]string, error) {
	retained := map[string]bool{"ip6tnl0": true}
	for _, router := range routers {
		if router == nil {
			continue
		}
		for _, resource := range router.Spec.Resources {
			switch resource.Kind {
			case "Interface":
				spec, err := resource.InterfaceSpec()
				if err != nil {
					return nil, err
				}
				retained[defaultString(spec.IfName, resource.Metadata.Name)] = true
			case "DSLiteTunnel":
				spec, err := resource.DSLiteTunnelSpec()
				if err != nil {
					return nil, err
				}
				retained[defaultString(spec.TunnelName, resource.Metadata.Name)] = true
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
	}
	out, err := exec.CommandContext(ctx, "ip", "-j", "-d", "link", "show").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list DS-Lite links: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var links []struct {
		IfName string `json:"ifname"`
		Info   struct {
			Kind string `json:"info_kind"`
			Data struct {
				Proto string `json:"proto"`
			} `json:"info_data"`
		} `json:"linkinfo"`
	}
	if err := json.Unmarshal(out, &links); err != nil {
		return nil, fmt.Errorf("decode DS-Lite links: %w", err)
	}
	var extra []string
	for _, link := range links {
		if link.Info.Kind == "ip6tnl" && link.Info.Data.Proto == "ipip6" && link.IfName != "" && !retained[link.IfName] {
			extra = append(extra, link.IfName)
		}
	}
	return extra, nil
}
