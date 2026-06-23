// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/lifecycle"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/sam"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type samProxyNeighborApplier interface {
	SetProxyARP(ctx context.Context, ifname string, enabled bool) error
	EnsureProxyNeighbor(ctx context.Context, address, ifname string) error
	DeleteProxyNeighbor(ctx context.Context, address, ifname string) error
	EnsureOSAddressAbsent(ctx context.Context, address string) (samOSAddressDeassignResult, error)
	ReconcileForwardPaths(ctx context.Context, paths []sam.CaptureAction) error
}

type samGratuitousARPAnnouncer interface {
	SendGratuitousARP(ctx context.Context, address, ifname string) error
}

type samStoredProxyNeighbor struct {
	address string
	ifname  string
}

type samOSAddressDeassignResult struct {
	address string
	ifname  string
	// removedThisReconcile is true only when this reconcile deleted the
	// captured address from a local OS interface.
	removedThisReconcile bool
}

func samSelectResources(resources []api.Resource, kind string) []api.Resource {
	var out []api.Resource
	for _, resource := range resources {
		if resource.Kind == kind {
			out = append(out, resource)
		}
	}
	return out
}

type SAMController struct {
	Router      *api.Router
	Bus         *bus.Bus
	Store       Store
	Lowerings   []sam.DeliveryLowering
	DryRun      bool
	OS          platform.OS
	Applier     samProxyNeighborApplier
	GARP        samGratuitousARPAnnouncer
	ListActions func(routerstate.ActionExecutionFilter) ([]routerstate.ActionExecutionRecord, error)
	// HandoffLeaseTTL overrides spec.reconcile.samHandoffLeaseTTL for tests.
	// The production default is 30s.
	HandoffLeaseTTL time.Duration
	Now             func() time.Time
}

const defaultSAMHandoffLeaseTTL = 30 * time.Second

func (c SAMController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	targetOS := c.OS
	if targetOS == "" {
		targetOS = platform.CurrentOS()
	}
	if targetOS != platform.OSLinux {
		return c.reconcileStatuses(targetOS, nil, nil, nil)
	}
	statuses, err := c.listObjectStatuses()
	if err != nil {
		return err
	}
	if err := c.cleanupRemovedCaptures(ctx, statuses); err != nil {
		return err
	}
	actions, err := sam.PlanCaptureWithOptions(c.Router, targetOS, sam.PlanOptions{StatusReader: c.Store})
	if err != nil {
		return err
	}
	if err := c.cleanupChangedCaptures(ctx, statuses, actions); err != nil {
		return err
	}
	if err := c.reconcileProxyARPSysctls(ctx, actions); err != nil {
		return err
	}
	if err := c.reconcileForwardPaths(ctx, actions); err != nil {
		return err
	}
	var failures []string
	deassignResults := map[string]samOSAddressDeassignResult{}
	garpSent := map[string]bool{}
	garpErrors := map[string]string{}
	priorNeighbors := samStoredProxyNeighbors(statuses)
	for _, action := range actions {
		switch action.Kind {
		case "proxy-neighbor":
			if c.DryRun {
				continue
			}
			applier := c.Applier
			if applier == nil {
				applier = defaultSAMProxyNeighborApplier()
			}
			if err := applier.EnsureProxyNeighbor(ctx, action.Address, action.Interface); err != nil {
				failures = append(failures, fmt.Sprintf("%s %s dev %s: %v", action.ClaimName, action.Address, action.Interface, err))
				continue
			}
			current := samStoredProxyNeighbor{address: strings.TrimSpace(action.Address), ifname: strings.TrimSpace(action.Interface)}
			if action.GratuitousARP && priorNeighbors[action.ClaimName] != current {
				announcer := c.GARP
				if announcer == nil {
					announcer = defaultSAMGratuitousARPAnnouncer()
				}
				if err := announcer.SendGratuitousARP(ctx, action.Address, action.Interface); err != nil {
					garpErrors[action.ClaimName] = fmt.Sprintf("gratuitous ARP %s dev %s: %v", action.Address, action.Interface, err)
				} else {
					garpSent[action.ClaimName] = true
				}
			}
		case "deassign-os-address":
			result := samOSAddressDeassignResult{address: strings.TrimSpace(action.Address)}
			deassignResults[action.ClaimName] = result
			if c.DryRun {
				continue
			}
			applier := c.Applier
			if applier == nil {
				applier = defaultSAMProxyNeighborApplier()
			}
			result, err := applier.EnsureOSAddressAbsent(ctx, action.Address)
			if result.address == "" {
				result.address = strings.TrimSpace(action.Address)
			}
			deassignResults[action.ClaimName] = result
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s deassign %s: %v", action.ClaimName, action.Address, err))
			}
		default:
			continue
		}
	}
	if err := c.reconcileStatuses(targetOS, deassignResults, garpSent, garpErrors); err != nil {
		return err
	}
	if len(failures) > 0 {
		return fmt.Errorf("SAM capture failed: %s", strings.Join(failures, "; "))
	}
	return nil
}

func (c SAMController) reconcileForwardPaths(ctx context.Context, actions []sam.CaptureAction) error {
	var paths []sam.CaptureAction
	for _, action := range actions {
		if action.Kind == "forward-path" || action.Kind == "forward-local-path" {
			paths = append(paths, action)
		}
	}
	if c.DryRun {
		return nil
	}
	applier := c.Applier
	if applier == nil {
		applier = defaultSAMProxyNeighborApplier()
	}
	return applier.ReconcileForwardPaths(ctx, paths)
}

func (c SAMController) reconcileProxyARPSysctls(ctx context.Context, actions []sam.CaptureAction) error {
	if c.DryRun {
		return nil
	}
	all := map[string]bool{}
	aliases := sam.CaptureInterfaceAliases(c.Router)
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "RemoteAddressClaim" {
			continue
		}
		spec, err := resource.RemoteAddressClaimSpec()
		if err != nil {
			continue
		}
		captureType := strings.TrimSpace(spec.Capture.Type)
		bgpDelivery := strings.TrimSpace(spec.Delivery.Mode) == "bgp"
		// provider-secondary+BGP uses explicit proxy-neighbor entries but must
		// keep interface-wide proxy_arp disabled. Include its interfaces here
		// so older routerd state is actively reset to proxy_arp=0.
		if captureType != "proxy-arp" && !(captureType == "provider-secondary-ip" && bgpDelivery) {
			continue
		}
		if iface := sam.ResolveCaptureInterface(strings.TrimSpace(spec.Capture.Interface), aliases); iface != "" {
			all[iface] = true
		}
	}
	if len(all) == 0 {
		return nil
	}
	active := map[string]bool{}
	for _, action := range actions {
		if action.Kind == "sysctl" && strings.HasSuffix(action.Key, ".proxy_arp") && action.Value == "1" && strings.TrimSpace(action.Interface) != "" {
			active[strings.TrimSpace(action.Interface)] = true
		}
	}
	applier := c.Applier
	if applier == nil {
		applier = defaultSAMProxyNeighborApplier()
	}
	for iface := range all {
		if err := applier.SetProxyARP(ctx, iface, active[iface]); err != nil {
			return fmt.Errorf("set SAM proxy_arp %s=%t: %w", iface, active[iface], err)
		}
	}
	return nil
}

func (c SAMController) reconcileStatuses(targetOS platform.OS, deassignResults map[string]samOSAddressDeassignResult, garpSent map[string]bool, garpErrors map[string]string) error {
	claims := samSelectResources(c.Router.Spec.Resources, "RemoteAddressClaim")
	for _, claim := range claims {
		status := sam.StatusForRemoteAddressClaim(claim, c.Lowerings, c.Store, targetOS)
		status["dryRun"] = c.DryRun
		if targetOS == platform.OSLinux {
			if spec, err := claim.RemoteAddressClaimSpec(); err == nil {
				if strings.TrimSpace(spec.Capture.Type) == "proxy-arp" {
					if status["captureStatus"] == sam.CaptureStatusCaptured {
						aliases := sam.CaptureInterfaceAliases(c.Router)
						status["captureProxyNeighbor"] = map[string]any{
							"address":   strings.TrimSpace(spec.Address),
							"interface": sam.ResolveCaptureInterface(strings.TrimSpace(spec.Capture.Interface), aliases),
						}
						if garpSent[claim.Metadata.Name] {
							status["lastGARPSent"] = true
						}
						if garpErrors[claim.Metadata.Name] != "" {
							status["lastGARPError"] = garpErrors[claim.Metadata.Name]
						}
					}
				}
				if providerSecondaryBGPCapture(spec) {
					aliases := sam.CaptureInterfaceAliases(c.Router)
					if iface := sam.ResolveCaptureInterface(strings.TrimSpace(spec.Capture.Interface), aliases); iface != "" {
						status["captureStatus"] = sam.CaptureStatusCaptured
						status["captureProxyNeighbor"] = map[string]any{
							"address":   strings.TrimSpace(spec.Address),
							"interface": iface,
						}
					}
				}
				if providerSecondaryEnforcesOSAddressAbsence(spec) {
					result := deassignResults[claim.Metadata.Name]
					note := map[string]any{
						"address": firstNonEmpty(result.address, strings.TrimSpace(spec.Address)),
						// enforced is an audit flag: routerd is actively enforcing
						// OS-absence for this provider-captured address.
						"enforced": true,
						// lastReconcileRemoved is a per-reconcile action signal. It
						// is false in steady state when the address was already absent.
						"lastReconcileRemoved": result.removedThisReconcile,
					}
					if result.ifname != "" {
						note["interface"] = result.ifname
					}
					if strings.TrimSpace(spec.Delivery.Mode) == "bgp" {
						note["reason"] = "bgp-delivery"
					} else {
						note["reason"] = "configureOSAddress=false"
					}
					status["captureOSAddressAbsence"] = note
				}
			}
		}
		if err := c.Store.SaveObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", claim.Metadata.Name, status); err != nil {
			return err
		}
	}
	for _, domain := range samSelectResources(c.Router.Spec.Resources, "AddressMobilityDomain") {
		status := sam.StatusForAddressMobilityDomain(domain, claims, c.Store)
		if err := c.Store.SaveObjectStatus(api.HybridAPIVersion, "AddressMobilityDomain", domain.Metadata.Name, status); err != nil {
			return err
		}
	}
	return nil
}

func providerSecondaryEnforcesOSAddressAbsence(spec api.RemoteAddressClaimSpec) bool {
	if strings.TrimSpace(spec.Capture.Type) != "provider-secondary-ip" {
		return false
	}
	return !spec.Capture.ConfigureOSAddress || strings.TrimSpace(spec.Delivery.Mode) == "bgp"
}

func providerSecondaryBGPCapture(spec api.RemoteAddressClaimSpec) bool {
	return strings.TrimSpace(spec.Capture.Type) == "provider-secondary-ip" && strings.TrimSpace(spec.Delivery.Mode) == "bgp"
}

func (c SAMController) cleanupRemovedCaptures(ctx context.Context, statuses []routerstate.ObjectStatus) error {
	if c.Store == nil {
		return nil
	}
	deleter, ok := c.Store.(interface {
		DeleteObject(apiVersion, kind, name string) error
	})
	if !ok {
		return nil
	}
	desired := map[string]bool{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion == api.HybridAPIVersion && resource.Kind == "RemoteAddressClaim" {
			desired[lifecycle.OwnerKey(resource.APIVersion, resource.Kind, resource.Metadata.Name)] = true
		}
	}
	applier := c.Applier
	if applier == nil {
		applier = defaultSAMProxyNeighborApplier()
	}
	recentlyAssigned := c.recentlyAssignedCaptureAddresses()
	plan := lifecycle.PlanResourceTeardownGC(desired, statuses)
	for _, action := range plan.Actions {
		if action.Type != lifecycle.GCActionTeardownResource {
			continue
		}
		status := action.Status
		if status.APIVersion != api.HybridAPIVersion || status.Kind != "RemoteAddressClaim" {
			continue
		}
		if capture, ok := samStoredProxyNeighborFromStatus(status); ok {
			addr := strings.TrimSuffix(strings.TrimSpace(capture.address), "/32")
			if recentlyAssigned[addr] {
				if removedCaptureHandoffEligible(status) {
					deferred, err := c.deferRemovedHandoffCapture(status)
					if err != nil {
						return err
					}
					if deferred {
						continue
					}
				} else {
					continue
				}
			}
		}
		deferred, err := c.deferRemovedHandoffCapture(status)
		if err != nil {
			return err
		}
		if deferred {
			continue
		}
		if err := c.teardownRemovedCapture(ctx, status, applier, deleter); err != nil {
			return err
		}
	}
	return nil
}

func (c SAMController) deferRemovedHandoffCapture(status routerstate.ObjectStatus) (bool, error) {
	if c.DryRun || c.Store == nil || !removedCaptureHandoffEligible(status) || handoffDestinationReady(status) {
		return false, nil
	}
	now := c.now().UTC()
	ttl := c.handoffLeaseTTL()
	since, ok := handoffPendingSince(status)
	if ok && !now.Before(since.Add(ttl)) {
		return false, nil
	}
	if !ok {
		since = now
	}
	next := copyStatusMap(status.Status)
	next["phase"] = "HandoffPending"
	next["reason"] = "SAMHandoffLease"
	next["message"] = "local SAM capture teardown is delayed briefly while handoff converges; BGP takeover is not dataplane ready"
	next["handoffPending"] = true
	next["handoffPendingSince"] = since.Format(time.RFC3339Nano)
	next["handoffLeaseTTL"] = ttl.String()
	next["handoffLeaseExpiresAt"] = since.Add(ttl).Format(time.RFC3339Nano)
	if err := c.Store.SaveObjectStatus(status.APIVersion, status.Kind, status.Name, next); err != nil {
		return false, err
	}
	return true, nil
}

func removedCaptureHandoffEligible(status routerstate.ObjectStatus) bool {
	if status.APIVersion != api.HybridAPIVersion || status.Kind != "RemoteAddressClaim" {
		return false
	}
	if strings.TrimSpace(fmt.Sprint(status.Status["captureType"])) != "provider-secondary-ip" {
		return false
	}
	if strings.TrimSpace(fmt.Sprint(status.Status["deliveryMode"])) != "bgp" {
		return false
	}
	if strings.TrimSpace(fmt.Sprint(status.Status["captureStatus"])) != sam.CaptureStatusCaptured {
		return false
	}
	_, ok := samStoredProxyNeighborFromStatus(status)
	return ok
}

func handoffDestinationReady(status routerstate.ObjectStatus) bool {
	if ready, ok := statusBool(status.Status["handoffDestinationReady"]); ok && ready {
		return true
	}
	if ready, ok := statusBool(status.Status["handoffReady"]); ok && ready {
		return true
	}
	return false
}

func handoffPendingSince(status routerstate.ObjectStatus) (time.Time, bool) {
	raw := strings.TrimSpace(fmt.Sprint(status.Status["handoffPendingSince"]))
	if raw == "" || raw == "<nil>" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func (c SAMController) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c SAMController) handoffLeaseTTL() time.Duration {
	if c.HandoffLeaseTTL > 0 {
		return c.HandoffLeaseTTL
	}
	if c.Router != nil {
		if value := strings.TrimSpace(c.Router.Spec.Apply.SAMHandoffLeaseTTL); value != "" {
			if duration, err := time.ParseDuration(value); err == nil && duration > 0 {
				return duration
			}
		}
	}
	return defaultSAMHandoffLeaseTTL
}

func (c SAMController) teardownRemovedCapture(ctx context.Context, status routerstate.ObjectStatus, applier samProxyNeighborApplier, deleter routerstate.ObjectDeleteStore) error {
	if !c.DryRun {
		if capture, ok := samStoredProxyNeighborFromStatus(status); ok {
			capture.ifname = sam.ResolveCaptureInterface(capture.ifname, sam.CaptureInterfaceAliases(c.Router))
			if err := applier.DeleteProxyNeighbor(ctx, capture.address, capture.ifname); err != nil {
				return fmt.Errorf("delete removed SAM proxy neighbor %s dev %s: %w", capture.address, capture.ifname, err)
			}
		}
	}
	if err := deleter.DeleteObject(api.HybridAPIVersion, "RemoteAddressClaim", status.Name); err != nil {
		return err
	}
	if c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.sam.capture.removed", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim", Name: status.Name}
		event.Attributes = map[string]string{"removedAt": time.Now().UTC().Format(time.RFC3339Nano)}
		if err := c.Bus.Publish(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (c SAMController) recentlyAssignedCaptureAddresses() map[string]bool {
	if c.ListActions == nil {
		return nil
	}
	actions, err := c.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		return nil
	}
	return latestAssignedAddresses(actions)
}

func (c SAMController) cleanupChangedCaptures(ctx context.Context, statuses []routerstate.ObjectStatus, actions []sam.CaptureAction) error {
	if c.Store == nil || c.DryRun {
		return nil
	}
	prior := samStoredProxyNeighbors(statuses)
	if len(prior) == 0 {
		return nil
	}
	desiredClaims := map[string]bool{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion == api.HybridAPIVersion && resource.Kind == "RemoteAddressClaim" {
			desiredClaims[resource.Metadata.Name] = true
		}
	}
	desiredNeighbors := map[string]samStoredProxyNeighbor{}
	for _, action := range actions {
		if action.Kind == "proxy-neighbor" {
			desiredNeighbors[action.ClaimName] = samStoredProxyNeighbor{address: strings.TrimSpace(action.Address), ifname: strings.TrimSpace(action.Interface)}
		}
	}
	applier := c.Applier
	if applier == nil {
		applier = defaultSAMProxyNeighborApplier()
	}
	aliases := sam.CaptureInterfaceAliases(c.Router)
	for name, old := range prior {
		if !desiredClaims[name] {
			continue
		}
		old.ifname = sam.ResolveCaptureInterface(old.ifname, aliases)
		next, ok := desiredNeighbors[name]
		if ok && next == old {
			continue
		}
		if err := applier.DeleteProxyNeighbor(ctx, old.address, old.ifname); err != nil {
			return fmt.Errorf("delete changed SAM proxy neighbor %s dev %s: %w", old.address, old.ifname, err)
		}
	}
	return nil
}

func (c SAMController) listObjectStatuses() ([]routerstate.ObjectStatus, error) {
	if c.Store == nil {
		return nil, nil
	}
	lister, ok := c.Store.(interface {
		ListObjectStatuses() ([]routerstate.ObjectStatus, error)
	})
	if !ok {
		return nil, nil
	}
	return lister.ListObjectStatuses()
}

func samStoredProxyNeighbors(statuses []routerstate.ObjectStatus) map[string]samStoredProxyNeighbor {
	out := map[string]samStoredProxyNeighbor{}
	for _, status := range statuses {
		if status.APIVersion != api.HybridAPIVersion || status.Kind != "RemoteAddressClaim" {
			continue
		}
		if capture, ok := samStoredProxyNeighborFromStatus(status); ok {
			out[status.Name] = capture
		}
	}
	return out
}

func samStoredProxyNeighborFromStatus(status routerstate.ObjectStatus) (samStoredProxyNeighbor, bool) {
	capture, ok := status.Status["captureProxyNeighbor"].(map[string]any)
	if !ok {
		return samStoredProxyNeighbor{}, false
	}
	address := strings.TrimSpace(fmt.Sprint(capture["address"]))
	ifname := strings.TrimSpace(fmt.Sprint(capture["interface"]))
	if address == "" || address == "<nil>" || ifname == "" || ifname == "<nil>" {
		return samStoredProxyNeighbor{}, false
	}
	return samStoredProxyNeighbor{address: address, ifname: ifname}, true
}
