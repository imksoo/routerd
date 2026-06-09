// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/healthcheck"
	"github.com/imksoo/routerd/pkg/hostdeps"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/resourcequery"
	"github.com/imksoo/routerd/pkg/tailscale"
)

type NetworkAdoptionController struct {
	Router *api.Router
	Bus    interface {
		Publish(context.Context, daemonapi.DaemonEvent) error
	}
	Store              Store
	Command            outputCommandFunc
	DryRun             bool
	NetworkdDropinBase string
	ResolvedDropinDir  string
	OSName             string
}

func (c NetworkAdoptionController) Reconcile(ctx context.Context) error {
	defaults, features := platform.Current()
	if c.NetworkdDropinBase == "" {
		c.NetworkdDropinBase = defaults.NetworkdDropinDir
	}
	if c.ResolvedDropinDir == "" {
		c.ResolvedDropinDir = "/etc/systemd/resolved.conf.d"
	}
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	for _, resource := range networkAdoptionControllerResources(c.Router) {
		spec, err := resource.NetworkAdoptionSpec()
		if err != nil {
			return err
		}
		osName := c.OSName
		if osName == "" {
			osName = networkAdoptionOSName(runtime.GOOS)
		}
		if osName == "nixos" {
			ifname := strings.TrimSpace(spec.IfName)
			if ifname == "" && spec.Interface != "" {
				ifname = interfaceIfName(c.Router, spec.Interface)
			}
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "NetworkAdoption", resource.Metadata.Name, map[string]any{
				"phase":     "Applied",
				"reason":    "NixOSDeclarativeNetworkConfig",
				"os":        osName,
				"ifname":    ifname,
				"changed":   false,
				"dryRun":    c.DryRun,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}); err != nil {
				return err
			}
			continue
		}
		if !features.HasSystemd {
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "NetworkAdoption", resource.Metadata.Name, map[string]any{
				"phase":     "Pending",
				"reason":    "SystemdUnsupported",
				"os":        osName,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}); err != nil {
				return err
			}
			continue
		}
		ifname := strings.TrimSpace(spec.IfName)
		if ifname == "" && spec.Interface != "" {
			ifname = interfaceIfName(c.Router, spec.Interface)
		}
		paths, changed, err := c.applyNetworkAdoption(ctx, resource.Metadata.Name, ifname, spec, command)
		if err != nil {
			if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "NetworkAdoption", resource.Metadata.Name, map[string]any{
				"phase":     "Error",
				"reason":    "ApplyFailed",
				"ifname":    ifname,
				"paths":     paths,
				"error":     err.Error(),
				"dryRun":    c.DryRun,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}); saveErr != nil {
				return saveErr
			}
			return err
		}
		phase := "Applied"
		if c.DryRun && changed {
			phase = "Rendered"
		}
		if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "NetworkAdoption", resource.Metadata.Name, map[string]any{
			"phase":     phase,
			"ifname":    ifname,
			"paths":     paths,
			"changed":   changed,
			"dryRun":    c.DryRun,
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
		if changed && !c.DryRun && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.network_adoption.applied", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "NetworkAdoption", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"ifname": ifname, "paths": strings.Join(paths, ",")}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func networkAdoptionControllerResources(router *api.Router) []api.Resource {
	return hostdeps.NetworkAdoptionResources(router)
}

func networkAdoptionOSName(goos string) string {
	if goos == "freebsd" {
		return "freebsd"
	}
	if goos != "linux" {
		return goos
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "linux"
	}
	id := parseOSReleaseID(string(data))
	if id == "" {
		return "linux"
	}
	return id
}

func (c NetworkAdoptionController) applyNetworkAdoption(ctx context.Context, name, ifname string, spec api.NetworkAdoptionSpec, command outputCommandFunc) ([]string, bool, error) {
	state := firstNonEmpty(spec.State, "present")
	var paths []string
	var changed bool
	networkdChanged := false
	resolvedChanged := false
	if networkdAdoptionConfigured(spec.SystemdNetworkd) {
		if ifname == "" {
			return paths, changed, fmt.Errorf("ifname is required for systemdNetworkd adoption")
		}
		path := filepath.Join(networkdDropinDir(c.NetworkdDropinBase, ifname), firstNonEmpty(spec.SystemdNetworkd.DropinName, "90-routerd-adoption.conf"))
		paths = append(paths, path)
		if state == "absent" {
			removed, err := removeFileIfExists(path, c.DryRun)
			if err != nil {
				return paths, changed, err
			}
			networkdChanged = removed
		} else {
			data := networkdAdoptionDropin(spec.SystemdNetworkd)
			fileChanged, err := writeFileIfChanged(path, data, 0644, c.DryRun)
			if err != nil {
				return paths, changed, err
			}
			networkdChanged = fileChanged
			for _, legacyPath := range legacyNetworkdAdoptionDropins(c.NetworkdDropinBase, ifname, path) {
				paths = append(paths, legacyPath)
				removed, err := removeFileIfExists(legacyPath, c.DryRun)
				if err != nil {
					return paths, changed, err
				}
				networkdChanged = networkdChanged || removed
			}
		}
		changed = changed || networkdChanged
	}
	if resolvedAdoptionConfigured(spec.SystemdResolved) {
		path := filepath.Join(c.ResolvedDropinDir, firstNonEmpty(spec.SystemdResolved.DropinName, "90-routerd-adoption.conf"))
		paths = append(paths, path)
		if state == "absent" {
			removed, err := removeFileIfExists(path, c.DryRun)
			if err != nil {
				return paths, changed, err
			}
			resolvedChanged = removed
		} else {
			data := resolvedAdoptionDropin(spec.SystemdResolved)
			fileChanged, err := writeFileIfChanged(path, data, 0644, c.DryRun)
			if err != nil {
				return paths, changed, err
			}
			resolvedChanged = fileChanged
		}
		changed = changed || resolvedChanged
	}
	if c.DryRun || !api.BoolDefault(spec.Reload, true) {
		return paths, changed, nil
	}
	if networkdChanged {
		if _, err := command(ctx, "networkctl", "reload"); err != nil {
			if _, fallbackErr := command(ctx, "systemctl", "restart", "systemd-networkd.service"); fallbackErr != nil {
				return paths, changed, fmt.Errorf("reload networkd: %w; restart fallback: %v", err, fallbackErr)
			}
		}
		if ifname != "" {
			if _, err := command(ctx, "networkctl", "reconfigure", ifname); err != nil {
				return paths, changed, fmt.Errorf("reconfigure %s: %w", ifname, err)
			}
		}
	}
	if resolvedChanged {
		if _, err := command(ctx, "systemctl", "restart", "systemd-resolved.service"); err != nil {
			return paths, changed, fmt.Errorf("restart systemd-resolved: %w", err)
		}
	}
	_ = name
	return paths, changed, nil
}

func legacyNetworkdAdoptionDropins(base, ifname, desiredPath string) []string {
	dir := networkdDropinDir(base, ifname)
	candidates := []string{
		filepath.Join(dir, "50-routerd-no-dhcpv6.conf"),
	}
	var out []string
	for _, candidate := range candidates {
		if filepath.Clean(candidate) == filepath.Clean(desiredPath) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func resolvedAdoptionConfigured(spec api.NetworkAdoptionResolvedSpec) bool {
	return spec.DisableDNSStubListener || len(spec.DNSServers) > 0 || len(spec.FallbackDNSServers) > 0
}

func resolvedAdoptionDropin(spec api.NetworkAdoptionResolvedSpec) []byte {
	var b strings.Builder
	b.WriteString("# Managed by routerd. Do not edit by hand.\n[Resolve]\n")
	if spec.DisableDNSStubListener {
		b.WriteString("DNSStubListener=no\n")
	}
	if len(spec.DNSServers) > 0 {
		b.WriteString("DNS=")
		b.WriteString(strings.Join(spec.DNSServers, " "))
		b.WriteByte('\n')
	}
	if len(spec.FallbackDNSServers) > 0 {
		b.WriteString("FallbackDNS=")
		b.WriteString(strings.Join(spec.FallbackDNSServers, " "))
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

type SystemdUnitController struct {
	Router         *api.Router
	DeclaredRouter *api.Router
	Bus            interface {
		Publish(context.Context, daemonapi.DaemonEvent) error
	}
	Store                       Store
	Command                     outputCommandFunc
	DryRun                      bool
	SystemdSystemDir            string
	SynthesizeClientDaemonUnits bool
}

func (c SystemdUnitController) Reconcile(ctx context.Context) error {
	defaults, features := platform.Current()
	if c.SystemdSystemDir == "" {
		c.SystemdSystemDir = defaults.SystemdSystemDir
	}
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	telemetryEnv, err := render.TelemetryEnvironment(c.Router)
	if err != nil {
		return err
	}
	if features.HasSystemd {
		explicitUnits := map[string]bool{}
		routerdSpec := render.RouterdServiceSystemdSpec()
		routerdSpec = maybeAugmentRouterdServiceAccess(c.Router, render.RouterdUnitName, routerdSpec)
		routerdSpec.Environment = mergeStringEnvs(routerdSpec.Environment, telemetryEnv)
		if err := c.reconcileSyntheticSystemdHelperUnit(ctx, render.RouterdUnitName, "Router/"+c.Router.Metadata.Name, routerdSpec, command); err != nil {
			return err
		}
		if routerHasBGP(c.Router) {
			if err := c.reconcileLongLivedSystemdHelperUnit(ctx, render.BGPUnitName, "Router/"+c.Router.Metadata.Name+"/BGPRouter", render.BGPSystemdSpec("/run/routerd/bgp/gobgp.sock"), command); err != nil {
				return err
			}
		} else {
			if err := c.cleanupLongLivedSystemdHelperUnit(ctx, render.BGPUnitName, "Router/"+c.Router.Metadata.Name+"/BGPRouter", command); err != nil {
				return err
			}
		}
		if err := c.reconcileDNSResolverUnits(ctx, defaults.StateDir, command); err != nil {
			return err
		}
		if err := c.cleanupStaleDNSResolverUnits(ctx, command); err != nil {
			return err
		}
		if err := c.reconcileEventFederationUnits(ctx, defaults.StateDir, command); err != nil {
			return err
		}
		if err := c.cleanupStaleEventFederationUnits(ctx, command); err != nil {
			return err
		}
		if c.SynthesizeClientDaemonUnits {
			if err := c.reconcileClientDaemonUnits(ctx, explicitUnits, telemetryEnv, command); err != nil {
				return err
			}
		} else if err := c.cleanupStaleClientDaemonUnits(ctx, command); err != nil {
			return err
		}
		for _, resource := range c.Router.Spec.Resources {
			if resource.Kind != "TailscaleNode" {
				continue
			}
			spec, err := resource.TailscaleNodeSpec()
			if err != nil {
				return err
			}
			unit := render.TailscaleSystemdSpec(resource.Metadata.Name, spec)
			unitName := firstNonEmpty(unit.UnitName, render.TailscaleUnitName(resource.Metadata.Name))
			path := filepath.Join(c.SystemdSystemDir, unitName)
			changed := false
			if !explicitUnits[unitName] {
				var err error
				changed, err = c.applySystemdUnit(ctx, resource.Metadata.Name, path, unitName, unit, command)
				if err != nil {
					if saveErr := c.Store.SaveObjectStatus(api.NetAPIVersion, "TailscaleNode", resource.Metadata.Name, map[string]any{
						"phase":     "Error",
						"reason":    "ApplyFailed",
						"unitName":  unitName,
						"path":      path,
						"error":     err.Error(),
						"dryRun":    c.DryRun,
						"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
					}); saveErr != nil {
						return saveErr
					}
					return err
				}
			}
			phase := "Applied"
			if c.DryRun && changed {
				phase = "Rendered"
			}
			if firstNonEmpty(spec.State, "present") == "absent" {
				phase = "Removed"
			}
			status := map[string]any{
				"phase":             phase,
				"unitName":          unitName,
				"path":              path,
				"changed":           changed,
				"dryRun":            c.DryRun,
				"explicitUnit":      explicitUnits[unitName],
				"advertiseExitNode": spec.AdvertiseExitNode,
				"advertiseRoutes":   strings.Join(spec.AdvertiseRoutes, ","),
				"acceptDNS":         boolPointerStatus(spec.AcceptDNS),
				"acceptRoutes":      boolPointerStatus(spec.AcceptRoutes),
				"updatedAt":         time.Now().UTC().Format(time.RFC3339Nano),
			}
			if firstNonEmpty(spec.State, "present") != "absent" && !c.DryRun {
				runtimeStatus, err := observeTailscaleRuntime(ctx, command, spec)
				if err != nil {
					status["runtimeObserved"] = false
					status["runtimeError"] = err.Error()
				} else {
					status["runtimeObserved"] = true
					status["backendState"] = runtimeStatus.BackendState
					status["tailnetName"] = runtimeStatus.TailnetName
					status["magicDNSSuffix"] = runtimeStatus.MagicDNSSuffix
					status["magicDNSEnabled"] = runtimeStatus.MagicDNSEnabled
					status["dnsName"] = runtimeStatus.DNSName
					status["tailscaleIPs"] = strings.Join(runtimeStatus.TailscaleIPs, ",")
					status["allowedIPs"] = strings.Join(runtimeStatus.AllowedIPs, ",")
					status["online"] = runtimeStatus.Online
					status["active"] = runtimeStatus.Active
					status["exitNodeOption"] = runtimeStatus.ExitNodeOption
					status["peerCount"] = runtimeStatus.PeerCount
					status["onlinePeerCount"] = runtimeStatus.OnlinePeerCount
					status["peers"] = tailscalePeerStatusMaps(runtimeStatus.Peers)
					if runtimeStatus.BackendState == "Running" {
						status["phase"] = "Running"
					}
					tailscale.RecordMetrics(ctx, resource.Metadata.Name, runtimeStatus.Status, time.Now().UTC())
				}
			}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "TailscaleNode", resource.Metadata.Name, status); err != nil {
				return err
			}
			if changed && !c.DryRun && c.Bus != nil {
				event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.tailscale.node.applied", daemonapi.SeverityInfo)
				event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "TailscaleNode", Name: resource.Metadata.Name}
				event.Attributes = map[string]string{"unitName": unitName, "advertiseRoutes": strings.Join(spec.AdvertiseRoutes, ","), "advertiseExitNode": fmt.Sprint(spec.AdvertiseExitNode)}
				if err := c.Bus.Publish(ctx, event); err != nil {
					return err
				}
			}
		}
		if err := c.cleanupWhenFalseSystemdUnits(ctx, command); err != nil {
			return err
		}
		for _, resource := range c.Router.Spec.Resources {
			if resource.Kind != "HealthCheck" {
				continue
			}
			spec, err := resource.HealthCheckSpec()
			if err != nil {
				return err
			}
			unitName := healthCheckUnitName(resource.Metadata.Name)
			if explicitUnits[unitName] {
				continue
			}
			path := filepath.Join(c.SystemdSystemDir, unitName)
			changed, err := c.applyHealthCheckSystemdUnit(ctx, path, unitName, resource.Metadata.Name, spec, telemetryEnv, command)
			if err != nil {
				if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, map[string]any{
					"phase":     "Error",
					"reason":    "ApplyFailed",
					"unitName":  unitName,
					"path":      path,
					"error":     err.Error(),
					"dryRun":    c.DryRun,
					"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
				}); saveErr != nil {
					return saveErr
				}
				return err
			}
			phase := "Applied"
			if c.DryRun && changed {
				phase = "Rendered"
			}
			if healthCheckDisabled(spec) {
				phase = "Disabled"
				if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", resource.Metadata.Name, map[string]any{
					"phase":     phase,
					"reason":    "Disabled",
					"daemon":    healthcheck.DaemonKind,
					"unitName":  unitName,
					"dryRun":    c.DryRun,
					"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
				}); err != nil {
					return err
				}
			}
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, map[string]any{
				"phase":     phase,
				"unitName":  unitName,
				"path":      path,
				"changed":   changed,
				"dryRun":    c.DryRun,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}); err != nil {
				return err
			}
			if changed && !c.DryRun && c.Bus != nil {
				event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.service_unit.applied", daemonapi.SeverityInfo)
				event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "ServiceUnit", Name: unitName}
				event.Attributes = map[string]string{"unitName": unitName, "path": path, "source": "HealthCheck/" + resource.Metadata.Name}
				if err := c.Bus.Publish(ctx, event); err != nil {
					return err
				}
			}
		}
		if err := c.cleanupStaleHealthCheckUnits(ctx, explicitUnits, command); err != nil {
			return err
		}
		if render.RouterWantsDPIClassifier(c.Router) {
			dpiSpec := render.DPIClassifierSystemdSpec("/run")
			dpiSpec.Environment = mergeStringEnvs(dpiSpec.Environment, telemetryEnv)
			if err := c.reconcileSyntheticSystemdHelperUnit(ctx, render.DPIClassifierUnitName, "TrafficFlowLog/FirewallEventLog", dpiSpec, command); err != nil {
				return err
			}
		}
		if render.RouterWantsNDPIAgent(c.Router) {
			ndpiSpec := render.NDPIAgentSystemdSpec("/run")
			ndpiSpec.Environment = mergeStringEnvs(ndpiSpec.Environment, telemetryEnv)
			if err := c.reconcileSyntheticSystemdHelperUnit(ctx, render.NDPIAgentUnitName, "TrafficFlowLog/FirewallEventLog", ndpiSpec, command); err != nil {
				return err
			}
		}
		for _, resource := range c.Router.Spec.Resources {
			if resource.Kind != "FirewallEventLog" {
				continue
			}
			spec, err := resource.FirewallEventLogSpec()
			if err != nil {
				return err
			}
			if !spec.Enabled {
				continue
			}
			dpiSocket := ""
			if render.RouterWantsDPIClassifier(c.Router) {
				dpiSocket = "/run/routerd/dpi-classifier/default.sock"
			}
			unit := render.FirewallLoggerSystemdSpec(spec, dpiSocket)
			unit.Environment = mergeStringEnvs(unit.Environment, telemetryEnv)
			if err := c.reconcileSyntheticSystemdHelperUnit(ctx, "routerd-firewall-logger.service", "FirewallEventLog/"+resource.Metadata.Name, unit, command); err != nil {
				return err
			}
		}
	}
	if err := c.reconcileDisabledPPPoESessions(); err != nil {
		return err
	}
	return nil
}

func (c SystemdUnitController) reconcileDisabledPPPoESessions() error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "PPPoESession" {
			continue
		}
		spec, err := resource.PPPoESessionSpec()
		if err != nil {
			return err
		}
		if !pppoeSessionDisabled(spec) {
			continue
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "PPPoESession", resource.Metadata.Name, map[string]any{
			"phase":     PhaseDisabled,
			"reason":    "Disabled",
			"interface": spec.Interface,
			"ifname":    firstNonEmpty(spec.IfName, "ppp-"+resource.Metadata.Name),
			"dryRun":    c.DryRun,
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
	}
	return nil
}

type tailscaleRuntimeStatus struct {
	tailscale.Status
	BackendState    string
	TailnetName     string
	MagicDNSSuffix  string
	MagicDNSEnabled bool
	DNSName         string
	TailscaleIPs    []string
	AllowedIPs      []string
	Online          bool
	Active          bool
	ExitNodeOption  bool
	PeerCount       int
	OnlinePeerCount int
	Peers           []tailscale.PeerStatus
}

func observeTailscaleRuntime(ctx context.Context, command outputCommandFunc, spec api.TailscaleNodeSpec) (tailscaleRuntimeStatus, error) {
	binary := firstNonEmpty(spec.BinaryPath, "tailscale")
	status, err := tailscale.Fetch(ctx, binary, tailscale.CommandFunc(command))
	if err != nil {
		return tailscaleRuntimeStatus{}, err
	}
	return tailscaleRuntimeStatus{
		Status:          status,
		BackendState:    status.BackendState,
		TailnetName:     status.TailnetName,
		MagicDNSSuffix:  status.MagicDNSSuffix,
		MagicDNSEnabled: status.MagicDNSEnabled,
		DNSName:         status.DNSName,
		TailscaleIPs:    status.TailscaleIPs,
		AllowedIPs:      status.AllowedIPs,
		Online:          status.Online,
		Active:          status.Active,
		ExitNodeOption:  status.ExitNodeOption,
		PeerCount:       len(status.Peers),
		OnlinePeerCount: tailscale.OnlinePeerCount(status),
		Peers:           status.Peers,
	}, nil
}

func tailscalePeerStatusMaps(peers []tailscale.PeerStatus) []map[string]any {
	out := make([]map[string]any, 0, len(peers))
	for _, peer := range peers {
		out = append(out, map[string]any{
			"id":             peer.ID,
			"hostName":       peer.HostName,
			"dnsName":        peer.DNSName,
			"tailscaleIPs":   peer.TailscaleIPs,
			"allowedIPs":     peer.AllowedIPs,
			"online":         peer.Online,
			"active":         peer.Active,
			"exitNode":       peer.ExitNode,
			"exitNodeOption": peer.ExitNodeOption,
			"relay":          peer.Relay,
			"lastSeen":       peer.LastSeen,
			"rxBytes":        peer.RxBytes,
			"txBytes":        peer.TxBytes,
		})
	}
	return out
}

func boolPointerStatus(value *bool) any {
	if value == nil {
		return ""
	}
	return *value
}

func (c SystemdUnitController) reconcileClientDaemonUnits(ctx context.Context, explicitUnits map[string]bool, telemetryEnv []string, command outputCommandFunc) error {
	aliases := interfaceAliases(c.Router)
	for _, resource := range c.Router.Spec.Resources {
		switch resource.Kind {
		case "DHCPv4Client":
			spec, err := resource.DHCPv4ClientSpec()
			if err != nil {
				return err
			}
			ifname := aliases[spec.Interface]
			if ifname == "" {
				return fmt.Errorf("%s references interface %q with no ifname", resource.ID(), spec.Interface)
			}
			unitName := "routerd-dhcpv4-client@" + resource.Metadata.Name + ".service"
			if explicitUnits[unitName] {
				continue
			}
			if err := c.reconcileSyntheticSystemdUnit(ctx, api.NetAPIVersion, "DHCPv4Client", resource.Metadata.Name, unitName, dhcpv4ClientUnitSpec(resource.Metadata.Name, ifname, spec, telemetryEnv), command); err != nil {
				return err
			}
		case "DHCPv6PrefixDelegation":
			spec, err := resource.DHCPv6PrefixDelegationSpec()
			if err != nil {
				return err
			}
			ifname := aliases[spec.Interface]
			if ifname == "" {
				return fmt.Errorf("%s references interface %q with no ifname", resource.ID(), spec.Interface)
			}
			unitName := "routerd-dhcpv6-client@" + resource.Metadata.Name + ".service"
			if explicitUnits[unitName] {
				continue
			}
			if err := c.reconcileSyntheticSystemdUnit(ctx, api.NetAPIVersion, "DHCPv6PrefixDelegation", resource.Metadata.Name, unitName, dhcpv6ClientUnitSpec(resource.Metadata.Name, ifname, spec, telemetryEnv), command); err != nil {
				return err
			}
		case "IPv6RouterAdvertisement":
			spec, err := resource.IPv6RouterAdvertisementSpec()
			if err != nil {
				return err
			}
			ifname := aliases[spec.Interface]
			if ifname == "" {
				ifname = spec.Interface
			}
			unitName := "routerd-ra-observer@" + resource.Metadata.Name + ".service"
			if explicitUnits[unitName] {
				continue
			}
			if err := c.reconcileSyntheticSystemdUnit(ctx, api.NetAPIVersion, "RogueRADetector", resource.Metadata.Name, unitName, raObserverUnitSpec(resource.Metadata.Name, ifname, telemetryEnv), command); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c SystemdUnitController) reconcileDNSResolverUnits(ctx context.Context, stateDir string, command outputCommandFunc) error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DNSResolver" {
			continue
		}
		spec, err := resource.DNSResolverSpec()
		if err != nil {
			return err
		}
		unitName := dnsResolverUnitName(resource.Metadata.Name)
		configPath := filepath.Join(stateDir, "dns-resolver", resource.Metadata.Name, "config.json")
		if err := c.reconcileLongLivedSystemdHelperUnit(ctx, unitName, "Router/"+c.Router.Metadata.Name+"/DNSResolver/"+resource.Metadata.Name, render.DNSResolverSystemdSpec(resource.Metadata.Name, spec, "/usr/local/sbin/routerd-dns-resolver", configPath), command); err != nil {
			return err
		}
	}
	return nil
}

func dnsResolverUnitName(name string) string {
	return "routerd-dns-resolver@" + name + ".service"
}

func (c SystemdUnitController) reconcileEventFederationUnits(ctx context.Context, stateDir string, command outputCommandFunc) error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "EventGroup" {
			continue
		}
		group := resource.Metadata.Name
		unitName := eventFederationUnitName(group)
		configPath := filepath.Join(stateDir, "eventd", group, "config.json")
		if err := c.reconcileLongLivedSystemdHelperUnit(ctx, unitName, "Router/"+c.Router.Metadata.Name+"/EventGroup/"+group, render.EventdSystemdSpec(group, "/usr/local/sbin/routerd-eventd", configPath), command); err != nil {
			return err
		}
	}
	return nil
}

func eventFederationUnitName(group string) string {
	return "routerd-eventd@" + group + ".service"
}

func (c SystemdUnitController) reconcileSyntheticSystemdUnit(ctx context.Context, apiVersion, kind, resourceName, unitName string, spec api.SystemdUnitSpec, command outputCommandFunc) error {
	path := filepath.Join(c.SystemdSystemDir, unitName)
	changed, err := c.applySystemdUnit(ctx, resourceName, path, unitName, spec, command)
	if err != nil {
		if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, map[string]any{
			"phase":     "Error",
			"reason":    "ApplyFailed",
			"unitName":  unitName,
			"path":      path,
			"source":    kind + "/" + resourceName,
			"error":     err.Error(),
			"dryRun":    c.DryRun,
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}); saveErr != nil {
			return saveErr
		}
		return err
	}
	phase := "Applied"
	if c.DryRun && changed {
		phase = "Rendered"
	}
	status := map[string]any{
		"phase":     phase,
		"unitName":  unitName,
		"path":      path,
		"source":    kind + "/" + resourceName,
		"changed":   changed,
		"dryRun":    c.DryRun,
		"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, status); err != nil {
		return err
	}
	if err := c.Store.SaveObjectStatus(apiVersion, kind, resourceName, map[string]any{
		"phase":     phase,
		"unitName":  unitName,
		"managedBy": "systemd",
		"dryRun":    c.DryRun,
		"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return err
	}
	if changed && !c.DryRun && c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.service_unit.applied", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "ServiceUnit", Name: unitName}
		event.Attributes = map[string]string{"unitName": unitName, "path": path, "source": kind + "/" + resourceName}
		return c.Bus.Publish(ctx, event)
	}
	return nil
}

func (c SystemdUnitController) reconcileSyntheticSystemdHelperUnit(ctx context.Context, unitName, source string, spec api.SystemdUnitSpec, command outputCommandFunc) error {
	path := filepath.Join(c.SystemdSystemDir, unitName)
	changed, err := c.applySystemdUnit(ctx, unitName, path, unitName, spec, command)
	if err != nil {
		if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, map[string]any{
			"phase":     "Error",
			"reason":    "ApplyFailed",
			"unitName":  unitName,
			"path":      path,
			"source":    source,
			"error":     err.Error(),
			"dryRun":    c.DryRun,
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}); saveErr != nil {
			return saveErr
		}
		return err
	}
	phase := "Applied"
	if c.DryRun && changed {
		phase = "Rendered"
	}
	status := map[string]any{
		"phase":     phase,
		"unitName":  unitName,
		"path":      path,
		"source":    source,
		"changed":   changed,
		"dryRun":    c.DryRun,
		"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, status); err != nil {
		return err
	}
	if changed && !c.DryRun && c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.service_unit.applied", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "ServiceUnit", Name: unitName}
		event.Attributes = map[string]string{"unitName": unitName, "path": path, "source": source}
		return c.Bus.Publish(ctx, event)
	}
	return nil
}

func (c SystemdUnitController) reconcileLongLivedSystemdHelperUnit(ctx context.Context, unitName, source string, spec api.SystemdUnitSpec, command outputCommandFunc) error {
	path := filepath.Join(c.SystemdSystemDir, unitName)
	changed, err := c.applyLongLivedSystemdUnit(ctx, path, unitName, spec, command)
	if err != nil {
		if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, map[string]any{
			"phase":     "Error",
			"reason":    "ApplyFailed",
			"unitName":  unitName,
			"path":      path,
			"source":    source,
			"error":     err.Error(),
			"dryRun":    c.DryRun,
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}); saveErr != nil {
			return saveErr
		}
		return err
	}
	phase := "Applied"
	if c.DryRun && changed {
		phase = "Rendered"
	}
	status := map[string]any{
		"phase":              phase,
		"unitName":           unitName,
		"path":               path,
		"source":             source,
		"changed":            changed,
		"dryRun":             c.DryRun,
		"restartOnReconcile": false,
		"updatedAt":          time.Now().UTC().Format(time.RFC3339Nano),
	}
	return c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, status)
}

func (c SystemdUnitController) cleanupLongLivedSystemdHelperUnit(ctx context.Context, unitName, source string, command outputCommandFunc) error {
	if c.SystemdSystemDir == "" {
		defaults, _ := platform.Current()
		c.SystemdSystemDir = defaults.SystemdSystemDir
	}
	path := filepath.Join(c.SystemdSystemDir, unitName)
	changed, err := c.applySystemdUnit(ctx, unitName, path, unitName, api.SystemdUnitSpec{State: "absent"}, command)
	if err != nil {
		if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, map[string]any{
			"phase":     "Error",
			"reason":    "CleanupFailed",
			"unitName":  unitName,
			"path":      path,
			"source":    source,
			"error":     err.Error(),
			"dryRun":    c.DryRun,
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}); saveErr != nil {
			return saveErr
		}
		return err
	}
	if changed {
		return c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, map[string]any{
			"phase":     "Removed",
			"unitName":  unitName,
			"path":      path,
			"source":    source,
			"changed":   true,
			"dryRun":    c.DryRun,
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	return nil
}

type synthesizedUnitRef struct {
	APIVersion string
	Kind       string
	Name       string
	UnitName   string
	Source     string
}

func (c SystemdUnitController) cleanupWhenFalseSystemdUnits(ctx context.Context, command outputCommandFunc) error {
	if c.DeclaredRouter == nil {
		return nil
	}
	stateStore, ok := c.Store.(resourcequery.StateStore)
	if !ok {
		return nil
	}
	for _, resource := range c.DeclaredRouter.Spec.Resources {
		when := resourcequery.ResourceWhen(resource)
		if !resourcequery.ResourceWhenPresent(when) || resourcequery.ResourceWhenMatches(when, stateStore) {
			continue
		}
		ref, ok := synthesizedUnitForResource(resource)
		if !ok {
			continue
		}
		if err := c.cleanupWhenFalseSystemdUnit(ctx, ref, command); err != nil {
			return err
		}
	}
	return nil
}

func synthesizedUnitForResource(resource api.Resource) (synthesizedUnitRef, bool) {
	name := strings.TrimSpace(resource.Metadata.Name)
	if name == "" {
		return synthesizedUnitRef{}, false
	}
	apiVersion := resource.APIVersion
	if apiVersion == "" {
		apiVersion = resourcequery.APIVersionForKind(resource.Kind)
	}
	ref := synthesizedUnitRef{
		APIVersion: apiVersion,
		Kind:       resource.Kind,
		Name:       name,
		Source:     resource.Kind + "/" + name,
	}
	switch resource.Kind {
	case "TailscaleNode":
		ref.UnitName = render.TailscaleUnitName(name)
	case "DHCPv4Client":
		ref.UnitName = "routerd-dhcpv4-client@" + name + ".service"
	case "DHCPv6PrefixDelegation":
		ref.UnitName = "routerd-dhcpv6-client@" + name + ".service"
	case "IPv6RouterAdvertisement":
		ref.UnitName = "routerd-ra-observer@" + name + ".service"
	case "DNSResolver":
		ref.UnitName = dnsResolverUnitName(name)
	case "EventGroup":
		ref.UnitName = eventFederationUnitName(name)
	default:
		return synthesizedUnitRef{}, false
	}
	return ref, true
}

func (c SystemdUnitController) cleanupWhenFalseSystemdUnit(ctx context.Context, ref synthesizedUnitRef, command outputCommandFunc) error {
	path := filepath.Join(c.SystemdSystemDir, ref.UnitName)
	changed, err := c.applySystemdUnit(ctx, ref.Name, path, ref.UnitName, api.SystemdUnitSpec{State: "absent"}, command)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err != nil {
		status := map[string]any{
			"phase":     "Error",
			"reason":    "WhenFalseCleanupFailed",
			"unitName":  ref.UnitName,
			"path":      path,
			"source":    ref.Source,
			"error":     err.Error(),
			"dryRun":    c.DryRun,
			"updatedAt": now,
		}
		if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", ref.UnitName, status); saveErr != nil {
			return saveErr
		}
		return fmt.Errorf("cleanup when-false unit %s: %w", ref.UnitName, err)
	}
	unitPhase := "Removed"
	if c.DryRun && changed {
		unitPhase = "Rendered"
	}
	if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", ref.UnitName, map[string]any{
		"phase":     unitPhase,
		"reason":    "WhenFalse",
		"unitName":  ref.UnitName,
		"path":      path,
		"source":    ref.Source,
		"changed":   changed,
		"dryRun":    c.DryRun,
		"updatedAt": now,
	}); err != nil {
		return err
	}
	if ref.APIVersion != "" && ref.Kind != "" && ref.Name != "" {
		if err := c.Store.SaveObjectStatus(ref.APIVersion, ref.Kind, ref.Name, map[string]any{
			"phase":     "Pending",
			"reason":    "WhenFalse",
			"unitName":  ref.UnitName,
			"managedBy": "systemd",
			"dryRun":    c.DryRun,
			"updatedAt": now,
		}); err != nil {
			return err
		}
	}
	if changed && !c.DryRun && c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.service_unit.removed", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "ServiceUnit", Name: ref.UnitName}
		event.Reason = "WhenFalse"
		event.Attributes = map[string]string{"unitName": ref.UnitName, "path": path, "source": ref.Source}
		return c.Bus.Publish(ctx, event)
	}
	return nil
}

func interfaceAliases(router *api.Router) map[string]string {
	out := map[string]string{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "Interface" {
			continue
		}
		spec, err := resource.InterfaceSpec()
		if err != nil || strings.TrimSpace(spec.IfName) == "" {
			continue
		}
		out[resource.Metadata.Name] = spec.IfName
	}
	return out
}

func dhcpv4ClientUnitSpec(resource, ifname string, spec api.DHCPv4ClientSpec, telemetryEnv []string) api.SystemdUnitSpec {
	noNewPrivileges := true
	privateTmp := true
	exec := []string{"/usr/local/sbin/routerd-dhcpv4-client", "daemon", "--resource", resource, "--interface", ifname}
	if spec.Hostname != "" {
		exec = append(exec, "--hostname", spec.Hostname)
	}
	if spec.RequestedAddress != "" {
		exec = append(exec, "--requested-address", spec.RequestedAddress)
	}
	if spec.ClassID != "" {
		exec = append(exec, "--class-id", spec.ClassID)
	}
	if spec.ClientID != "" {
		exec = append(exec, "--client-id", spec.ClientID)
	}
	return api.SystemdUnitSpec{
		Description:              "routerd DHCPv4 client " + resource,
		ExecStart:                exec,
		Environment:              telemetryEnv,
		Wants:                    []string{"network-online.target"},
		After:                    []string{"network-online.target"},
		WantedBy:                 []string{"multi-user.target"},
		Restart:                  "always",
		RestartSec:               "5s",
		RuntimeDirectory:         []string{"routerd/dhcpv4-client"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd/dhcpv4-client"},
		LogsDirectory:            []string{"routerd"},
		ReadWritePaths:           []string{"/run/routerd", "/var/lib/routerd", "/var/log/routerd"},
		AmbientCapabilities:      []string{"CAP_NET_RAW", "CAP_NET_ADMIN", "CAP_NET_BIND_SERVICE"},
		CapabilityBoundingSet:    []string{"CAP_NET_RAW", "CAP_NET_ADMIN", "CAP_NET_BIND_SERVICE"},
		RestrictAddressFamilies:  []string{"AF_UNIX", "AF_INET", "AF_INET6", "AF_NETLINK", "AF_PACKET"},
		ProtectSystem:            "strict",
		ProtectHome:              "yes",
		NoNewPrivileges:          &noNewPrivileges,
		PrivateTmp:               &privateTmp,
	}
}

func dhcpv6ClientUnitSpec(resource, ifname string, spec api.DHCPv6PrefixDelegationSpec, telemetryEnv []string) api.SystemdUnitSpec {
	noNewPrivileges := true
	privateTmp := true
	exec := []string{"/usr/local/sbin/routerd-dhcpv6-client", "daemon", "--resource", resource, "--interface", ifname}
	if spec.IAID != "" {
		exec = append(exec, "--iaid", spec.IAID)
	}
	if spec.ClientDUID != "" {
		exec = append(exec, "--client-duid", spec.ClientDUID)
	}
	return api.SystemdUnitSpec{
		Description:              "routerd DHCPv6 client " + resource,
		ExecStart:                exec,
		Environment:              telemetryEnv,
		Wants:                    []string{"network-online.target"},
		After:                    []string{"network-online.target"},
		WantedBy:                 []string{"multi-user.target"},
		Restart:                  "always",
		RestartSec:               "5s",
		RuntimeDirectory:         []string{"routerd/dhcpv6-client"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd/dhcpv6-client"},
		LogsDirectory:            []string{"routerd"},
		ReadWritePaths:           []string{"/run/routerd", "/var/lib/routerd", "/var/log/routerd"},
		AmbientCapabilities:      []string{"CAP_NET_RAW", "CAP_NET_ADMIN", "CAP_NET_BIND_SERVICE"},
		CapabilityBoundingSet:    []string{"CAP_NET_RAW", "CAP_NET_ADMIN", "CAP_NET_BIND_SERVICE"},
		RestrictAddressFamilies:  []string{"AF_UNIX", "AF_INET", "AF_INET6", "AF_NETLINK"},
		ProtectSystem:            "strict",
		ProtectHome:              "yes",
		NoNewPrivileges:          &noNewPrivileges,
		PrivateTmp:               &privateTmp,
	}
}

func raObserverUnitSpec(resource, ifname string, telemetryEnv []string) api.SystemdUnitSpec {
	noNewPrivileges := true
	privateTmp := true
	exec := []string{
		"/usr/local/sbin/routerd-ra-observer", "daemon",
		"--resource", resource,
		"--interface", ifname,
		"--socket", "/run/routerd/ra-observer/" + resource + ".sock",
		"--event-file", "/var/log/routerd/ra-observer-" + resource + ".events.jsonl",
	}
	return api.SystemdUnitSpec{
		Description:              "routerd IPv6 RA observer " + resource,
		ExecStart:                exec,
		Environment:              telemetryEnv,
		Wants:                    []string{"network-online.target"},
		After:                    []string{"network-online.target"},
		WantedBy:                 []string{"multi-user.target"},
		Restart:                  "always",
		RestartSec:               "5s",
		RuntimeDirectory:         []string{"routerd/ra-observer"},
		RuntimeDirectoryPreserve: "yes",
		LogsDirectory:            []string{"routerd"},
		ReadWritePaths:           []string{"/run/routerd", "/var/log/routerd"},
		AmbientCapabilities:      []string{"CAP_NET_RAW"},
		CapabilityBoundingSet:    []string{"CAP_NET_RAW"},
		RestrictAddressFamilies:  []string{"AF_UNIX", "AF_PACKET"},
		ProtectSystem:            "strict",
		ProtectHome:              "yes",
		NoNewPrivileges:          &noNewPrivileges,
		PrivateTmp:               &privateTmp,
	}
}

func healthCheckUnitName(name string) string {
	return "routerd-healthcheck@" + name + ".service"
}

func mergeStringEnvs(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(base)+len(extra))
	for _, value := range append(extra, base...) {
		key, _, _ := strings.Cut(value, "=")
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func maybeAugmentRouterdServiceAccess(router *api.Router, unitName string, spec api.SystemdUnitSpec) api.SystemdUnitSpec {
	if unitName != "routerd.service" || !routerNeedsKeepalivedAccess(router) || firstNonEmpty(spec.State, "present") == "absent" {
		return spec
	}
	spec.AmbientCapabilities = appendMissingStrings(spec.AmbientCapabilities, "CAP_DAC_OVERRIDE")
	spec.CapabilityBoundingSet = appendMissingStrings(spec.CapabilityBoundingSet, "CAP_DAC_OVERRIDE")
	return spec
}

func routerNeedsKeepalivedAccess(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, res := range router.Spec.Resources {
		switch {
		case res.APIVersion == api.FirewallAPIVersion && res.Kind == "IngressService":
			return true
		case res.APIVersion == api.NetAPIVersion && res.Kind == "VirtualAddress":
			spec, err := res.VirtualAddressSpec()
			if err == nil && firstNonEmpty(spec.Mode, "static") == "vrrp" {
				return true
			}
		}
	}
	return false
}

func appendMissingStrings(values []string, additions ...string) []string {
	out := append([]string(nil), values...)
	for _, addition := range additions {
		addition = strings.TrimSpace(addition)
		if addition == "" {
			continue
		}
		found := false
		for _, existing := range out {
			if existing == addition {
				found = true
				break
			}
		}
		if !found {
			out = append(out, addition)
		}
	}
	return out
}

func (c SystemdUnitController) applyHealthCheckSystemdUnit(ctx context.Context, path, unitName, resourceName string, spec api.HealthCheckSpec, telemetryEnv []string, command outputCommandFunc) (bool, error) {
	socket := filepath.Join("/run/routerd/healthcheck", resourceName+".sock")
	resolved := healthcheck.ResolveSpecWithStoreForResource(c.Router, c.Store, resourceName, spec)
	data := render.HealthCheckSystemdUnit(render.HealthCheckSystemdOptions{
		Resource:        resourceName,
		Target:          resolved.Target,
		Protocol:        resolved.Protocol,
		Via:             resolved.Via,
		FwMark:          resolved.FwMark,
		SourceInterface: resolved.SourceInterface,
		SourceAddress:   resolved.SourceAddress,
		Port:            resolved.Port,
		Interval:        resolved.Interval,
		Timeout:         resolved.Timeout,
		SocketPath:      socket,
		StateFile:       filepath.Join("/var/lib/routerd/healthcheck", resourceName, "state.json"),
		EventFile:       filepath.Join("/var/lib/routerd/healthcheck", resourceName, "events.jsonl"),
		Environment:     telemetryEnv,
	})
	changed, err := writeFileIfChanged(path, data, 0644, c.DryRun)
	if err != nil {
		return changed, err
	}
	if healthCheckDisabled(spec) {
		if c.DryRun {
			return changed, nil
		}
		if changed {
			if _, err := command(ctx, "systemctl", "daemon-reload"); err != nil {
				return changed, err
			}
		}
		if changed || systemdUnitEnabledOrActive(ctx, command, unitName) {
			_, _ = command(ctx, "systemctl", "disable", "--now", unitName)
			_, _ = command(ctx, "systemctl", "reset-failed", unitName)
		}
		return changed, nil
	}
	if c.DryRun {
		return changed, nil
	}
	if changed {
		if _, err := command(ctx, "systemctl", "daemon-reload"); err != nil {
			return changed, err
		}
		if _, err := command(ctx, "systemctl", "enable", unitName); err != nil {
			return changed, err
		}
	}
	active := true
	if _, err := command(ctx, "systemctl", "is-active", "--quiet", unitName); err != nil {
		active = false
	}
	if changed || !active {
		if _, err := command(ctx, "systemctl", "restart", unitName); err != nil {
			return changed, err
		}
		return true, nil
	}
	return changed, nil
}

func (c SystemdUnitController) cleanupStaleHealthCheckUnits(ctx context.Context, explicitUnits map[string]bool, command outputCommandFunc) error {
	if c.DryRun {
		return nil
	}
	desired := map[string]bool{}
	for unitName := range explicitUnits {
		desired[unitName] = true
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "HealthCheck" {
			continue
		}
		if _, err := resource.HealthCheckSpec(); err != nil {
			return err
		}
		desired[healthCheckUnitName(resource.Metadata.Name)] = true
	}
	matches, err := filepath.Glob(filepath.Join(c.SystemdSystemDir, "routerd-healthcheck@*.service"))
	if err != nil {
		return err
	}
	var removed bool
	for _, path := range matches {
		unitName := filepath.Base(path)
		if desired[unitName] {
			continue
		}
		_, _ = command(ctx, "systemctl", "disable", "--now", unitName)
		_, _ = command(ctx, "systemctl", "reset-failed", unitName)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		removed = true
	}
	if removed {
		if _, err := command(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	return nil
}

func (c SystemdUnitController) cleanupStaleDNSResolverUnits(ctx context.Context, command outputCommandFunc) error {
	if c.DryRun {
		return nil
	}
	desired := map[string]bool{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DNSResolver" {
			continue
		}
		if _, err := resource.DNSResolverSpec(); err != nil {
			return err
		}
		desired[dnsResolverUnitName(resource.Metadata.Name)] = true
	}
	matches, err := filepath.Glob(filepath.Join(c.SystemdSystemDir, "routerd-dns-resolver@*.service"))
	if err != nil {
		return err
	}
	var removed bool
	for _, path := range matches {
		unitName := filepath.Base(path)
		if desired[unitName] {
			continue
		}
		status := map[string]any{
			"phase":     "Removing",
			"reason":    "StaleDNSResolverUnit",
			"unitName":  unitName,
			"path":      path,
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}
		if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, status); err != nil {
			return err
		}
		if _, err := command(ctx, "systemctl", "disable", "--now", unitName); err != nil {
			status["phase"] = "Error"
			status["error"] = err.Error()
			status["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
			if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, status); saveErr != nil {
				return saveErr
			}
			return fmt.Errorf("disable stale DNS resolver unit %s: %w", unitName, err)
		}
		_, _ = command(ctx, "systemctl", "reset-failed", unitName)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		status["phase"] = "Removed"
		status["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, status); err != nil {
			return err
		}
		removed = true
	}
	if removed {
		if _, err := command(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	return nil
}

func (c SystemdUnitController) cleanupStaleEventFederationUnits(ctx context.Context, command outputCommandFunc) error {
	if c.DryRun {
		return nil
	}
	desired := map[string]bool{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "EventGroup" {
			continue
		}
		if _, err := resource.EventGroupSpec(); err != nil {
			return err
		}
		desired[eventFederationUnitName(resource.Metadata.Name)] = true
	}
	matches, err := filepath.Glob(filepath.Join(c.SystemdSystemDir, "routerd-eventd@*.service"))
	if err != nil {
		return err
	}
	legacyPath := filepath.Join(c.SystemdSystemDir, "routerd-eventd.service")
	legacyPresent, err := legacyEventdUnitNeedsCleanup(ctx, command, legacyPath)
	if err != nil {
		return err
	}
	if legacyPresent {
		matches = append(matches, legacyPath)
	}
	var removed bool
	for _, path := range matches {
		unitName := filepath.Base(path)
		if desired[unitName] {
			continue
		}
		reason := "StaleEventFederationUnit"
		if unitName == "routerd-eventd.service" {
			reason = "LegacyEventFederationUnit"
		}
		status := map[string]any{
			"phase":     "Removing",
			"reason":    reason,
			"unitName":  unitName,
			"path":      path,
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}
		if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, status); err != nil {
			return err
		}
		if _, err := command(ctx, "systemctl", "disable", "--now", unitName); err != nil {
			status["phase"] = "Error"
			status["error"] = err.Error()
			status["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
			if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, status); saveErr != nil {
				return saveErr
			}
			return fmt.Errorf("disable stale event federation unit %s: %w", unitName, err)
		}
		_, _ = command(ctx, "systemctl", "reset-failed", unitName)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		status["phase"] = "Removed"
		status["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, status); err != nil {
			return err
		}
		removed = true
	}
	if removed {
		if _, err := command(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	return nil
}

func legacyEventdUnitNeedsCleanup(ctx context.Context, command outputCommandFunc, path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return true, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return systemdUnitEnabledOrActive(ctx, command, "routerd-eventd.service"), nil
}

func (c SystemdUnitController) cleanupStaleClientDaemonUnits(ctx context.Context, command outputCommandFunc) error {
	if c.DryRun {
		return nil
	}
	patterns := []string{
		"routerd-dhcpv4-client@*.service",
		"routerd-dhcpv6-client@*.service",
		"routerd-pppoe-client@*.service",
	}
	var removed bool
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(c.SystemdSystemDir, pattern))
		if err != nil {
			return err
		}
		for _, path := range matches {
			unitName := filepath.Base(path)
			active := systemdUnitActive(ctx, command, unitName)
			status := map[string]any{
				"phase":     "Removing",
				"reason":    "StaleClientDaemonUnit",
				"unitName":  unitName,
				"path":      path,
				"active":    active,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}
			if active {
				status["phase"] = "Pending"
				status["reason"] = "StaleClientDaemonUnitActive"
				status["message"] = "stale client daemon unit is active; cleanup deferred to avoid service interruption"
				if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, status); err != nil {
					return err
				}
				if c.Bus != nil {
					event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.service_unit.stale_cleanup_deferred", daemonapi.SeverityWarning)
					event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "ServiceUnit", Name: unitName}
					event.Reason = "StaleClientDaemonUnitActive"
					event.Attributes = map[string]string{"unitName": unitName, "path": path, "active": "true"}
					if err := c.Bus.Publish(ctx, event); err != nil {
						return err
					}
				}
				continue
			}
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, status); err != nil {
				return err
			}
			if _, err := command(ctx, "systemctl", "disable", "--now", unitName); err != nil {
				status["phase"] = "Error"
				status["error"] = err.Error()
				status["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
				if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, status); saveErr != nil {
					return saveErr
				}
				if c.Bus != nil {
					event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.service_unit.stale_cleanup_failed", daemonapi.SeverityError)
					event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "ServiceUnit", Name: unitName}
					event.Reason = "StaleClientDaemonUnitCleanupFailed"
					event.Attributes = map[string]string{"unitName": unitName, "path": path, "active": fmt.Sprint(active), "error": err.Error()}
					if publishErr := c.Bus.Publish(ctx, event); publishErr != nil {
						return publishErr
					}
				}
				return fmt.Errorf("disable stale client daemon unit %s: %w", unitName, err)
			}
			_, _ = command(ctx, "systemctl", "reset-failed", unitName)
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			status["phase"] = "Removed"
			status["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName, status); err != nil {
				return err
			}
			if c.Bus != nil {
				event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.service_unit.stale_removed", daemonapi.SeverityInfo)
				event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "ServiceUnit", Name: unitName}
				event.Reason = "StaleClientDaemonUnit"
				event.Attributes = map[string]string{"unitName": unitName, "path": path, "active": fmt.Sprint(active)}
				if err := c.Bus.Publish(ctx, event); err != nil {
					return err
				}
			}
			removed = true
		}
	}
	if removed {
		if _, err := command(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	return nil
}

func systemdUnitActive(ctx context.Context, command outputCommandFunc, unitName string) bool {
	_, err := command(ctx, "systemctl", "is-active", "--quiet", unitName)
	return err == nil
}

func (c SystemdUnitController) applySystemdUnit(ctx context.Context, name, path, unitName string, spec api.SystemdUnitSpec, command outputCommandFunc) (bool, error) {
	state := firstNonEmpty(spec.State, "present")
	var changed bool
	if state == "absent" {
		removed, err := removeFileIfExists(path, c.DryRun)
		if err != nil {
			return changed, err
		}
		changed = removed
		if c.DryRun {
			return changed, nil
		}
		if removed {
			if _, err := command(ctx, "systemctl", "daemon-reload"); err != nil {
				return changed, err
			}
		}
		if removed || systemdUnitEnabledOrActive(ctx, command, unitName) {
			_, _ = command(ctx, "systemctl", "disable", "--now", unitName)
		}
		return changed, nil
	}
	data := render.SystemdUnit(name, spec)
	fileChanged, err := writeFileIfChanged(path, data, 0644, c.DryRun)
	if err != nil {
		return changed, err
	}
	changed = changed || fileChanged
	if c.DryRun {
		return changed, nil
	}
	if fileChanged {
		if _, err := command(ctx, "systemctl", "daemon-reload"); err != nil {
			return changed, err
		}
	}
	if api.BoolDefault(spec.Enabled, true) {
		if fileChanged || !systemdUnitEnabled(ctx, command, unitName) {
			if _, err := command(ctx, "systemctl", "unmask", unitName); err != nil {
				return changed, err
			}
			if _, err := command(ctx, "systemctl", "enable", unitName); err != nil {
				return changed, err
			}
		}
	} else if systemdUnitEnabledOrActive(ctx, command, unitName) {
		if _, err := command(ctx, "systemctl", "disable", "--now", unitName); err != nil {
			return changed, err
		}
	}
	if api.BoolDefault(spec.Started, true) {
		if unitName == "routerd.service" && fileChanged {
			if err := scheduleRouterdServiceRestart(ctx, command); err != nil {
				return changed, err
			}
			return true, nil
		}
		active := true
		if !fileChanged {
			if _, err := command(ctx, "systemctl", "is-active", "--quiet", unitName); err != nil {
				active = false
			}
		}
		if fileChanged || !active {
			if _, err := command(ctx, "systemctl", "restart", unitName); err != nil {
				return changed, err
			}
			return true, nil
		}
	}
	return changed, nil
}

func (c SystemdUnitController) applyLongLivedSystemdUnit(ctx context.Context, path, unitName string, spec api.SystemdUnitSpec, command outputCommandFunc) (bool, error) {
	data := render.SystemdUnit(unitName, spec)
	fileChanged, err := writeFileIfChanged(path, data, 0644, c.DryRun)
	if err != nil {
		return false, err
	}
	if c.DryRun {
		return fileChanged, nil
	}
	if fileChanged {
		if _, err := command(ctx, "systemctl", "daemon-reload"); err != nil {
			return fileChanged, err
		}
	}
	if api.BoolDefault(spec.Enabled, true) {
		if fileChanged || !systemdUnitEnabled(ctx, command, unitName) {
			if _, err := command(ctx, "systemctl", "unmask", unitName); err != nil {
				return true, err
			}
			if _, err := command(ctx, "systemctl", "enable", unitName); err != nil {
				return true, err
			}
		}
	} else if systemdUnitEnabledOrActive(ctx, command, unitName) {
		if _, err := command(ctx, "systemctl", "disable", "--now", unitName); err != nil {
			return true, err
		}
		return true, nil
	}
	if api.BoolDefault(spec.Started, true) {
		if _, err := command(ctx, "systemctl", "is-active", "--quiet", unitName); err != nil {
			if _, err := command(ctx, "systemctl", "start", unitName); err != nil {
				return true, err
			}
			return true, nil
		}
	}
	return fileChanged, nil
}

func routerHasBGP(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.NetAPIVersion && resource.Kind == "BGPRouter" {
			return true
		}
	}
	return false
}

func systemdUnitEnabledOrActive(ctx context.Context, command outputCommandFunc, unitName string) bool {
	if _, err := command(ctx, "systemctl", "is-active", "--quiet", unitName); err == nil {
		return true
	}
	return systemdUnitEnabled(ctx, command, unitName)
}

func systemdUnitEnabled(ctx context.Context, command outputCommandFunc, unitName string) bool {
	_, err := command(ctx, "systemctl", "is-enabled", "--quiet", unitName)
	return err == nil
}

func scheduleRouterdServiceRestart(ctx context.Context, command outputCommandFunc) error {
	unitName := fmt.Sprintf("routerd-self-restart-%d-%d.service", os.Getpid(), time.Now().UnixNano())
	_, err := command(ctx,
		"systemd-run",
		"--unit", unitName,
		"--description", "Restart routerd after managed unit update",
		"--on-active=10s",
		"--collect",
		"systemctl", "restart", "routerd.service",
	)
	if err != nil {
		return fmt.Errorf("schedule routerd.service restart through systemd-run: %w", err)
	}
	return nil
}

func networkdAdoptionDropin(spec api.NetworkAdoptionNetworkdSpec) []byte {
	var b strings.Builder
	b.WriteString("# Managed by routerd. Do not edit by hand.\n[Network]\n")
	if spec.DisableDHCPv4 && spec.DisableDHCPv6 {
		b.WriteString("DHCP=no\n")
	} else if spec.DisableDHCPv4 {
		b.WriteString("DHCP=ipv6\n")
	} else if spec.DisableDHCPv6 {
		b.WriteString("DHCP=ipv4\n")
	}
	if spec.DisableIPv6RA {
		b.WriteString("IPv6AcceptRA=no\n")
	}
	if spec.DisableDHCPv6 && !spec.DisableIPv6RA {
		b.WriteString("IPv6AcceptRA=yes\n")
		b.WriteString("\n[IPv6AcceptRA]\nDHCPv6Client=no\n")
	}
	if spec.DHCPv4UseRoutes != nil || spec.DHCPv4UseDNS != nil || spec.DHCPv4RouteMetric != 0 {
		b.WriteString("\n[DHCPv4]\n")
		if spec.DHCPv4UseRoutes != nil {
			b.WriteString("UseRoutes=" + systemdBool(*spec.DHCPv4UseRoutes) + "\n")
		}
		if spec.DHCPv4UseDNS != nil {
			b.WriteString("UseDNS=" + systemdBool(*spec.DHCPv4UseDNS) + "\n")
		}
		if spec.DHCPv4RouteMetric != 0 {
			b.WriteString(fmt.Sprintf("RouteMetric=%d\n", spec.DHCPv4RouteMetric))
		}
	}
	return []byte(b.String())
}

func networkdAdoptionConfigured(spec api.NetworkAdoptionNetworkdSpec) bool {
	return spec.DisableDHCPv4 ||
		spec.DisableDHCPv6 ||
		spec.DisableIPv6RA ||
		spec.DHCPv4UseRoutes != nil ||
		spec.DHCPv4UseDNS != nil ||
		spec.DHCPv4RouteMetric != 0
}

func systemdBool(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func writeFileIfChanged(path string, data []byte, mode os.FileMode, dryRun bool) (bool, error) {
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, data) {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if dryRun {
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return false, err
	}
	return true, nil
}

func removeFileIfExists(path string, dryRun bool) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if dryRun {
		return true, nil
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
}

var unsafeNetworkdName = regexp.MustCompile(`[^A-Za-z0-9_.:-]+`)

func networkdDropinDir(base, ifname string) string {
	if base == "" {
		base = "/etc/systemd/network"
	}
	name := strings.Trim(unsafeNetworkdName.ReplaceAllString(ifname, "-"), "-.")
	if name == "" {
		name = "interface"
	}
	return filepath.Join(base, "10-netplan-"+name+".network.d")
}
