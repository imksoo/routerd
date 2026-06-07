// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/lifecycle"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"github.com/imksoo/routerd/pkg/wireguard"
)

type WireGuardController struct {
	Router       *api.Router
	Bus          *bus.Bus
	Store        Store
	DryRun       bool
	Command      wireguard.CommandRunner
	CommandStdin wireguard.CommandStdinRunner
	LookupHost   func(context.Context, string) ([]string, error)
	Logger       *slog.Logger
}

func (c WireGuardController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	if err := c.cleanupStaleResources(ctx); err != nil {
		return err
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "WireGuardInterface" {
			continue
		}
		if err := c.reconcileInterface(ctx, resource); err != nil {
			return err
		}
	}
	return nil
}

func (c WireGuardController) cleanupStaleResources(ctx context.Context) error {
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
	desiredInterfaces := map[string]struct{}{}
	desiredPeers := map[string]struct{}{}
	desired := map[string]bool{}
	for _, resource := range c.Router.Spec.Resources {
		switch resource.Kind {
		case "WireGuardInterface":
			desiredInterfaces[resource.Metadata.Name] = struct{}{}
			desired[lifecycle.OwnerKey(api.NetAPIVersion, "WireGuardInterface", resource.Metadata.Name)] = true
			if ifname := interfaceIfName(c.Router, resource.Metadata.Name); ifname != "" {
				desiredInterfaces[ifname] = struct{}{}
				desired[lifecycle.OwnerKey(api.NetAPIVersion, "WireGuardInterface", ifname)] = true
			}
		case "WireGuardPeer":
			desiredPeers[resource.Metadata.Name] = struct{}{}
			desired[lifecycle.OwnerKey(api.NetAPIVersion, "WireGuardPeer", resource.Metadata.Name)] = true
		}
	}
	staleInterfaces := map[string]struct{}{}
	plan := lifecycle.PlanResourceTeardownGC(desired, statuses)
	for _, action := range plan.Actions {
		if action.Type != lifecycle.GCActionTeardownResource {
			continue
		}
		item := action.Status
		if item.APIVersion != api.NetAPIVersion || item.Kind != "WireGuardInterface" || !routerdManagedObjectStatus(item) {
			continue
		}
		ifname := firstNonEmpty(statusString(item.Status, "ifname"), statusString(item.Status, "interface"), item.Name)
		if err := c.teardownWireGuardInterface(ctx, item, ifname, deleter); err != nil {
			return err
		}
		staleInterfaces[item.Name] = struct{}{}
		staleInterfaces[ifname] = struct{}{}
	}
	for _, action := range plan.Actions {
		if action.Type != lifecycle.GCActionTeardownResource {
			continue
		}
		item := action.Status
		if item.APIVersion != api.NetAPIVersion || item.Kind != "WireGuardPeer" || !routerdManagedObjectStatus(item) {
			continue
		}
		if err := c.teardownWireGuardPeer(ctx, item, deleter); err != nil {
			return err
		}
	}
	for _, item := range statuses {
		if item.APIVersion != api.NetAPIVersion || item.Kind != "WireGuardPeer" || !routerdManagedObjectStatus(item) {
			continue
		}
		if _, peerStillDesired := desiredPeers[item.Name]; !peerStillDesired {
			continue
		}
		if _, interfaceRemoved := staleInterfaces[statusString(item.Status, "interface")]; !interfaceRemoved {
			continue
		}
		if err := c.teardownWireGuardPeer(ctx, item, deleter); err != nil {
			return err
		}
	}
	return nil
}

func (c WireGuardController) teardownWireGuardInterface(ctx context.Context, item routerstate.ObjectStatus, ifname string, deleter routerstate.ObjectDeleteStore) error {
	if ifname != "" && !c.DryRun {
		if err := c.deleteWireGuardInterface(ctx, ifname); err != nil {
			return err
		}
	}
	if err := deleter.DeleteObject(item.APIVersion, item.Kind, item.Name); err != nil {
		return err
	}
	return c.publishWireGuardRemoved(ctx, item, map[string]string{"interface": ifname})
}

func (c WireGuardController) teardownWireGuardPeer(ctx context.Context, item routerstate.ObjectStatus, deleter routerstate.ObjectDeleteStore) error {
	if err := deleter.DeleteObject(item.APIVersion, item.Kind, item.Name); err != nil {
		return err
	}
	return c.publishWireGuardRemoved(ctx, item, map[string]string{"interface": statusString(item.Status, "interface")})
}

func (c WireGuardController) publishWireGuardRemoved(ctx context.Context, item routerstate.ObjectStatus, attrs map[string]string) error {
	if c.Bus == nil {
		return nil
	}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.wireguard.resource.removed", daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: item.APIVersion, Kind: item.Kind, Name: item.Name}
	event.Attributes = attrs
	return c.Bus.Publish(ctx, event)
}

func (c WireGuardController) deleteWireGuardInterface(ctx context.Context, ifname string) error {
	run := c.Command
	if run == nil {
		run = wireguard.DefaultCommandRunner
	}
	out, err := run(ctx, "ip", "link", "delete", "dev", ifname)
	if err == nil || wireGuardDeleteMissingLink(out, err) {
		return nil
	}
	return fmt.Errorf("delete stale WireGuard interface %s: %w: %s", ifname, err, strings.TrimSpace(string(out)))
}

func wireGuardDeleteMissingLink(out []byte, err error) bool {
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

func routerdManagedObjectStatus(item routerstate.ObjectStatus) bool {
	if managed, ok := statusBool(item.Status["managed"]); ok && !managed {
		return false
	}
	managedBy := firstNonEmpty(item.ManagedBy, statusString(item.Status, "managedBy"))
	if strings.EqualFold(managedBy, "external") {
		return false
	}
	management := firstNonEmpty(item.Management, statusString(item.Status, "management"))
	if strings.EqualFold(management, "adopted") {
		return false
	}
	if managedBy == "" && management == "" && resourceOwnerController(item.Kind) == "" {
		return false
	}
	return true
}

func statusString(status map[string]any, key string) string {
	if status == nil {
		return ""
	}
	value, ok := status[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func (c WireGuardController) reconcileInterface(ctx context.Context, resource api.Resource) error {
	cfg, err := wireguard.BuildInterface(resource, c.Router.Spec.Resources)
	if err != nil {
		return err
	}
	if err := c.saveUnconfiguredPeerStatuses(resource.Metadata.Name); err != nil {
		return err
	}
	status := map[string]any{
		"phase":      "Pending",
		"interface":  resource.Metadata.Name,
		"ifname":     cfg.Name,
		"listenPort": cfg.ListenPort,
		"mtu":        cfg.MTU,
		"fwmark":     cfg.FwMark,
		"peerCount":  len(cfg.Peers),
		"dryRun":     c.DryRun,
	}
	configHash := wireGuardConfigHash(cfg, c.DryRun)
	if configHash != "" {
		status["configHash"] = configHash
	}
	if cfg.PrivateKey == "" && cfg.PrivateKeyFile == "" {
		status["reason"] = "PrivateKeyMissing"
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardInterface", resource.Metadata.Name, status); err != nil {
			return err
		}
		c.savePeerPendingStatuses(resource.Metadata.Name, cfg.Peers, "InterfacePending")
		return nil
	}
	controller := wireguard.Controller{Command: c.Command, CommandStdin: c.CommandStdin, DryRun: c.DryRun}
	observed, statusErr := c.interfaceStatus(ctx, cfg.Name)
	applied := false
	current := c.Store.ObjectStatus(api.NetAPIVersion, "WireGuardInterface", resource.Metadata.Name)
	if !c.DryRun && statusErr == nil && configHash != "" && fmt.Sprint(current["configHash"]) == configHash && c.interfaceMatchesDesired(ctx, cfg, observed) {
		status["reason"] = "AlreadyConfigured"
	} else if _, err := controller.Apply(ctx, cfg); err != nil {
		status["phase"] = "Error"
		status["reason"] = "ApplyFailed"
		status["error"] = err.Error()
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardInterface", resource.Metadata.Name, status); err != nil {
			return err
		}
		c.savePeerPendingStatuses(resource.Metadata.Name, cfg.Peers, "InterfaceError")
		return nil
	} else {
		applied = true
		observed, statusErr = c.interfaceStatus(ctx, cfg.Name)
	}
	status["phase"] = "Up"
	if c.DryRun {
		status["phase"] = "Planned"
	}
	if statusErr == nil {
		if observed.PublicKey != "" {
			status["publicKey"] = observed.PublicKey
		}
		if observed.ListenPort != 0 {
			status["listenPort"] = observed.ListenPort
		}
		if observed.FwMark != "" {
			status["fwmark"] = observed.FwMark
		}
		status["peerCount"] = len(observed.Peers)
		c.savePeerObservedStatuses(resource.Metadata.Name, cfg.Peers, observed.Peers)
	} else if !c.DryRun {
		status["statusError"] = statusErr.Error()
		c.savePeerPendingStatuses(resource.Metadata.Name, cfg.Peers, "StatusUnavailable")
	} else {
		c.savePeerPendingStatuses(resource.Metadata.Name, cfg.Peers, "DryRun")
	}
	if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardInterface", resource.Metadata.Name, status); err != nil {
		return err
	}
	if applied && c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.wireguard.interface.applied", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{
			"interface": cfg.Name,
			"peers":     fmt.Sprintf("%d", len(cfg.Peers)),
			"dryRun":    fmt.Sprintf("%t", c.DryRun),
		}
		return c.Bus.Publish(ctx, event)
	}
	return nil
}

func (c WireGuardController) saveUnconfiguredPeerStatuses(iface string) error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "WireGuardPeer" {
			continue
		}
		spec, err := resource.WireGuardPeerSpec()
		if err != nil {
			return err
		}
		if spec.Interface != iface || wireguard.PeerSpecConfigured(spec) {
			continue
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardPeer", resource.Metadata.Name, map[string]any{
			"phase":     "NotConfigured",
			"reason":    "PeerSpecEmpty",
			"interface": iface,
			"ifname":    interfaceIfName(c.Router, iface),
			"dryRun":    c.DryRun,
		}); err != nil {
			return err
		}
	}
	return nil
}

func wireGuardConfigHash(cfg wireguard.InterfaceConfig, dryRun bool) string {
	resolved, err := wireguard.ResolveKeyFiles(cfg)
	if err != nil {
		if !dryRun {
			return ""
		}
		resolved = cfg
		if resolved.PrivateKey == "" && resolved.PrivateKeyFile != "" {
			resolved.PrivateKey = "REDACTED_FROM_FILE"
		}
		for i := range resolved.Peers {
			if resolved.Peers[i].PresharedKey == "" && resolved.Peers[i].PresharedKeyFile != "" {
				resolved.Peers[i].PresharedKey = "REDACTED_FROM_FILE"
			}
		}
	}
	conf, err := wireguard.RenderSetConf(resolved)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(conf)
	return hex.EncodeToString(sum[:])
}

func (c WireGuardController) interfaceMatchesDesired(ctx context.Context, cfg wireguard.InterfaceConfig, observed wireguard.InterfaceStatus) bool {
	if cfg.ListenPort != 0 && observed.ListenPort != cfg.ListenPort {
		return false
	}
	if cfg.FwMark != 0 && !fwmarkMatches(observed.FwMark, cfg.FwMark) {
		return false
	}
	if cfg.MTU != 0 && !c.linkMTUMatches(ctx, cfg.Name, cfg.MTU) {
		return false
	}
	byKey := map[string]wireguard.PeerStatus{}
	for _, peer := range observed.Peers {
		byKey[peer.PublicKey] = peer
	}
	if len(byKey) != len(cfg.Peers) {
		return false
	}
	for _, desired := range cfg.Peers {
		current, ok := byKey[desired.PublicKey]
		if !ok {
			return false
		}
		if !stringSetEqual(desired.AllowedIPs, current.AllowedIPs) {
			return false
		}
		if strings.TrimSpace(desired.Endpoint) != "" && !c.endpointMatches(ctx, desired.Endpoint, current.LatestEndpoint) {
			return false
		}
		if desired.PersistentKeepalive != current.PersistentKeepalive {
			return false
		}
	}
	return true
}

func (c WireGuardController) endpointMatches(ctx context.Context, desired, current string) bool {
	desired = strings.TrimSpace(desired)
	current = strings.TrimSpace(current)
	if desired == current {
		return true
	}
	if current == "" {
		return true
	}
	desiredHost, desiredPort, err := net.SplitHostPort(desired)
	if err != nil {
		return false
	}
	currentHost, currentPort, err := net.SplitHostPort(current)
	if err != nil || desiredPort != currentPort {
		return false
	}
	if desiredAddr, err := netip.ParseAddr(desiredHost); err == nil {
		currentAddr, err := netip.ParseAddr(currentHost)
		return err == nil && desiredAddr == currentAddr
	}
	lookup := c.LookupHost
	if lookup == nil {
		lookup = net.DefaultResolver.LookupHost
	}
	addrs, err := lookup(ctx, desiredHost)
	if err != nil {
		return false
	}
	currentAddr, err := netip.ParseAddr(currentHost)
	if err != nil {
		return false
	}
	for _, raw := range addrs {
		addr, err := netip.ParseAddr(strings.TrimSpace(raw))
		if err == nil && addr == currentAddr {
			return true
		}
	}
	return false
}

func (c WireGuardController) linkMTUMatches(ctx context.Context, ifname string, mtu int) bool {
	run := c.Command
	if run == nil {
		run = wireguard.DefaultCommandRunner
	}
	out, err := run(ctx, "ip", "-o", "link", "show", "dev", ifname)
	if err != nil {
		return false
	}
	fields := strings.Fields(string(out))
	for i, field := range fields {
		if field == "mtu" && i+1 < len(fields) {
			got, _ := strconv.Atoi(fields[i+1])
			return got == mtu
		}
	}
	return false
}

func fwmarkMatches(current string, desired int) bool {
	current = strings.TrimSpace(strings.ToLower(current))
	if current == "" {
		return desired == 0
	}
	return current == fmt.Sprintf("0x%x", desired) || current == fmt.Sprintf("%d", desired)
}

func stringSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	left := append([]string(nil), a...)
	right := append([]string(nil), b...)
	sort.Strings(left)
	sort.Strings(right)
	for i := range left {
		if strings.TrimSpace(left[i]) != strings.TrimSpace(right[i]) {
			return false
		}
	}
	return true
}

func (c WireGuardController) interfaceStatus(ctx context.Context, ifname string) (wireguard.InterfaceStatus, error) {
	run := c.Command
	if run == nil {
		run = wireguard.DefaultCommandRunner
	}
	out, err := run(ctx, "wg", "show", ifname, "dump")
	if err != nil {
		return wireguard.InterfaceStatus{Name: ifname}, err
	}
	return wireguard.ParseInterfaceDump(ifname, out)
}

func (c WireGuardController) savePeerPendingStatuses(iface string, peers []wireguard.PeerConfig, reason string) {
	ifname := interfaceIfName(c.Router, iface)
	for _, peer := range peers {
		_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardPeer", peer.Name, map[string]any{
			"phase":      "Pending",
			"reason":     reason,
			"interface":  iface,
			"ifname":     ifname,
			"publicKey":  peer.PublicKey,
			"allowedIPs": append([]string(nil), peer.AllowedIPs...),
			"endpoint":   peer.Endpoint,
			"dryRun":     c.DryRun,
		})
	}
}

func (c WireGuardController) savePeerObservedStatuses(iface string, desired []wireguard.PeerConfig, observed []wireguard.PeerStatus) {
	ifname := interfaceIfName(c.Router, iface)
	byKey := map[string]wireguard.PeerStatus{}
	for _, peer := range observed {
		byKey[peer.PublicKey] = peer
	}
	for _, peer := range desired {
		status := map[string]any{
			"phase":               "Configured",
			"interface":           iface,
			"ifname":              ifname,
			"publicKey":           peer.PublicKey,
			"allowedIPs":          append([]string(nil), peer.AllowedIPs...),
			"endpoint":            peer.Endpoint,
			"persistentKeepalive": peer.PersistentKeepalive,
			"dryRun":              c.DryRun,
		}
		if got, ok := byKey[peer.PublicKey]; ok {
			if got.LatestEndpoint != "" {
				status["latestEndpoint"] = got.LatestEndpoint
			}
			if !got.LatestHandshake.IsZero() {
				status["phase"] = "Connected"
				status["latestHandshake"] = got.LatestHandshake.Format(time.RFC3339)
				status["handshakeAgeSeconds"] = int(time.Since(got.LatestHandshake).Seconds())
			} else if strings.TrimSpace(peer.Endpoint) != "" {
				status["phase"] = "Waiting"
				status["reason"] = "NoHandshakeYet"
			}
			status["transferRxBytes"] = got.TransferRxBytes
			status["transferTxBytes"] = got.TransferTxBytes
		} else {
			status["phase"] = "Pending"
			status["reason"] = "PeerNotObserved"
		}
		_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardPeer", peer.Name, status)
	}
}
