// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/json"
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
	EnsureOSAddressPresent(ctx context.Context, address, ifname string) (samOSAddressAssignResult, error)
	EnsureOSAddressAbsent(ctx context.Context, address string) (samOSAddressDeassignResult, error)
	EnsureReturnPolicyRoute(ctx context.Context, sourceCIDR, destinationCIDR, ifname string, table, priority, metric int) error
	DeleteReturnPolicyRoute(ctx context.Context, sourceCIDR, destinationCIDR string, table, priority int) error
	EnsureForwardPath(ctx context.Context, captureIface, tunnelIface string) error
	DeleteForwardPath(ctx context.Context, captureIface, tunnelIface string) error
}

type samGratuitousARPAnnouncer interface {
	SendGratuitousARP(ctx context.Context, address, ifname string) error
}

type samStoredProxyNeighbor struct {
	address string
	ifname  string
}

type samOSAddressDeassignResult struct {
	address   string
	ifname    string
	enforced  bool
	lastError string
	// removedThisReconcile is true only when this reconcile deleted the
	// captured address from a local OS interface.
	removedThisReconcile bool
}

type samOSAddressAssignResult struct {
	address            string
	ifname             string
	enforced           bool
	lastError          string
	addedThisReconcile bool
}

type samProviderOwnershipBlock struct {
	address string
	ifname  string
	reason  string
}

type samReturnPolicyRouteResult struct {
	source      string
	destination string
	ifname      string
	table       int
	priority    int
	metric      int
	enforced    bool
	lastError   string
}

type samForwardPathResult struct {
	captureIface string
	tunnelIface  string
	enforced     bool
	lastError    string
}

func (r samForwardPathResult) key() string {
	return r.captureIface + "\x00" + r.tunnelIface
}

func (r samReturnPolicyRouteResult) sameDesired(next samReturnPolicyRouteResult) bool {
	return r.source == next.source &&
		r.destination == next.destination &&
		r.ifname == next.ifname &&
		r.table == next.table &&
		r.priority == next.priority &&
		r.metric == next.metric
}

func replaceSAMForwardPathResult(results []samForwardPathResult, next samForwardPathResult) []samForwardPathResult {
	for i, existing := range results {
		if existing.key() == next.key() {
			results[i] = next
			return results
		}
	}
	return append(results, next)
}

func samForwardPathStatusNotes(paths []samForwardPathResult) []map[string]any {
	notes := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		note := map[string]any{
			"captureInterface": path.captureIface,
			"tunnelInterface":  path.tunnelIface,
			"enforced":         path.enforced,
			"managedBy":        "routerd",
		}
		if path.lastError != "" {
			note["lastError"] = path.lastError
		}
		notes = append(notes, note)
	}
	return notes
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
	Router    *api.Router
	Bus       *bus.Bus
	Store     Store
	Lowerings []sam.DeliveryLowering
	DryRun    bool
	OS        platform.OS
	Applier   samProxyNeighborApplier
	GARP      samGratuitousARPAnnouncer
}

func (c SAMController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	targetOS := c.OS
	if targetOS == "" {
		targetOS = platform.CurrentOS()
	}
	if targetOS != platform.OSLinux {
		return c.reconcileStatuses(targetOS, nil, nil, nil, nil, nil, nil, nil)
	}
	statuses, err := c.listObjectStatuses()
	if err != nil {
		return err
	}
	if err := c.cleanupRemovedCaptures(ctx, statuses); err != nil {
		return err
	}
	actions, err := sam.PlanCaptureWithOptions(c.Router, targetOS, sam.PlanOptions{
		StatusReader: c.Store,
		ProviderOwnershipConfirmed: func(_ string, capture api.AddressCapture, address string) bool {
			return c.providerSecondaryOwnershipConfirmed(capture, address)
		},
	})
	if err != nil {
		return err
	}
	if err := c.cleanupChangedCaptures(ctx, statuses, actions); err != nil {
		return err
	}
	if err := c.reconcileProxyARPInterfaces(ctx, actions); err != nil {
		return err
	}
	var failures []string
	assignResults := map[string]samOSAddressAssignResult{}
	deassignResults := map[string]samOSAddressDeassignResult{}
	providerBlocks := map[string]samProviderOwnershipBlock{}
	returnRouteResults := map[string]samReturnPolicyRouteResult{}
	forwardPathResults := map[string][]samForwardPathResult{}
	garpSent := map[string]bool{}
	garpErrors := map[string]string{}
	priorNeighbors := samStoredProxyNeighbors(statuses)
	for _, action := range actions {
		switch action.Kind {
		case "provider-ownership-blocked":
			providerBlocks[action.ClaimName] = samProviderOwnershipBlock{
				address: strings.TrimSpace(action.Address),
				ifname:  strings.TrimSpace(action.Interface),
				reason:  "ProviderOwnershipPending",
			}
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
			if err != nil {
				result.lastError = err.Error()
				failures = append(failures, fmt.Sprintf("%s deassign %s: %v", action.ClaimName, action.Address, err))
			} else {
				result.enforced = true
			}
			deassignResults[action.ClaimName] = result
		case "assign-os-address":
			result := samOSAddressAssignResult{address: strings.TrimSpace(action.Address), ifname: strings.TrimSpace(action.Interface)}
			assignResults[action.ClaimName] = result
			if c.DryRun {
				continue
			}
			applier := c.Applier
			if applier == nil {
				applier = defaultSAMProxyNeighborApplier()
			}
			result, err := applier.EnsureOSAddressPresent(ctx, action.Address, action.Interface)
			if result.address == "" {
				result.address = strings.TrimSpace(action.Address)
			}
			if result.ifname == "" {
				result.ifname = strings.TrimSpace(action.Interface)
			}
			if err != nil {
				result.lastError = err.Error()
				failures = append(failures, fmt.Sprintf("%s assign %s dev %s: %v", action.ClaimName, action.Address, action.Interface, err))
			} else {
				result.enforced = true
			}
			assignResults[action.ClaimName] = result
		case "return-policy-route":
			result := samReturnPolicyRouteResult{
				source:      strings.TrimSpace(action.Address),
				destination: strings.TrimSpace(action.Destination),
				ifname:      strings.TrimSpace(action.Interface),
				table:       action.Table,
				priority:    action.Priority,
				metric:      action.Metric,
			}
			returnRouteResults[action.ClaimName] = result
			if c.DryRun {
				continue
			}
			applier := c.Applier
			if applier == nil {
				applier = defaultSAMProxyNeighborApplier()
			}
			err := applier.EnsureReturnPolicyRoute(ctx, action.Address, action.Destination, action.Interface, action.Table, action.Priority, action.Metric)
			if err != nil {
				result.lastError = err.Error()
				failures = append(failures, fmt.Sprintf("%s return route from %s to %s dev %s: %v", action.ClaimName, action.Address, action.Destination, action.Interface, err))
			} else {
				result.enforced = true
			}
			returnRouteResults[action.ClaimName] = result
		case "forward-path":
			result := samForwardPathResult{
				captureIface: strings.TrimSpace(action.Interface),
				tunnelIface:  strings.TrimSpace(action.PeerInterface),
			}
			forwardPathResults[action.ClaimName] = append(forwardPathResults[action.ClaimName], result)
			if c.DryRun {
				continue
			}
			applier := c.Applier
			if applier == nil {
				applier = defaultSAMProxyNeighborApplier()
			}
			err := applier.EnsureForwardPath(ctx, action.Interface, action.PeerInterface)
			if err != nil {
				result.lastError = err.Error()
				failures = append(failures, fmt.Sprintf("%s forward path %s <-> %s: %v", action.ClaimName, action.Interface, action.PeerInterface, err))
			} else {
				result.enforced = true
			}
			forwardPathResults[action.ClaimName] = replaceSAMForwardPathResult(forwardPathResults[action.ClaimName], result)
		default:
			continue
		}
	}
	if err := c.reconcileStatuses(targetOS, assignResults, deassignResults, providerBlocks, returnRouteResults, forwardPathResults, garpSent, garpErrors); err != nil {
		return err
	}
	if len(failures) > 0 {
		return fmt.Errorf("SAM capture failed: %s", strings.Join(failures, "; "))
	}
	return nil
}

func (c SAMController) reconcileProxyARPInterfaces(ctx context.Context, actions []sam.CaptureAction) error {
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
		if err != nil || strings.TrimSpace(spec.Capture.Type) != "proxy-arp" {
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

func (c SAMController) reconcileStatuses(targetOS platform.OS, assignResults map[string]samOSAddressAssignResult, deassignResults map[string]samOSAddressDeassignResult, providerBlocks map[string]samProviderOwnershipBlock, returnRouteResults map[string]samReturnPolicyRouteResult, forwardPathResults map[string][]samForwardPathResult, garpSent map[string]bool, garpErrors map[string]string) error {
	claims := samSelectResources(c.Router.Spec.Resources, "RemoteAddressClaim")
	for _, claim := range claims {
		status := sam.StatusForRemoteAddressClaim(claim, c.Lowerings, c.Store, targetOS)
		status["dryRun"] = c.DryRun
		if targetOS == platform.OSLinux {
			if spec, err := claim.RemoteAddressClaimSpec(); err == nil && strings.TrimSpace(spec.Capture.Type) == "proxy-arp" {
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
			} else if err == nil && strings.TrimSpace(spec.Capture.Type) == "provider-secondary-ip" {
				if spec.Capture.ConfigureOSAddress {
					if block, blocked := providerBlocks[claim.Metadata.Name]; blocked {
						status["phase"] = "Degraded"
						status["reason"] = block.reason
						status["captureStatus"] = sam.CaptureStatusBlocked
						status["cloudClaimPhase"] = sam.CloudClaimPending
						status["osCapturePhase"] = sam.OSCaptureMissing
						status["advertisementGatePhase"] = sam.AdvertisementGateBlocked
						status["samConvergencePhase"] = sam.SAMConvergenceDegraded
						status["blockingReasons"] = []string{"provider ownership not confirmed for provider-secondary-ip capture; OS address not installed"}
						status["captureProviderOwnership"] = map[string]any{
							"address":     firstNonEmpty(block.address, strings.TrimSpace(spec.Address)),
							"expectedRef": strings.TrimSpace(spec.Capture.NICRef),
							"providerRef": strings.TrimSpace(spec.Capture.ProviderRef),
							"confirmed":   false,
							"reason":      block.reason,
						}
						status["captureOSAddressPresence"] = map[string]any{
							"address":   firstNonEmpty(block.address, strings.TrimSpace(spec.Address)),
							"interface": block.ifname,
							"enforced":  false,
							"blocked":   true,
							"reason":    block.reason,
						}
						if err := c.Store.SaveObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", claim.Metadata.Name, status); err != nil {
							return err
						}
						continue
					}
					result := assignResults[claim.Metadata.Name]
					if result.ifname == "" {
						aliases := sam.CaptureInterfaceAliases(c.Router)
						result.ifname = sam.ResolveCaptureInterface(strings.TrimSpace(spec.Capture.Interface), aliases)
					}
					status["captureOSAddressPresence"] = map[string]any{
						"address":            firstNonEmpty(result.address, strings.TrimSpace(spec.Address)),
						"interface":          result.ifname,
						"enforced":           result.enforced,
						"lastReconcileAdded": result.addedThisReconcile,
					}
					if result.lastError != "" {
						status["captureOSAddressPresence"].(map[string]any)["lastError"] = result.lastError
					}
					if route, ok := returnRouteResults[claim.Metadata.Name]; ok {
						note := map[string]any{
							"source":      route.source,
							"destination": route.destination,
							"interface":   route.ifname,
							"table":       route.table,
							"priority":    route.priority,
							"metric":      route.metric,
							"enforced":    route.enforced,
						}
						if route.lastError != "" {
							note["lastError"] = route.lastError
						}
						status["captureReturnPolicyRoute"] = note
					}
					if paths := forwardPathResults[claim.Metadata.Name]; len(paths) > 0 {
						notes := samForwardPathStatusNotes(paths)
						status["captureForwardingPaths"] = notes
						status["captureForwardingPath"] = notes[0]
					}
				} else {
					result := deassignResults[claim.Metadata.Name]
					note := map[string]any{
						"address": firstNonEmpty(result.address, strings.TrimSpace(spec.Address)),
						// enforced is an audit flag: routerd is actively enforcing
						// OS-absence for this provider-captured address.
						"enforced": result.enforced,
						// lastReconcileRemoved is a per-reconcile action signal. It
						// is false in steady state when the address was already absent.
						"lastReconcileRemoved": result.removedThisReconcile,
					}
					if result.ifname != "" {
						note["interface"] = result.ifname
					}
					if result.lastError != "" {
						note["lastError"] = result.lastError
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

func (c SAMController) providerSecondaryOwnershipConfirmed(capture api.AddressCapture, address string) bool {
	providerRef := strings.TrimSpace(capture.ProviderRef)
	nicRef := strings.TrimSpace(capture.NICRef)
	address = strings.TrimSpace(address)
	if providerRef == "" || nicRef == "" || address == "" || c.Store == nil {
		return false
	}
	lister, ok := c.Store.(interface {
		ListActions(routerstate.ActionExecutionFilter) ([]routerstate.ActionExecutionRecord, error)
	})
	if !ok {
		return false
	}
	rows, err := lister.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row.Status != routerstate.ActionSucceeded || strings.TrimSpace(row.Action) != "assign-secondary-ip" || strings.TrimSpace(row.ProviderRef) != providerRef {
			continue
		}
		target := map[string]string{}
		if strings.TrimSpace(row.TargetJSON) != "" {
			if err := json.Unmarshal([]byte(row.TargetJSON), &target); err != nil {
				continue
			}
		}
		if strings.TrimSpace(target["nicRef"]) == nicRef && strings.TrimSpace(target["address"]) == address {
			return true
		}
	}
	return false
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
	plan := lifecycle.PlanResourceTeardownGC(desired, statuses)
	for _, action := range plan.Actions {
		if action.Type != lifecycle.GCActionTeardownResource {
			continue
		}
		status := action.Status
		if status.APIVersion != api.HybridAPIVersion || status.Kind != "RemoteAddressClaim" {
			continue
		}
		if err := c.teardownRemovedCapture(ctx, status, applier, deleter); err != nil {
			return err
		}
	}
	return nil
}

func (c SAMController) teardownRemovedCapture(ctx context.Context, status routerstate.ObjectStatus, applier samProxyNeighborApplier, deleter routerstate.ObjectDeleteStore) error {
	if !c.DryRun {
		if capture, ok := samStoredProxyNeighborFromStatus(status); ok {
			capture.ifname = sam.ResolveCaptureInterface(capture.ifname, sam.CaptureInterfaceAliases(c.Router))
			if err := applier.DeleteProxyNeighbor(ctx, capture.address, capture.ifname); err != nil {
				return fmt.Errorf("delete removed SAM proxy neighbor %s dev %s: %w", capture.address, capture.ifname, err)
			}
		}
		if address, ok := samStoredOSAddressPresenceFromStatus(status); ok {
			if _, err := applier.EnsureOSAddressAbsent(ctx, address); err != nil {
				return fmt.Errorf("delete removed SAM OS address %s: %w", address, err)
			}
		}
		if route, ok := samStoredReturnPolicyRouteFromStatus(status); ok {
			if err := applier.DeleteReturnPolicyRoute(ctx, route.source, route.destination, route.table, route.priority); err != nil {
				return fmt.Errorf("delete removed SAM return policy route from %s to %s table %d priority %d: %w", route.source, route.destination, route.table, route.priority, err)
			}
		}
		for _, path := range samStoredForwardPathsFromStatus(status) {
			if err := applier.DeleteForwardPath(ctx, path.captureIface, path.tunnelIface); err != nil {
				return fmt.Errorf("delete removed SAM forward path %s <-> %s: %w", path.captureIface, path.tunnelIface, err)
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

func (c SAMController) cleanupChangedCaptures(ctx context.Context, statuses []routerstate.ObjectStatus, actions []sam.CaptureAction) error {
	if c.Store == nil || c.DryRun {
		return nil
	}
	prior := samStoredProxyNeighbors(statuses)
	priorOS := samStoredOSAddressPresences(statuses)
	priorReturnRoutes := samStoredReturnPolicyRoutes(statuses)
	priorForwardPaths := samStoredForwardPaths(statuses)
	if len(prior) == 0 && len(priorOS) == 0 && len(priorReturnRoutes) == 0 && len(priorForwardPaths) == 0 {
		return nil
	}
	desiredClaims := map[string]bool{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion == api.HybridAPIVersion && resource.Kind == "RemoteAddressClaim" {
			desiredClaims[resource.Metadata.Name] = true
		}
	}
	desiredNeighbors := map[string]samStoredProxyNeighbor{}
	desiredOS := map[string]string{}
	desiredReturnRoutes := map[string]samReturnPolicyRouteResult{}
	desiredForwardPaths := map[string]map[string]samForwardPathResult{}
	for _, action := range actions {
		switch action.Kind {
		case "proxy-neighbor":
			desiredNeighbors[action.ClaimName] = samStoredProxyNeighbor{address: strings.TrimSpace(action.Address), ifname: strings.TrimSpace(action.Interface)}
		case "assign-os-address":
			desiredOS[action.ClaimName] = strings.TrimSpace(action.Address)
		case "return-policy-route":
			desiredReturnRoutes[action.ClaimName] = samReturnPolicyRouteResult{
				source:      strings.TrimSpace(action.Address),
				destination: strings.TrimSpace(action.Destination),
				ifname:      strings.TrimSpace(action.Interface),
				table:       action.Table,
				priority:    action.Priority,
				metric:      action.Metric,
			}
		case "forward-path":
			path := samForwardPathResult{
				captureIface: strings.TrimSpace(action.Interface),
				tunnelIface:  strings.TrimSpace(action.PeerInterface),
			}
			if desiredForwardPaths[action.ClaimName] == nil {
				desiredForwardPaths[action.ClaimName] = map[string]samForwardPathResult{}
			}
			desiredForwardPaths[action.ClaimName][path.key()] = path
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
	for name, oldAddress := range priorOS {
		if !desiredClaims[name] {
			continue
		}
		nextAddress, ok := desiredOS[name]
		if ok && nextAddress == oldAddress {
			continue
		}
		if _, err := applier.EnsureOSAddressAbsent(ctx, oldAddress); err != nil {
			return fmt.Errorf("delete changed SAM OS address %s: %w", oldAddress, err)
		}
	}
	for name, old := range priorReturnRoutes {
		if !desiredClaims[name] {
			continue
		}
		next, ok := desiredReturnRoutes[name]
		if ok && old.sameDesired(next) {
			continue
		}
		if err := applier.DeleteReturnPolicyRoute(ctx, old.source, old.destination, old.table, old.priority); err != nil {
			return fmt.Errorf("delete changed SAM return policy route from %s to %s table %d priority %d: %w", old.source, old.destination, old.table, old.priority, err)
		}
	}
	for name, paths := range priorForwardPaths {
		if !desiredClaims[name] {
			continue
		}
		for _, old := range paths {
			if desiredForwardPaths[name][old.key()] == old {
				continue
			}
			if err := applier.DeleteForwardPath(ctx, old.captureIface, old.tunnelIface); err != nil {
				return fmt.Errorf("delete changed SAM forward path %s <-> %s: %w", old.captureIface, old.tunnelIface, err)
			}
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

func samStoredOSAddressPresences(statuses []routerstate.ObjectStatus) map[string]string {
	out := map[string]string{}
	for _, status := range statuses {
		if status.APIVersion != api.HybridAPIVersion || status.Kind != "RemoteAddressClaim" {
			continue
		}
		if address, ok := samStoredOSAddressPresenceFromStatus(status); ok {
			out[status.Name] = address
		}
	}
	return out
}

func samStoredReturnPolicyRoutes(statuses []routerstate.ObjectStatus) map[string]samReturnPolicyRouteResult {
	out := map[string]samReturnPolicyRouteResult{}
	for _, status := range statuses {
		if status.APIVersion != api.HybridAPIVersion || status.Kind != "RemoteAddressClaim" {
			continue
		}
		if route, ok := samStoredReturnPolicyRouteFromStatus(status); ok {
			out[status.Name] = route
		}
	}
	return out
}

func samStoredForwardPaths(statuses []routerstate.ObjectStatus) map[string][]samForwardPathResult {
	out := map[string][]samForwardPathResult{}
	for _, status := range statuses {
		if status.APIVersion != api.HybridAPIVersion || status.Kind != "RemoteAddressClaim" {
			continue
		}
		if paths := samStoredForwardPathsFromStatus(status); len(paths) > 0 {
			out[status.Name] = paths
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

func samStoredOSAddressPresenceFromStatus(status routerstate.ObjectStatus) (string, bool) {
	capture, ok := status.Status["captureOSAddressPresence"].(map[string]any)
	if !ok {
		return "", false
	}
	enforced, ok := capture["enforced"].(bool)
	if !ok || !enforced {
		return "", false
	}
	address := strings.TrimSpace(fmt.Sprint(capture["address"]))
	if address == "" || address == "<nil>" {
		return "", false
	}
	return address, true
}

func samStoredReturnPolicyRouteFromStatus(status routerstate.ObjectStatus) (samReturnPolicyRouteResult, bool) {
	capture, ok := status.Status["captureReturnPolicyRoute"].(map[string]any)
	if !ok {
		return samReturnPolicyRouteResult{}, false
	}
	route := samReturnPolicyRouteResult{
		source:      strings.TrimSpace(fmt.Sprint(capture["source"])),
		destination: strings.TrimSpace(fmt.Sprint(capture["destination"])),
		ifname:      strings.TrimSpace(fmt.Sprint(capture["interface"])),
		table:       intFromStatus(capture["table"]),
		priority:    intFromStatus(capture["priority"]),
		metric:      intFromStatus(capture["metric"]),
	}
	if route.source == "" || route.source == "<nil>" || route.destination == "" || route.destination == "<nil>" || route.table == 0 || route.priority == 0 {
		return samReturnPolicyRouteResult{}, false
	}
	return route, true
}

func samStoredForwardPathsFromStatus(status routerstate.ObjectStatus) []samForwardPathResult {
	if captures, ok := status.Status["captureForwardingPaths"].([]map[string]any); ok {
		out := make([]samForwardPathResult, 0, len(captures))
		for _, capture := range captures {
			if path, ok := samStoredForwardPathFromMap(capture); ok {
				out = append(out, path)
			}
		}
		return out
	}
	if captures, ok := status.Status["captureForwardingPaths"].([]any); ok {
		var out []samForwardPathResult
		for _, item := range captures {
			capture, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if path, ok := samStoredForwardPathFromMap(capture); ok {
				out = append(out, path)
			}
		}
		return out
	}
	capture, ok := status.Status["captureForwardingPath"].(map[string]any)
	if !ok {
		return nil
	}
	if path, ok := samStoredForwardPathFromMap(capture); ok {
		return []samForwardPathResult{path}
	}
	return nil
}

func samStoredForwardPathFromMap(capture map[string]any) (samForwardPathResult, bool) {
	if strings.TrimSpace(fmt.Sprint(capture["managedBy"])) != "routerd" {
		return samForwardPathResult{}, false
	}
	path := samForwardPathResult{
		captureIface: strings.TrimSpace(fmt.Sprint(capture["captureInterface"])),
		tunnelIface:  strings.TrimSpace(fmt.Sprint(capture["tunnelInterface"])),
	}
	if path.captureIface == "" || path.captureIface == "<nil>" || path.tunnelIface == "" || path.tunnelIface == "<nil>" {
		return samForwardPathResult{}, false
	}
	return path, true
}

func intFromStatus(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		out, _ := v.Int64()
		return int(out)
	default:
		var out int
		_, _ = fmt.Sscanf(strings.TrimSpace(fmt.Sprint(value)), "%d", &out)
		return out
	}
}
