// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/resourcequery"
	"github.com/imksoo/routerd/pkg/servicemgr"
)

func (c SystemdUnitController) reconcileClientDaemonUnits(ctx context.Context, telemetryEnv []string) error {
	defaults, features := platform.Current()
	if !features.HasSystemd && !features.HasRCD {
		return nil
	}
	if c.systemdSystemDir == "" {
		c.systemdSystemDir = defaults.SystemdSystemDir
	}

	desired := map[string]bool{}
	// Do not start a conditional WAN client until this generation has observed
	// its role. In particular a persisted VRRP role cannot authorize takeover.
	router := resourcequery.FilterRouterByResolvedWhen(c.Router, c.Store)
	aliases := interfaceAliases(c.Router)
	for _, resource := range router.Spec.Resources {
		var unit api.SystemdUnitSpec
		var unitName string
		var ifname string
		switch resource.Kind {
		case "DHCPv4Client":
			spec, err := resource.DHCPv4ClientSpec()
			if err != nil {
				return err
			}
			unitName = "routerd-dhcpv4-client@" + resource.Metadata.Name + ".service"
			ifname = aliases[spec.Interface]
			unit = dhcpv4ClientUnitSpec(resource.Metadata.Name, ifname, spec, telemetryEnv)
		case "DHCPv6PrefixDelegation":
			spec, err := resource.DHCPv6PrefixDelegationSpec()
			if err != nil {
				return err
			}
			if api.EffectiveIPv6PDClient(spec.Client) != api.IPv6PDClientRouterd {
				continue
			}
			unitName = "routerd-dhcpv6-client@" + resource.Metadata.Name + ".service"
			ifname = aliases[spec.Interface]
			unit = dhcpv6ClientUnitSpec(resource.Metadata.Name, ifname, spec, telemetryEnv)
		case "PPPoESession":
			spec, err := resource.PPPoESessionSpec()
			if err != nil {
				return err
			}
			if pppoeSessionDisabled(spec) {
				continue
			}
			unitName = "routerd-pppoe-client@" + resource.Metadata.Name + ".service"
			ifname = aliases[spec.Interface]
			unit = render.PPPoEClientSystemdSpec(resource.Metadata.Name, ifname, spec, telemetryEnv)
		case "IPv6RouterAdvertisement":
			spec, err := resource.IPv6RouterAdvertisementSpec()
			if err != nil {
				return err
			}
			unitName = "routerd-ra-observer@" + resource.Metadata.Name + ".service"
			ifname = firstNonEmpty(aliases[spec.Interface], spec.Interface)
			unit = raObserverUnitSpec(resource.Metadata.Name, ifname, telemetryEnv)
		default:
			continue
		}
		if ifname == "" {
			return fmt.Errorf("%s references an interface with no ifname", resource.ID())
		}
		desired[unitName] = true
		if err := c.reconcileClientDaemonUnit(ctx, resource.Kind, resource.Metadata.Name, unitName, unit); err != nil {
			return err
		}
	}
	for _, spec := range c.arpObservers {
		unitName := "routerd-arp-observer@" + spec.ResourceName + ".service"
		unit := api.SystemdUnitSpec{
			Description:              "routerd ARP observer " + spec.ResourceName,
			ExecStart:                append([]string{"/usr/local/sbin/routerd-arp-observer"}, arpObserverDaemonArgs(spec)...),
			Environment:              telemetryEnv,
			RuntimeDirectory:         []string{"routerd/arp-observer"},
			RuntimeDirectoryPreserve: "yes",
			StateDirectory:           []string{"routerd/arp-observer"},
			CapabilityBoundingSet:    []string{"CAP_NET_ADMIN", "CAP_NET_RAW"},
			AmbientCapabilities:      []string{"CAP_NET_ADMIN", "CAP_NET_RAW"},
		}
		desired[unitName] = true
		if err := c.reconcileClientDaemonUnit(ctx, "ARPObserver", spec.ResourceName, unitName, unit); err != nil {
			return err
		}
	}
	return c.cleanupClientDaemonUnits(ctx, desired)
}

func (c SystemdUnitController) reconcileClientDaemonUnit(ctx context.Context, kind, name, unitName string, unit api.SystemdUnitSpec) error {
	// routerd starts configured clients after resolving When/HA conditions.
	// Do not enable them at boot before that decision is available.
	enabled := false
	unit.Enabled = &enabled
	_, features := platform.Current()
	if features.HasSystemd {
		// Lease/session status belongs to the daemon controllers, not unit
		// reconciliation. Record only ServiceUnit status here.
		return c.reconcileSyntheticSystemdHelperUnit(ctx, unitName, kind+"/"+name, unit)
	}
	service := (servicemgr.RCD{}).ServiceName(servicemgr.Service{SystemdName: unitName})
	data, err := render.FreeBSDRCDScript(service, unit)
	if err != nil {
		return err
	}
	path := filepath.Join("/usr/local/etc/rc.d", service)
	changed, err := writeFileIfChanged(path, data, 0755, c.DryRun)
	if err != nil || c.DryRun {
		return err
	}
	if _, err := runOutputCommandContext(ctx, "sysrc", service+"_enable=NO"); err != nil {
		return err
	}
	if changed {
		_, err = runOutputCommandContext(ctx, "service", service, "onerestart")
	} else if _, statusErr := runOutputCommandContext(ctx, "service", service, "onestatus"); statusErr != nil {
		_, err = runOutputCommandContext(ctx, "service", service, "onestart")
	}
	return err
}

func (c SystemdUnitController) cleanupClientDaemonUnits(ctx context.Context, desired map[string]bool) error {
	defaults, features := platform.Current()
	dir := c.systemdSystemDir
	patterns := []string{"routerd-dhcpv4-client@*.service", "routerd-dhcpv6-client@*.service", "routerd-pppoe-client@*.service", "routerd-arp-observer@*.service", "routerd-ra-observer@*.service"}
	if features.HasSystemd && dir == "" {
		dir = defaults.SystemdSystemDir
	}
	if features.HasRCD {
		dir = "/usr/local/etc/rc.d"
		for i, pattern := range patterns {
			patterns[i] = strings.ReplaceAll(strings.TrimSuffix(pattern, ".service"), "-", "_")
			patterns[i] = strings.ReplaceAll(patterns[i], "@", "_")
		}
		nativeDesired := map[string]bool{}
		for name := range desired {
			nativeDesired[(servicemgr.RCD{}).ServiceName(servicemgr.Service{SystemdName: name})] = true
		}
		desired = nativeDesired
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return err
		}
		for _, path := range matches {
			name := filepath.Base(path)
			if desired[name] {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !strings.Contains(string(data), "# routerd-managed-service: v1\n") {
				continue
			}
			if features.HasSystemd {
				if _, err := c.applySystemdUnit(ctx, name, path, name, api.SystemdUnitSpec{State: "absent"}); err != nil {
					return err
				}
			} else if !c.DryRun {
				if _, err := runOutputCommandContext(ctx, "service", name, "onestop"); err != nil {
					return err
				}
				if _, err := runOutputCommandContext(ctx, "sysrc", "-x", name+"_enable"); err != nil {
					return err
				}
				if err := os.Remove(path); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
