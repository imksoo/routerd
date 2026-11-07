// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"path/filepath"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

// dhcpv6Worker binds declarations to the single UDP/546 owner on their resolved
// kernel interface. Configuration validation rejects duplicate associations.
// A PD keeps its existing worker identity. An address-only worker uses a kind
// namespace (resource names cannot contain '/') so equal names in different
// resource kinds cannot alias supervisor markers, sockets or lease files.
type dhcpv6Worker struct {
	Name, IfName, Address string
	Prefix                *api.DHCPv6PrefixDelegationSpec
}

func observedDHCPv6Workers(router *api.Router, store Store) ([]dhcpv6Worker, error) {
	if router == nil {
		return nil, nil
	}
	present := false
	for _, resource := range router.Spec.Resources {
		if resource.Kind == "DHCPv6Address" || resource.Kind == "DHCPv6PrefixDelegation" {
			present = true
			break
		}
	}
	if !present {
		return nil, nil
	}
	stateStore, _ := store.(resourcequery.StateStore)
	effective, err := resourcequery.FilterRouterByResolvedWhen(router, stateStore)
	if err != nil {
		return nil, err
	}
	return dhcpv6Workers(effective), nil
}

func dhcpv6Workers(router *api.Router) []dhcpv6Worker {
	if router == nil {
		return nil
	}
	var out []dhcpv6Worker
	byInterface := map[string]int{}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DHCPv6PrefixDelegation" && resource.Kind != "DHCPv6Address" {
			continue
		}
		var iface string
		var prefix *api.DHCPv6PrefixDelegationSpec
		if resource.Kind == "DHCPv6PrefixDelegation" {
			spec, err := resource.DHCPv6PrefixDelegationSpec()
			if err != nil {
				continue
			} // caller supplies a validated Router
			iface, prefix = spec.Interface, &spec
		} else {
			spec, err := resource.DHCPv6AddressSpec()
			if err != nil {
				continue
			}
			iface = spec.Interface
		}
		ifname := api.ResolveInterfaceIfName(router, iface)
		index, found := byInterface[ifname]
		if !found {
			index = len(out)
			byInterface[ifname] = index
			out = append(out, dhcpv6Worker{IfName: ifname})
		}
		worker := &out[index]
		if prefix != nil {
			worker.Name, worker.Prefix = resource.Metadata.Name, prefix
		} else {
			worker.Address = resource.Metadata.Name
			if worker.Prefix == nil {
				worker.Name = "DHCPv6Address/" + resource.Metadata.Name
			}
		}
	}
	return out
}

func (w dhcpv6Worker) socket(overrides map[string]string) string {
	if socket := overrides[w.Name]; socket != "" {
		return socket
	}
	return defaultClientSocket("routerd-dhcpv6-client", w.Name)
}

func (w dhcpv6Worker) daemonSpec() supervisedDaemonSpec {
	defaults, _ := platform.Current()
	mode := "pd"
	if w.Prefix == nil {
		mode = "address"
	} else if w.Address != "" {
		mode = "pd-address"
	}
	args := []string{"daemon", "--resource", w.Name, "--interface", w.IfName,
		"--request-mode", mode,
		"--socket", w.socket(nil),
		"--lease-file", filepath.Join(defaults.StateDir, "dhcpv6-client", w.Name, "lease.json"),
		"--event-file", filepath.Join(defaults.StateDir, "dhcpv6-client", w.Name, "events.jsonl"),
	}
	if w.Address != "" {
		args = append(args, "--address-resource", w.Address)
	}
	if w.Prefix != nil {
		if w.Prefix.IAID != "" {
			args = append(args, "--iaid", w.Prefix.IAID)
		}
		if w.Prefix.ClientDUID != "" {
			args = append(args, "--client-duid", w.Prefix.ClientDUID)
		}
	}
	return supervisedDaemonSpec{ResourceName: w.Name, Binary: "routerd-dhcpv6-client", Args: args}
}
