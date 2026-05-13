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

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
	"routerd/pkg/healthcheck"
	"routerd/pkg/platform"
	"routerd/pkg/render"
	"routerd/pkg/tailscale"
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
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "NetworkAdoption" {
			continue
		}
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
	Router *api.Router
	Bus    interface {
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
		explicitUnits := systemdUnitNames(c.Router)
		if c.SynthesizeClientDaemonUnits {
			if err := c.reconcileClientDaemonUnits(ctx, explicitUnits, telemetryEnv, command); err != nil {
				return err
			}
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
		for _, resource := range c.Router.Spec.Resources {
			if resource.Kind != "HealthCheck" {
				continue
			}
			spec, err := resource.HealthCheckSpec()
			if err != nil {
				return err
			}
			if spec.Daemon != healthcheck.DaemonKind {
				continue
			}
			unitName := healthCheckUnitName(resource.Metadata.Name)
			if explicitUnits[unitName] {
				continue
			}
			path := filepath.Join(c.SystemdSystemDir, unitName)
			changed, err := c.applyHealthCheckSystemdUnit(ctx, path, unitName, resource.Metadata.Name, spec, telemetryEnv, command)
			if err != nil {
				if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "SystemdUnit", unitName, map[string]any{
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
					"daemon":    spec.Daemon,
					"unitName":  unitName,
					"dryRun":    c.DryRun,
					"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
				}); err != nil {
					return err
				}
			}
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "SystemdUnit", unitName, map[string]any{
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
				event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.systemd_unit.applied", daemonapi.SeverityInfo)
				event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "SystemdUnit", Name: unitName}
				event.Attributes = map[string]string{"unitName": unitName, "path": path, "source": "HealthCheck/" + resource.Metadata.Name}
				if err := c.Bus.Publish(ctx, event); err != nil {
					return err
				}
			}
		}
	}
	if err := c.reconcileDisabledPPPoEInterfaces(); err != nil {
		return err
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "SystemdUnit" {
			continue
		}
		spec, err := resource.SystemdUnitSpec()
		if err != nil {
			return err
		}
		unitName := firstNonEmpty(spec.UnitName, resource.Metadata.Name)
		if !features.HasSystemd {
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "SystemdUnit", resource.Metadata.Name, map[string]any{
				"phase":     "Pending",
				"reason":    "SystemdUnsupported",
				"unitName":  unitName,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}); err != nil {
				return err
			}
			continue
		}
		path := filepath.Join(c.SystemdSystemDir, unitName)
		spec.Environment = mergeStringEnvs(spec.Environment, telemetryEnv)
		changed, err := c.applySystemdUnit(ctx, resource.Metadata.Name, path, unitName, spec, command)
		if err != nil {
			if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "SystemdUnit", resource.Metadata.Name, map[string]any{
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
		if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "SystemdUnit", resource.Metadata.Name, map[string]any{
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
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.systemd_unit.applied", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "SystemdUnit", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"unitName": unitName, "path": path}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c SystemdUnitController) reconcileDisabledPPPoEInterfaces() error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "PPPoEInterface" {
			continue
		}
		spec, err := resource.PPPoEInterfaceSpec()
		if err != nil {
			return err
		}
		if !pppoeInterfaceDisabled(spec) {
			continue
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "PPPoEInterface", resource.Metadata.Name, map[string]any{
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
		case "DHCPv4Lease":
			spec, err := resource.DHCPv4LeaseSpec()
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
			if err := c.reconcileSyntheticSystemdUnit(ctx, api.NetAPIVersion, "DHCPv4Lease", resource.Metadata.Name, unitName, dhcpv4ClientUnitSpec(resource.Metadata.Name, ifname, spec, telemetryEnv), command); err != nil {
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
		}
	}
	return nil
}

func (c SystemdUnitController) reconcileSyntheticSystemdUnit(ctx context.Context, apiVersion, kind, resourceName, unitName string, spec api.SystemdUnitSpec, command outputCommandFunc) error {
	path := filepath.Join(c.SystemdSystemDir, unitName)
	changed, err := c.applySystemdUnit(ctx, resourceName, path, unitName, spec, command)
	if err != nil {
		if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "SystemdUnit", unitName, map[string]any{
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
	if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "SystemdUnit", unitName, status); err != nil {
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
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.systemd_unit.applied", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "SystemdUnit", Name: unitName}
		event.Attributes = map[string]string{"unitName": unitName, "path": path, "source": kind + "/" + resourceName}
		return c.Bus.Publish(ctx, event)
	}
	return nil
}

func systemdUnitNames(router *api.Router) map[string]bool {
	out := map[string]bool{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "SystemdUnit" {
			continue
		}
		spec, err := resource.SystemdUnitSpec()
		if err != nil {
			continue
		}
		out[firstNonEmpty(spec.UnitName, resource.Metadata.Name)] = true
	}
	return out
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

func dhcpv4ClientUnitSpec(resource, ifname string, spec api.DHCPv4LeaseSpec, telemetryEnv []string) api.SystemdUnitSpec {
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

func (c SystemdUnitController) applyHealthCheckSystemdUnit(ctx context.Context, path, unitName, resourceName string, spec api.HealthCheckSpec, telemetryEnv []string, command outputCommandFunc) (bool, error) {
	socket := spec.SocketSource
	if socket == "" {
		socket = filepath.Join("/run/routerd/healthcheck", resourceName+".sock")
	}
	resolved := healthcheck.ResolveSpecWithStore(c.Router, c.Store, spec)
	data := render.HealthCheckSystemdUnit(render.HealthCheckSystemdOptions{
		Resource:        resourceName,
		Target:          resolved.Target,
		Protocol:        resolved.Protocol,
		Via:             resolved.Via,
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
		_, _ = command(ctx, "systemctl", "disable", "--now", unitName)
		_, _ = command(ctx, "systemctl", "reset-failed", unitName)
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
		if _, err := command(ctx, "systemctl", "daemon-reload"); err != nil {
			return changed, err
		}
		_, _ = command(ctx, "systemctl", "disable", "--now", unitName)
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
	if _, err := command(ctx, "systemctl", "unmask", unitName); err != nil {
		return changed, err
	}
	if api.BoolDefault(spec.Enabled, true) {
		if _, err := command(ctx, "systemctl", "enable", unitName); err != nil {
			return changed, err
		}
	}
	if api.BoolDefault(spec.Started, true) {
		if selfSystemdUnit(ctx, unitName, command) {
			return changed, nil
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

func selfSystemdUnit(ctx context.Context, unitName string, command outputCommandFunc) bool {
	_, _ = ctx, command
	return unitName == "routerd.service"
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
