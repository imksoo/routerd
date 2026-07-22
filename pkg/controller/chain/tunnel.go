// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/hybrid"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type TunnelCommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type TunnelInterfaceController struct {
	Router  *api.Router
	Bus     *bus.Bus
	Store   Store
	DryRun  bool
	Command TunnelCommandRunner
	OS      platform.OS
	Logger  *slog.Logger
}

type tunnelDesired struct {
	Name              string
	Mode              string
	Local             string
	Remote            string
	MTU               int
	TTL               int
	Key               int
	EncapSport        int
	EncapDport        int
	Address           string
	UnderlayInterface string
	UnderlayMTU       int
	Overhead          int
}

type tunnelObserved struct {
	Exists     bool
	Mode       string
	Local      string
	Remote     string
	MTU        int
	TTL        int
	Key        int
	EncapSport int
	EncapDport int
	Up         bool
}

func (c TunnelInterfaceController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	targetOS := c.OS
	if targetOS == "" {
		targetOS = platform.CurrentOS()
	}
	if targetOS == platform.OSLinux || targetOS == platform.OSFreeBSD {
		if err := c.cleanupStaleResources(ctx); err != nil {
			return err
		}
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "TunnelInterface" {
			continue
		}
		if targetOS != platform.OSLinux && targetOS != platform.OSFreeBSD {
			if err := c.saveUnsupportedStatus(resource, targetOS); err != nil {
				return err
			}
			continue
		}
		if err := c.reconcileInterface(ctx, resource); err != nil {
			return err
		}
	}
	return nil
}

func (c TunnelInterfaceController) cleanupStaleResources(ctx context.Context) error {
	lister, ok := c.Store.(routerstate.ObjectStatusLister)
	if !ok {
		return nil
	}
	deleter, ok := c.Store.(routerstate.ObjectDeleteStore)
	if !ok {
		return nil
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return err
	}
	desired := map[string]struct{}{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion == api.HybridAPIVersion && resource.Kind == "TunnelInterface" {
			desired[resource.Metadata.Name] = struct{}{}
		}
	}
	for _, item := range statuses {
		if item.APIVersion != api.HybridAPIVersion || item.Kind != "TunnelInterface" {
			continue
		}
		if _, ok := desired[item.Name]; ok || !routerdManagedObjectStatus(item) {
			continue
		}
		ifname := firstNonEmpty(statusString(item.Status, "ifname"), statusString(item.Status, "interface"), item.Name)
		if ifname != "" && !c.DryRun {
			if err := c.deleteTunnelInterface(ctx, ifname); err != nil {
				return err
			}
		}
		if listener, ok := fouListenerFromStatus(item.Status); ok && !c.DryRun {
			if c.desiredUsesFOUListener(listener, "") {
				if err := c.transferFOUOwnership(listener, item.Name); err != nil {
					return err
				}
			} else if err := c.deleteFOUListener(ctx, listener); err != nil {
				return err
			}
		}
		if err := deleter.DeleteObject(item.APIVersion, item.Kind, item.Name); err != nil {
			return err
		}
	}
	return nil
}

func (c TunnelInterfaceController) saveUnsupportedStatus(resource api.Resource, targetOS platform.OS) error {
	spec, err := resource.TunnelInterfaceSpec()
	if err != nil {
		return err
	}
	desired := tunnelDesiredFromSpec(*c.Router, resource.Metadata.Name, spec)
	return c.Store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", resource.Metadata.Name, tunnelStatus(desired, c.DryRun, map[string]any{
		"phase":  "Unsupported",
		"reason": "PlatformUnsupported",
		"os":     string(targetOS),
	}))
}

func (c TunnelInterfaceController) reconcileInterface(ctx context.Context, resource api.Resource) error {
	spec, err := resource.TunnelInterfaceSpec()
	if err != nil {
		return err
	}
	desired, pending, pendingReason, err := c.resolveTunnelDesired(resource.Metadata.Name, spec)
	if err != nil {
		return c.saveResolveError(resource, tunnelDesiredFromSpec(*c.Router, resource.Metadata.Name, spec), err)
	}
	status := tunnelStatus(desired, c.DryRun, map[string]any{"phase": "Pending"})
	if pending {
		status["reason"] = "EndpointSourcePending"
		status["pendingSource"] = pendingReason
		return c.Store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", resource.Metadata.Name, status)
	}
	if c.DryRun {
		status["phase"] = "Planned"
		return c.Store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", resource.Metadata.Name, status)
	}
	observed, err := c.observeTunnel(ctx, desired.Name)
	if err != nil {
		status["phase"] = "Error"
		status["reason"] = "StatusFailed"
		status["error"] = err.Error()
		return c.Store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", resource.Metadata.Name, status)
	}
	previous := c.Store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", resource.Metadata.Name)
	listenerCreated, err := c.ensureFOUListener(ctx, desired)
	if err != nil {
		return c.saveApplyError(resource, desired, err)
	}
	applied := false
	created := false
	if !observed.Exists {
		if err := c.addTunnelInterface(ctx, desired); err != nil {
			return c.saveApplyError(resource, desired, err)
		}
		applied = true
		created = true
	} else if observed.Mode != "" && observed.Mode != desired.Mode {
		if err := c.deleteTunnelInterface(ctx, desired.Name); err != nil {
			return c.saveApplyError(resource, desired, err)
		}
		if err := c.addTunnelInterface(ctx, desired); err != nil {
			return c.saveApplyError(resource, desired, err)
		}
		applied = true
		created = true
	} else if !tunnelLinkMatches(observed, desired) {
		if c.targetOS() == platform.OSFreeBSD && desired.Mode == "gre" && desired.Key == 0 && observed.Key != 0 {
			return c.saveApplyError(resource, desired, fmt.Errorf("clear FreeBSD GRE key on %s is not yet native-verified; refusing to retain the old key", desired.Name))
		}
		if err := c.changeTunnelInterface(ctx, desired); err != nil {
			return c.saveApplyError(resource, desired, err)
		}
		applied = true
	}
	if desired.MTU > 0 && (observed.MTU != desired.MTU || !observed.Up || created) {
		if err := c.setTunnelLink(ctx, desired); err != nil {
			return c.saveApplyError(resource, desired, err)
		}
		applied = true
	} else if !observed.Up {
		if err := c.setTunnelLinkUp(ctx, desired.Name); err != nil {
			return c.saveApplyError(resource, desired, err)
		}
		applied = true
	}
	if desired.Address != "" || statusString(previous, "address") != "" {
		addressChanged, err := c.reconcileTunnelAddress(ctx, desired)
		if err != nil {
			return c.saveApplyError(resource, desired, err)
		}
		applied = applied || addressChanged
	}
	status = tunnelStatus(desired, c.DryRun, map[string]any{"phase": "Up"})
	if listener, ok := fouListenerForDesired(desired); ok {
		if listenerCreated || fouListenerMatchesStatus(listener, previous) || c.routerdOwnsFOUListener(listener, resource.Metadata.Name) {
			status["fouListenerOwned"] = true
		}
	}
	if previousListener, ok := fouListenerFromStatus(previous); ok && !c.DryRun && !fouListenerMatchesStatus(previousListener, status) {
		if c.desiredUsesFOUListener(previousListener, resource.Metadata.Name) {
			if err := c.transferFOUOwnership(previousListener, resource.Metadata.Name); err != nil {
				return c.saveApplyError(resource, desired, err)
			}
		} else if err := c.deleteFOUListener(ctx, previousListener); err != nil {
			return c.saveApplyError(resource, desired, err)
		}
	}
	if !applied {
		status["reason"] = "AlreadyConfigured"
	}
	if err := c.Store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", resource.Metadata.Name, status); err != nil {
		return err
	}
	if applied && c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.tunnel.interface.applied", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{
			"interface": desired.Name,
			"mode":      desired.Mode,
			"dryRun":    fmt.Sprintf("%t", c.DryRun),
		}
		return c.Bus.Publish(ctx, event)
	}
	return nil
}

func (c TunnelInterfaceController) saveResolveError(resource api.Resource, desired tunnelDesired, err error) error {
	status := tunnelStatus(desired, c.DryRun, map[string]any{
		"phase":  "Error",
		"reason": "EndpointResolveFailed",
		"error":  err.Error(),
	})
	return c.Store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", resource.Metadata.Name, status)
}

func (c TunnelInterfaceController) saveApplyError(resource api.Resource, desired tunnelDesired, applyErr error) error {
	status := tunnelStatus(desired, c.DryRun, map[string]any{
		"phase":  "Error",
		"reason": "ApplyFailed",
		"error":  applyErr.Error(),
	})
	return c.Store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", resource.Metadata.Name, status)
}

func tunnelDesiredFromSpec(router api.Router, name string, spec api.TunnelInterfaceSpec) tunnelDesired {
	estimate, _ := hybrid.TunnelInterfaceMTUEstimate(router, spec)
	mtu := estimate.EstimatedMTU
	ttl := spec.TTL
	if ttl == 0 {
		ttl = 64
	}
	return tunnelDesired{
		Name:              strings.TrimSpace(name),
		Mode:              strings.TrimSpace(spec.Mode),
		Local:             strings.TrimSpace(spec.Local),
		Remote:            strings.TrimSpace(spec.Remote),
		MTU:               mtu,
		TTL:               ttl,
		Key:               spec.Key,
		EncapSport:        spec.EncapSport,
		EncapDport:        spec.EncapDport,
		Address:           strings.TrimSpace(spec.Address),
		UnderlayInterface: strings.TrimSpace(spec.UnderlayInterface),
		UnderlayMTU:       estimate.UnderlayMTU,
		Overhead:          estimate.Overhead,
	}
}

func (c TunnelInterfaceController) resolveTunnelDesired(name string, spec api.TunnelInterfaceSpec) (tunnelDesired, bool, string, error) {
	desired := tunnelDesiredFromSpec(*c.Router, name, spec)
	if strings.TrimSpace(spec.LocalFrom.Resource) != "" {
		value, pending, err := c.tunnelEndpointFromSource(spec.LocalFrom)
		if err != nil {
			return desired, false, "", fmt.Errorf("resolve localFrom: %w", err)
		}
		if pending {
			return desired, true, statusSourceLabel(spec.LocalFrom), nil
		}
		desired.Local = value
	}
	if strings.TrimSpace(spec.RemoteFrom.Resource) != "" {
		value, pending, err := c.tunnelEndpointFromSource(spec.RemoteFrom)
		if err != nil {
			return desired, false, "", fmt.Errorf("resolve remoteFrom: %w", err)
		}
		if pending {
			return desired, true, statusSourceLabel(spec.RemoteFrom), nil
		}
		desired.Remote = value
	}
	return desired, false, "", nil
}

func (c TunnelInterfaceController) tunnelEndpointFromSource(source api.StatusValueSourceSpec) (string, bool, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(source.Resource), "/")
	if !ok || kind == "" || name == "" {
		return "", false, fmt.Errorf("resource must be Kind/name")
	}
	field := strings.TrimSpace(source.Field)
	if field == "" {
		return "", false, fmt.Errorf("field is required")
	}
	status := c.Store.ObjectStatus(c.statusSourceAPIVersion(kind, name), kind, name)
	value := statusString(status, field)
	if value == "" {
		return "", true, nil
	}
	endpoint, err := normalizeTunnelEndpoint(value)
	if err != nil {
		return "", false, err
	}
	return endpoint, false, nil
}

func (c TunnelInterfaceController) statusSourceAPIVersion(kind, name string) string {
	if c.Router != nil {
		for _, resource := range c.Router.Spec.Resources {
			if resource.Kind == kind && resource.Metadata.Name == name {
				return resource.APIVersion
			}
		}
	}
	switch kind {
	case "TunnelInterface", "OverlayPeer", "HybridRoute", "AddressMobilityDomain", "RemoteAddressClaim", "CloudProviderProfile":
		return api.HybridAPIVersion
	case "RouterdCluster", "ServiceUnit", "NetworkAdoption":
		return api.SystemAPIVersion
	default:
		return api.NetAPIVersion
	}
}

func normalizeTunnelEndpoint(value string) (string, error) {
	value = strings.TrimSpace(value)
	if prefix, err := netip.ParsePrefix(value); err == nil {
		addr := prefix.Addr()
		if !addr.Is4() {
			return "", fmt.Errorf("%q must be an IPv4 address or prefix", value)
		}
		return addr.String(), nil
	}
	addr, err := netip.ParseAddr(value)
	if err != nil || !addr.Is4() {
		return "", fmt.Errorf("%q must be an IPv4 address or prefix", value)
	}
	return addr.String(), nil
}

func statusSourceLabel(source api.StatusValueSourceSpec) string {
	field := strings.TrimSpace(source.Field)
	if field == "" {
		field = "phase"
	}
	return strings.TrimSpace(source.Resource) + "." + field
}

func tunnelStatus(desired tunnelDesired, dryRun bool, extra map[string]any) map[string]any {
	status := map[string]any{
		"interface": desired.Name,
		"ifname":    desired.Name,
		"mode":      desired.Mode,
		"local":     desired.Local,
		"remote":    desired.Remote,
		"mtu":       desired.MTU,
		"ttl":       desired.TTL,
		"dryRun":    dryRun,
	}
	if desired.Key != 0 {
		status["key"] = desired.Key
	}
	if desired.EncapSport != 0 {
		status["encapSport"] = desired.EncapSport
	}
	if desired.EncapDport != 0 {
		status["encapDport"] = desired.EncapDport
	}
	if desired.Address != "" {
		status["address"] = desired.Address
	}
	if desired.UnderlayInterface != "" {
		status["underlayInterface"] = desired.UnderlayInterface
	}
	if desired.UnderlayMTU != 0 {
		status["underlayMTU"] = desired.UnderlayMTU
	}
	if desired.Overhead != 0 {
		status["tunnelOverhead"] = desired.Overhead
	}
	for key, value := range extra {
		status[key] = value
	}
	return status
}

func tunnelLinkMatches(observed tunnelObserved, desired tunnelDesired) bool {
	if !observed.Exists {
		return false
	}
	if observed.Mode != "" && observed.Mode != desired.Mode {
		return false
	}
	if observed.Local != "" && observed.Local != desired.Local {
		return false
	}
	if observed.Remote != "" && observed.Remote != desired.Remote {
		return false
	}
	if observed.TTL != 0 && observed.TTL != desired.TTL {
		return false
	}
	if desired.Mode == "gre" && observed.Key != desired.Key {
		return false
	}
	if (desired.Mode == "fou" || desired.Mode == "gue") && observed.EncapSport != desired.EncapSport {
		return false
	}
	if (desired.Mode == "fou" || desired.Mode == "gue") && observed.EncapDport != desired.EncapDport {
		return false
	}
	return true
}

func (c TunnelInterfaceController) observeTunnel(ctx context.Context, ifname string) (tunnelObserved, error) {
	if c.targetOS() == platform.OSFreeBSD {
		out, err := c.run(ctx, "ifconfig", ifname)
		if err != nil {
			if tunnelMissingLink(out, err) {
				return tunnelObserved{}, nil
			}
			return tunnelObserved{}, fmt.Errorf("observe FreeBSD tunnel interface %s: %w: %s", ifname, err, strings.TrimSpace(string(out)))
		}
		return parseFreeBSDTunnelStatus(ifname, out), nil
	}
	out, err := c.run(ctx, "ip", "-d", "-o", "link", "show", "dev", ifname)
	if err != nil {
		if tunnelMissingLink(out, err) {
			return tunnelObserved{}, nil
		}
		return tunnelObserved{}, fmt.Errorf("observe tunnel interface %s: %w: %s", ifname, err, strings.TrimSpace(string(out)))
	}
	observed := parseTunnelLinkStatus(out)
	observed.Exists = true
	return observed, nil
}

func parseTunnelLinkStatus(out []byte) tunnelObserved {
	text := string(out)
	fields := strings.Fields(text)
	observed := tunnelObserved{Exists: strings.TrimSpace(text) != ""}
	if strings.Contains(text, "<") && strings.Contains(text, "UP") {
		observed.Up = true
	}
	for i, field := range fields {
		switch {
		case field == "mtu" && i+1 < len(fields):
			observed.MTU, _ = strconv.Atoi(fields[i+1])
		case field == "ttl" && i+1 < len(fields):
			observed.TTL, _ = strconv.Atoi(fields[i+1])
		case field == "key" && i+1 < len(fields):
			observed.Key, _ = strconv.Atoi(fields[i+1])
		case field == "encap-sport" && i+1 < len(fields):
			observed.EncapSport, _ = strconv.Atoi(fields[i+1])
		case field == "encap-dport" && i+1 < len(fields):
			observed.EncapDport, _ = strconv.Atoi(fields[i+1])
		case field == "encap" && i+1 < len(fields):
			switch strings.Trim(fields[i+1], ",") {
			case "fou":
				observed.Mode = "fou"
			case "gue":
				observed.Mode = "gue"
			}
		case field == "local" && i+1 < len(fields):
			observed.Local = strings.Trim(fields[i+1], ",")
		case (field == "remote" || field == "peer") && i+1 < len(fields):
			observed.Remote = strings.Trim(fields[i+1], ",")
		case field == "link/ipip" || strings.HasPrefix(field, "ipip/"):
			if observed.Mode == "" {
				observed.Mode = "ipip"
			}
		case field == "link/gre" || strings.HasPrefix(field, "gre/"):
			observed.Mode = "gre"
		}
	}
	return observed
}

func parseFreeBSDTunnelStatus(ifname string, out []byte) tunnelObserved {
	text := string(out)
	observed := tunnelObserved{Exists: strings.TrimSpace(text) != ""}
	if strings.Contains(text, "<") && strings.Contains(text, "UP") {
		observed.Up = true
	}
	if strings.HasPrefix(ifname, "gif") {
		observed.Mode = "ipip"
	} else if strings.HasPrefix(ifname, "gre") {
		observed.Mode = "gre"
	}
	fields := strings.Fields(text)
	for i, field := range fields {
		switch {
		case field == "mtu" && i+1 < len(fields):
			observed.MTU, _ = strconv.Atoi(fields[i+1])
		case field == "grekey:" && i+1 < len(fields):
			observed.Key, _ = strconv.Atoi(fields[i+1])
		case field == "tunnel" && i+3 < len(fields) && fields[i+1] == "inet" && fields[i+3] == "-->":
			observed.Local = fields[i+2]
			if i+4 < len(fields) {
				observed.Remote = fields[i+4]
			}
		}
	}
	return observed
}

func parseFreeBSDIPv4AddressPrefixes(out []byte) []string {
	seen := map[string]bool{}
	var addresses []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "inet" {
			continue
		}
		addr, err := netip.ParseAddr(fields[1])
		if err != nil || !addr.Is4() {
			continue
		}
		bits := 32
		for j := 2; j+1 < len(fields); j++ {
			if fields[j] == "netmask" {
				bits = freeBSDNetmaskBits(fields[j+1])
				break
			}
		}
		prefix := netip.PrefixFrom(addr, bits).String()
		if !seen[prefix] {
			seen[prefix] = true
			addresses = append(addresses, prefix)
		}
	}
	return addresses
}

func freeBSDNetmaskBits(value string) int {
	value = strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if len(value) != 8 {
		return 32
	}
	n, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return 32
	}
	bits := 0
	for n&0x80000000 != 0 {
		bits++
		n <<= 1
	}
	if bits == 0 && n != 0 {
		return 32
	}
	return bits
}

func (c TunnelInterfaceController) addTunnelInterface(ctx context.Context, desired tunnelDesired) error {
	if c.targetOS() == platform.OSFreeBSD {
		if _, err := c.run(ctx, "ifconfig", desired.Name, "create"); err != nil {
			return commandError("create FreeBSD tunnel interface "+desired.Name, err)
		}
		return c.changeTunnelInterface(ctx, desired)
	}
	_, err := c.run(ctx, "ip", tunnelAddArgs(desired)...)
	return commandError("add tunnel interface "+desired.Name, err)
}

func (c TunnelInterfaceController) changeTunnelInterface(ctx context.Context, desired tunnelDesired) error {
	if c.targetOS() == platform.OSFreeBSD {
		if _, err := c.run(ctx, "ifconfig", desired.Name, "tunnel", desired.Local, desired.Remote); err != nil {
			return commandError("configure FreeBSD tunnel endpoints "+desired.Name, err)
		}
		if desired.Mode == "gre" && desired.Key != 0 {
			if _, err := c.run(ctx, "ifconfig", desired.Name, "grekey", strconv.Itoa(desired.Key)); err != nil {
				return commandError("configure FreeBSD GRE key "+desired.Name, err)
			}
		}
		return nil
	}
	_, err := c.run(ctx, "ip", tunnelChangeArgs(desired)...)
	return commandError("change tunnel interface "+desired.Name, err)
}

func (c TunnelInterfaceController) deleteTunnelInterface(ctx context.Context, ifname string) error {
	if c.targetOS() == platform.OSFreeBSD {
		out, err := c.run(ctx, "ifconfig", ifname, "destroy")
		if err == nil || tunnelMissingLink(out, err) {
			return nil
		}
		return fmt.Errorf("delete FreeBSD tunnel interface %s: %w: %s", ifname, err, strings.TrimSpace(string(out)))
	}
	out, err := c.run(ctx, "ip", "link", "del", "dev", ifname)
	if err == nil || tunnelMissingLink(out, err) {
		return nil
	}
	return fmt.Errorf("delete tunnel interface %s: %w: %s", ifname, err, strings.TrimSpace(string(out)))
}

func (c TunnelInterfaceController) setTunnelLink(ctx context.Context, desired tunnelDesired) error {
	if c.targetOS() == platform.OSFreeBSD {
		args := []string{desired.Name}
		if desired.MTU > 0 {
			args = append(args, "mtu", strconv.Itoa(desired.MTU))
		}
		args = append(args, "up")
		_, err := c.run(ctx, "ifconfig", args...)
		return commandError("set FreeBSD tunnel interface "+desired.Name, err)
	}
	args := []string{"link", "set", "dev", desired.Name}
	if desired.MTU > 0 {
		args = append(args, "mtu", strconv.Itoa(desired.MTU))
	}
	args = append(args, "up")
	_, err := c.run(ctx, "ip", args...)
	return commandError("set tunnel interface "+desired.Name, err)
}

func (c TunnelInterfaceController) setTunnelLinkUp(ctx context.Context, ifname string) error {
	if c.targetOS() == platform.OSFreeBSD {
		_, err := c.run(ctx, "ifconfig", ifname, "up")
		return commandError("bring FreeBSD tunnel interface "+ifname+" up", err)
	}
	_, err := c.run(ctx, "ip", "link", "set", "dev", ifname, "up")
	return commandError("bring tunnel interface "+ifname+" up", err)
}

func (c TunnelInterfaceController) reconcileTunnelAddress(ctx context.Context, desired tunnelDesired) (bool, error) {
	current, err := c.tunnelIPv4Addresses(ctx, desired.Name)
	if err != nil {
		return false, err
	}
	changed := false
	hasDesired := false
	for _, address := range current {
		if address == desired.Address {
			hasDesired = true
			continue
		}
		if err := c.deleteTunnelAddress(ctx, desired.Name, address); err != nil {
			return changed, err
		}
		changed = true
	}
	if hasDesired || desired.Address == "" {
		return changed, nil
	}
	if err := c.setTunnelAddress(ctx, desired); err != nil {
		return changed, err
	}
	return true, nil
}

func (c TunnelInterfaceController) tunnelIPv4Addresses(ctx context.Context, ifname string) ([]string, error) {
	if c.targetOS() == platform.OSFreeBSD {
		out, err := c.run(ctx, "ifconfig", ifname)
		if err != nil {
			return nil, fmt.Errorf("list FreeBSD tunnel interface %s addresses: %w: %s", ifname, err, strings.TrimSpace(string(out)))
		}
		return parseFreeBSDIPv4AddressPrefixes(out), nil
	}
	out, err := c.run(ctx, "ip", "-o", "-4", "addr", "show", "dev", ifname)
	if err != nil {
		return nil, fmt.Errorf("list tunnel interface %s addresses: %w: %s", ifname, err, strings.TrimSpace(string(out)))
	}
	return parseIPv4AddressPrefixes(out), nil
}

func parseIPv4AddressPrefixes(out []byte) []string {
	seen := map[string]bool{}
	var addresses []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if field != "inet" || i+1 >= len(fields) {
				continue
			}
			prefix := strings.TrimSpace(fields[i+1])
			parsed, err := netip.ParsePrefix(prefix)
			if err != nil || !parsed.Addr().Is4() || seen[prefix] {
				continue
			}
			seen[prefix] = true
			addresses = append(addresses, prefix)
		}
	}
	return addresses
}

func (c TunnelInterfaceController) deleteTunnelAddress(ctx context.Context, ifname, address string) error {
	if c.targetOS() == platform.OSFreeBSD {
		_, err := c.run(ctx, "ifconfig", ifname, "inet", address, "-alias")
		return commandError("delete FreeBSD tunnel interface "+ifname+" address "+address, err)
	}
	_, err := c.run(ctx, "ip", "addr", "del", address, "dev", ifname)
	return commandError("delete tunnel interface "+ifname+" address "+address, err)
}

func (c TunnelInterfaceController) setTunnelAddress(ctx context.Context, desired tunnelDesired) error {
	if c.targetOS() == platform.OSFreeBSD {
		_, err := c.run(ctx, "ifconfig", desired.Name, "inet", desired.Address)
		return commandError("set FreeBSD tunnel interface "+desired.Name+" address", err)
	}
	_, err := c.run(ctx, "ip", "addr", "replace", desired.Address, "dev", desired.Name)
	return commandError("set tunnel interface "+desired.Name+" address", err)
}

type fouListener struct {
	Mode string
	Port int
}

func fouListenerForDesired(desired tunnelDesired) (fouListener, bool) {
	if desired.Mode != "fou" && desired.Mode != "gue" {
		return fouListener{}, false
	}
	return fouListener{Mode: desired.Mode, Port: desired.EncapSport}, true
}

func fouListenerFromStatus(status map[string]any) (fouListener, bool) {
	owned, ok := statusBool(status["fouListenerOwned"])
	if !ok || !owned {
		return fouListener{}, false
	}
	mode := statusString(status, "mode")
	port, ok := statusInt(status["encapSport"])
	if (mode != "fou" && mode != "gue") || !ok || port < 1 || port > 65535 {
		return fouListener{}, false
	}
	return fouListener{Mode: mode, Port: port}, true
}

func fouListenerMatchesStatus(listener fouListener, status map[string]any) bool {
	previous, ok := fouListenerFromStatus(status)
	return ok && previous == listener
}

func (c TunnelInterfaceController) desiredUsesFOUListener(listener fouListener, exceptName string) bool {
	if c.Router == nil {
		return false
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "TunnelInterface" || resource.Metadata.Name == exceptName {
			continue
		}
		spec, err := resource.TunnelInterfaceSpec()
		if err != nil {
			continue
		}
		if candidate, ok := fouListenerForDesired(tunnelDesiredFromSpec(*c.Router, resource.Metadata.Name, spec)); ok && candidate == listener {
			return true
		}
	}
	return false
}

func (c TunnelInterfaceController) routerdOwnsFOUListener(listener fouListener, exceptName string) bool {
	lister, ok := c.Store.(routerstate.ObjectStatusLister)
	if !ok {
		return false
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return false
	}
	for _, item := range statuses {
		if item.APIVersion == api.HybridAPIVersion && item.Kind == "TunnelInterface" && item.Name != exceptName && routerdManagedObjectStatus(item) {
			if owned, ok := fouListenerFromStatus(item.Status); ok && owned == listener {
				return true
			}
		}
	}
	return false
}

func (c TunnelInterfaceController) transferFOUOwnership(listener fouListener, exceptName string) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "TunnelInterface" || resource.Metadata.Name == exceptName {
			continue
		}
		spec, err := resource.TunnelInterfaceSpec()
		if err != nil {
			continue
		}
		candidate, ok := fouListenerForDesired(tunnelDesiredFromSpec(*c.Router, resource.Metadata.Name, spec))
		if !ok || candidate != listener {
			continue
		}
		status := c.Store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", resource.Metadata.Name)
		if status == nil {
			status = map[string]any{}
		}
		status["fouListenerOwned"] = true
		status["encapSport"] = listener.Port
		status["mode"] = listener.Mode
		return c.Store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", resource.Metadata.Name, status)
	}
	return fmt.Errorf("no desired TunnelInterface can assume routerd-owned %s listener port %d", listener.Mode, listener.Port)
}

func (c TunnelInterfaceController) ensureFOUListener(ctx context.Context, desired tunnelDesired) (bool, error) {
	if c.targetOS() == platform.OSFreeBSD && (desired.Mode == "fou" || desired.Mode == "gue") {
		return false, fmt.Errorf("FreeBSD TunnelInterface mode %s has no equivalent FOU/GUE listener", desired.Mode)
	}
	if desired.Mode != "fou" && desired.Mode != "gue" {
		return false, nil
	}
	args := []string{"fou", "add", "port", strconv.Itoa(desired.EncapSport)}
	if desired.Mode == "gue" {
		args = append(args, "gue")
	} else {
		args = append(args, "ipproto", "4")
	}
	out, err := c.run(ctx, "ip", args...)
	if err == nil {
		return true, nil
	}
	if tunnelFOUAlreadyExists(out, err) {
		if err := c.verifyFOUListener(ctx, fouListener{Mode: desired.Mode, Port: desired.EncapSport}); err != nil {
			return false, err
		}
		return false, nil
	}
	return false, fmt.Errorf("ensure %s listener port %d: %w: %s", desired.Mode, desired.EncapSport, err, strings.TrimSpace(string(out)))
}

func (c TunnelInterfaceController) verifyFOUListener(ctx context.Context, listener fouListener) error {
	out, err := c.run(ctx, "ip", "fou", "show")
	if err != nil {
		return fmt.Errorf("inspect existing %s listener port %d: %w: %s", listener.Mode, listener.Port, err, strings.TrimSpace(string(out)))
	}
	needle := "port " + strconv.Itoa(listener.Port)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, needle) {
			continue
		}
		if listener.Mode == "gue" && strings.Contains(line, "gue") {
			return nil
		}
		if listener.Mode == "fou" && strings.Contains(line, "ipproto 4") && !strings.Contains(line, "gue") {
			return nil
		}
	}
	return fmt.Errorf("existing FOU listener port %d does not match routerd %s listener shape", listener.Port, listener.Mode)
}

func (c TunnelInterfaceController) deleteFOUListener(ctx context.Context, listener fouListener) error {
	out, err := c.run(ctx, "ip", "fou", "del", "port", strconv.Itoa(listener.Port))
	if err == nil || tunnelFOUMissing(out, err) {
		return nil
	}
	return fmt.Errorf("delete %s listener port %d: %w: %s", listener.Mode, listener.Port, err, strings.TrimSpace(string(out)))
}

func tunnelAddArgs(desired tunnelDesired) []string {
	linkType := desired.Mode
	if desired.Mode == "fou" || desired.Mode == "gue" {
		linkType = "ipip"
	}
	args := []string{"link", "add", "dev", desired.Name, "type", linkType, "local", desired.Local, "remote", desired.Remote, "ttl", strconv.Itoa(desired.TTL)}
	if desired.Mode == "gre" && desired.Key != 0 {
		args = append(args, "key", strconv.Itoa(desired.Key))
	}
	if desired.Mode == "fou" || desired.Mode == "gue" {
		args = append(args, "encap", desired.Mode, "encap-sport", strconv.Itoa(desired.EncapSport), "encap-dport", strconv.Itoa(desired.EncapDport))
	}
	return args
}

func tunnelChangeArgs(desired tunnelDesired) []string {
	linkMode := desired.Mode
	if desired.Mode == "fou" || desired.Mode == "gue" {
		linkMode = "ipip"
	}
	args := []string{"link", "set", "dev", desired.Name, "type", linkMode, "local", desired.Local, "remote", desired.Remote, "ttl", strconv.Itoa(desired.TTL)}
	if desired.Mode == "gre" {
		if desired.Key == 0 {
			args = append(args, "nokey")
		} else {
			args = append(args, "key", strconv.Itoa(desired.Key))
		}
	}
	if desired.Mode == "fou" || desired.Mode == "gue" {
		args = append(args, "encap", desired.Mode, "encap-sport", strconv.Itoa(desired.EncapSport), "encap-dport", strconv.Itoa(desired.EncapDport))
	}
	return args
}

func (c TunnelInterfaceController) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	run := c.Command
	if run == nil {
		run = defaultTunnelCommandRunner
	}
	return run(ctx, name, args...)
}

func (c TunnelInterfaceController) targetOS() platform.OS {
	if c.OS != "" {
		return c.OS
	}
	return platform.CurrentOS()
}

func defaultTunnelCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

func commandError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", action, err)
}

func tunnelMissingLink(out []byte, err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(string(out)) + " " + err.Error())
	for _, needle := range []string{"cannot find device", "does not exist", "not found", "no such device"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func tunnelFOUAlreadyExists(out []byte, err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(string(out)) + " " + err.Error())
	return strings.Contains(msg, "file exists") || strings.Contains(msg, "object already exists") || strings.Contains(msg, "already exists") || strings.Contains(msg, "address already in use")
}

func tunnelFOUMissing(out []byte, err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "no such") || strings.Contains(msg, "does not exist")
}
