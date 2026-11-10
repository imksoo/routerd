// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/hostdeps"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/sam"
)

// SAMController applies only the typed local capture intents emitted by a
// MobilityPool plan. It deliberately does not inspect status or legacy SAM
// resources to rediscover desired state.
type SAMController struct {
	Router              *api.Router
	LocalCaptureIntents []dynamicconfig.LocalCaptureIntent
	DryRun              bool
}

func (c SAMController) Reconcile(ctx context.Context) error {
	if c.Router == nil {
		return nil
	}
	targetOS := platform.CurrentOS()
	if targetOS != platform.OSLinux && targetOS != platform.OSFreeBSD {
		return nil
	}
	actions, err := sam.PlanLocalCaptureIntents(c.LocalCaptureIntents, targetOS)
	if err != nil {
		return err
	}
	state, err := c.reconcileState(ctx, actions)
	if err != nil {
		return err
	}
	deassigned := map[string]bool{}
	var failures []string
	applier := defaultSAMProxyNeighborApplier()
	for _, action := range actions {
		if action.Kind != "deassign-os-address" || c.DryRun {
			continue
		}
		result, err := applier.EnsureOSAddressAbsent(ctx, action.Address)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s deassign %s: %v", action.IntentID, action.Address, err))
			continue
		}
		deassigned[action.IntentID] = result
	}
	if len(failures) != 0 {
		return fmt.Errorf("SAM capture failed: %s", strings.Join(failures, "; "))
	}
	if err := c.reconcileProxyARPSysctls(ctx, actions, state); err != nil {
		return err
	}
	if err := c.reconcileSAMIPForwarding(ctx, state); err != nil {
		return err
	}
	if err := c.reconcileForwardPaths(ctx, state); err != nil {
		return err
	}
	if err := c.cleanupReleasedProxyNeighbors(ctx, actions, state); err != nil {
		return err
	}
	for _, action := range actions {
		if action.Kind != "proxy-neighbor" || c.DryRun {
			continue
		}
		if err := applier.EnsureProxyNeighbor(ctx, action.Address, action.Interface); err != nil {
			failures = append(failures, fmt.Sprintf("%s %s dev %s: %v", action.IntentID, action.Address, action.Interface, err))
			continue
		}
		if action.GratuitousARP && deassigned[action.IntentID] {
			announcer := defaultSAMGratuitousARPAnnouncer()
			if err := announcer.SendGratuitousARP(ctx, action.Address, action.Interface); err != nil {
				failures = append(failures, fmt.Sprintf("%s gratuitous ARP %s dev %s: %v", action.IntentID, action.Address, action.Interface, err))
			}
		}
	}
	if len(failures) != 0 {
		return fmt.Errorf("SAM capture failed: %s", strings.Join(failures, "; "))
	}
	return nil
}

type samObservedProxy struct {
	Address   string
	Interface string
}

type samReconcileState struct {
	proxyNeighbors   []samObservedProxy
	heldProxyARP     map[string]bool
	heldForwardPaths []sam.CaptureAction
	forwardPaths     []sam.CaptureAction
	hasHold          bool
}

func (c SAMController) reconcileState(ctx context.Context, actions []sam.CaptureAction) (samReconcileState, error) {
	state := samReconcileState{heldProxyARP: map[string]bool{}}
	var err error
	if !c.DryRun {
		state.proxyNeighbors, err = defaultSAMProxyNeighborApplier().ListProxyNeighbors(ctx)
		if err != nil {
			return samReconcileState{}, err
		}
	}
	for _, action := range actions {
		if action.Kind == "forward-path" || action.Kind == "forward-local-path" {
			state.forwardPaths = append(state.forwardPaths, action)
		}
	}
	for _, intent := range c.LocalCaptureIntents {
		if intent.Disposition != dynamicconfig.CaptureHold {
			continue
		}
		state.hasHold = true
		if intent.CaptureType == "proxy-arp" {
			for _, neighbor := range state.proxyNeighbors {
				if neighbor.Address == intent.Address && neighbor.Interface == intent.CaptureInterface {
					state.heldProxyARP[intent.CaptureInterface] = true
				}
			}
		}
		kind := "forward-local-path"
		if intent.CaptureType == "provider-secondary-ip" {
			kind = "forward-path"
		}
		for _, tunnel := range intent.TunnelInterfaces {
			state.heldForwardPaths = append(state.heldForwardPaths, sam.CaptureAction{Kind: kind, Address: intent.Address, Interface: intent.CaptureInterface, PeerInterface: tunnel})
		}
	}
	return state, nil
}

// cleanupReleasedProxyNeighbors removes proxy entries not required by capture intents.
func (c SAMController) cleanupReleasedProxyNeighbors(ctx context.Context, actions []sam.CaptureAction, state samReconcileState) error {
	if c.DryRun {
		return nil
	}
	desired := map[string]bool{}
	key := func(address, ifname string) string {
		return strings.TrimSpace(ifname) + "\x00" + strings.TrimSpace(address)
	}
	for _, action := range actions {
		if action.Kind == "proxy-neighbor" {
			desired[key(action.Address, action.Interface)] = true
		}
	}
	for _, intent := range c.LocalCaptureIntents {
		if intent.Disposition == dynamicconfig.CaptureHold && (intent.CaptureType == "proxy-arp" || intent.CaptureType == "provider-secondary-ip") {
			desired[key(intent.Address, intent.CaptureInterface)] = true
		}
	}
	applier := defaultSAMProxyNeighborApplier()
	for _, applied := range state.proxyNeighbors {
		if desired[key(applied.Address, applied.Interface)] {
			continue
		}
		if err := applier.DeleteProxyNeighbor(ctx, applied.Address, applied.Interface); err != nil {
			return fmt.Errorf("remove stale SAM proxy neighbor %s dev %s: %w", applied.Address, applied.Interface, err)
		}
	}
	return nil
}

func (c SAMController) reconcileSAMIPForwarding(ctx context.Context, state samReconcileState) error {
	if c.DryRun || len(state.forwardPaths) == 0 {
		return nil
	}
	return defaultSAMProxyNeighborApplier().SetIPForwarding(ctx, true)
}

func (c SAMController) reconcileForwardPaths(ctx context.Context, state samReconcileState) error {
	if c.DryRun {
		return nil
	}
	applier := defaultSAMProxyNeighborApplier()
	return applier.ReconcileForwardPaths(ctx, state.forwardPaths, state.heldForwardPaths)
}

func (c SAMController) reconcileProxyARPSysctls(ctx context.Context, actions []sam.CaptureAction, state samReconcileState) error {
	if c.DryRun {
		return nil
	}
	targetOS := platform.CurrentOS()
	applier := defaultSAMProxyNeighborApplier()
	desired := map[string]bool{}
	explicitProxyAll := false
	if targetOS == platform.OSFreeBSD {
		if freeBSDProxyARPEnabled(actions) {
			desired[""] = true
		}
	} else {
		for _, action := range actions {
			if action.Kind == "sysctl" && strings.HasSuffix(action.Key, ".proxy_arp") && action.Value == "1" && action.Interface != "" {
				desired[action.Interface] = true
			}
		}
		for iface := range state.heldProxyARP {
			desired[iface] = true
		}
	}
	for _, resource := range hostdeps.SysctlResources(c.Router) {
		var settings []sysctlSetting
		switch resource.Kind {
		case "Sysctl":
			spec, err := resource.SysctlSpec()
			if err != nil {
				return err
			}
			settings = []sysctlSetting{{Key: spec.Key, Value: spec.Value}}
		case "SysctlProfile":
			spec, err := resource.SysctlProfileSpec()
			if err != nil {
				return err
			}
			settings, err = sysctlProfileSettings(spec)
			if err != nil {
				return err
			}
		}
		for _, setting := range settings {
			key := strings.TrimSpace(setting.Key)
			if targetOS == platform.OSFreeBSD && strings.ReplaceAll(key, "/", ".") == "net.link.ether.inet.proxyall" {
				explicitProxyAll = true
				desired[""] = strings.TrimSpace(setting.Value) == "1"
			}
			iface := ""
			if strings.HasPrefix(key, "net.ipv4.conf.") && strings.HasSuffix(key, ".proxy_arp") {
				iface = strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(key, "net.ipv4.conf."), ".proxy_arp"), "/", ".")
			} else if strings.HasPrefix(key, "net/ipv4/conf/") && strings.HasSuffix(key, "/proxy_arp") {
				iface = strings.TrimSuffix(strings.TrimPrefix(key, "net/ipv4/conf/"), "/proxy_arp")
			}
			if strings.TrimSpace(setting.Value) == "1" && iface != "" {
				desired[iface] = true
			}
		}
	}
	interfaces := map[string]bool{}
	if targetOS == platform.OSFreeBSD {
		if !state.hasHold || desired[""] || explicitProxyAll {
			interfaces[""] = true
		}
	} else {
		for iface := range desired {
			if iface != "all" && iface != "default" {
				interfaces[iface] = true
			}
		}
		links, err := net.Interfaces()
		if err != nil {
			return err
		}
		for _, link := range links {
			interfaces[link.Name] = true
		}
	}
	ordered := make([]string, 0, len(interfaces))
	for iface := range interfaces {
		ordered = append(ordered, iface)
	}
	sort.Strings(ordered)
	for _, iface := range ordered {
		if err := applier.SetProxyARP(ctx, iface, desired[iface]); err != nil {
			return fmt.Errorf("set SAM proxy_arp %q=%t: %w", iface, desired[iface], err)
		}
	}
	return nil
}

func freeBSDProxyARPEnabled(actions []sam.CaptureAction) bool {
	for _, action := range actions {
		if action.Kind == "proxy-neighbor" {
			return true
		}
	}
	return false
}
