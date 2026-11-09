// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/hostdeps"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/sam"
)

type samOSAddressDeassignResult struct {
	removedThisReconcile bool
}

// SAMController applies only the typed local capture intents emitted by a
// MobilityPool plan. It deliberately does not inspect status or legacy SAM
// resources to rediscover desired state.
type SAMController struct {
	Router              *api.Router
	Store               Store
	LocalCaptureIntents []dynamicconfig.LocalCaptureIntent
	DryRun              bool
}

func (c SAMController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
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
	state, err := c.reconcileState(actions, targetOS)
	if err != nil {
		return err
	}
	if !c.DryRun && (len(actions) != 0 || len(state.previousProxyNeighbors) != 0 || len(state.heldForwardPaths) != 0 || len(state.previousProxyARP) != 0) {
		if _, ok := c.Store.(objectStatusMerger); !ok {
			return fmt.Errorf("SAM capture effects require an applied-effect status merger")
		}
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
		deassigned[action.IntentID] = result.removedThisReconcile
	}
	if len(failures) != 0 {
		return fmt.Errorf("SAM capture failed: %s", strings.Join(failures, "; "))
	}
	if err := c.reconcileProxyARPSysctls(ctx, actions, &state); err != nil {
		return err
	}
	if err := c.reconcileSAMIPForwarding(ctx, targetOS, actions, state); err != nil {
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
	return c.saveAppliedDataplane(actions, state)
}

type samAppliedProxyNeighbor struct {
	ID         string `json:"id"`
	PoolRef    string `json:"poolRef"`
	PoolPrefix string `json:"poolPrefix"`
	Address    string `json:"address"`
	Interface  string `json:"interface"`
}

type samAppliedForwardPath struct {
	ID            string `json:"id"`
	PoolRef       string `json:"poolRef"`
	PoolPrefix    string `json:"poolPrefix"`
	Kind          string `json:"kind"`
	Address       string `json:"address"`
	Interface     string `json:"interface"`
	PeerInterface string `json:"peerInterface"`
}

// samAppliedProxyARP records interfaces that may require cleanup after removal.
type samAppliedProxyARP struct {
	Interface string `json:"interface"`
}

const samDataplaneStatusName = "sam-dataplane"

// samReconcileState is one observed snapshot for a local SAM reconcile. The
// applied status is used only to retain or remove effects that routerd already
// installed; it never creates new desired capture.
type samReconcileState struct {
	previousProxyNeighbors []samAppliedProxyNeighbor
	heldProxyNeighbors     []samAppliedProxyNeighbor
	heldForwardPaths       []samAppliedForwardPath
	heldProxyARPIDs        map[string]bool
	previousProxyARP       map[string]samAppliedProxyARP
	nextProxyARP           map[string]samAppliedProxyARP
	forwardPaths           []sam.CaptureAction
}

func (c SAMController) reconcileState(actions []sam.CaptureAction, targetOS platform.OS) (samReconcileState, error) {
	status := c.Store.ObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName)
	previousProxyNeighbors, err := samDecodeRecords(status["appliedProxyNeighbors"], statusValuePresent(status, "appliedProxyNeighbors"), validateSAMAppliedProxyNeighbor)
	if err != nil {
		return samReconcileState{}, fmt.Errorf("decode applied SAM proxy-neighbor ledger: %w", err)
	}
	previousForwardPaths, err := samDecodeRecords(status["appliedForwardPaths"], statusValuePresent(status, "appliedForwardPaths"), validateSAMAppliedForwardPath)
	if err != nil {
		return samReconcileState{}, fmt.Errorf("decode applied SAM forward-path ledger: %w", err)
	}
	proxySettings, err := samDecodeRecords(status["appliedProxyARP"], statusValuePresent(status, "appliedProxyARP"), func(setting samAppliedProxyARP) error {
		if targetOS == platform.OSFreeBSD {
			if setting.Interface != "" {
				return fmt.Errorf("FreeBSD proxyall ledger interface must be empty")
			}
			return nil
		}
		return sam.ValidateCaptureInterface(setting.Interface)
	})
	if err != nil {
		return samReconcileState{}, fmt.Errorf("decode applied SAM proxy_arp ledger: %w", err)
	}
	previousProxyARP := make(map[string]samAppliedProxyARP, len(proxySettings))
	for _, setting := range proxySettings {
		previousProxyARP[setting.Interface] = setting
	}
	held, heldProxyARPIDs, err := samHeldCaptures(c.LocalCaptureIntents)
	if err != nil {
		return samReconcileState{}, err
	}
	state := samReconcileState{
		previousProxyNeighbors: previousProxyNeighbors,
		heldProxyARPIDs:        heldProxyARPIDs,
		previousProxyARP:       previousProxyARP,
		nextProxyARP:           map[string]samAppliedProxyARP{},
	}
	for _, applied := range state.previousProxyNeighbors {
		if hold, ok := held[applied.ID]; ok && hold.matchesProxyNeighbor(applied) {
			state.heldProxyNeighbors = append(state.heldProxyNeighbors, applied)
		}
	}
	for _, applied := range previousForwardPaths {
		if hold, ok := held[applied.ID]; ok && hold.matchesForwardPath(applied) {
			state.heldForwardPaths = append(state.heldForwardPaths, applied)
		}
	}
	state.forwardPaths = reconciledSAMForwardPaths(actions, state.heldForwardPaths)
	return state, nil
}

type samHeldCapture struct {
	id, poolRef, poolPrefix, address, captureType, captureInterface string
	tunnels                                                         map[string]bool
}

func (h samHeldCapture) matchesProxyNeighbor(applied samAppliedProxyNeighbor) bool {
	return h.id == applied.ID && h.poolRef == applied.PoolRef && h.poolPrefix == applied.PoolPrefix &&
		h.address == applied.Address && h.captureInterface == applied.Interface
}

func (h samHeldCapture) matchesForwardPath(applied samAppliedForwardPath) bool {
	wantKind := ""
	switch h.captureType {
	case "provider-secondary-ip":
		wantKind = "forward-path"
	case "proxy-arp":
		wantKind = "forward-local-path"
	}
	return wantKind != "" && h.id == applied.ID && h.poolRef == applied.PoolRef &&
		h.poolPrefix == applied.PoolPrefix && h.address == applied.Address && h.captureInterface == applied.Interface &&
		applied.Kind == wantKind && h.tunnels[applied.PeerInterface]
}

func samHeldCaptures(intents []dynamicconfig.LocalCaptureIntent) (map[string]samHeldCapture, map[string]bool, error) {
	held, proxyARP := map[string]samHeldCapture{}, map[string]bool{}
	for _, intent := range intents {
		if intent.Disposition != dynamicconfig.CaptureHold {
			continue
		}
		address, err := canonicalSAMIPv4Host(intent.Address)
		if err != nil {
			return nil, nil, err
		}
		scope, err := dynamicconfig.ParseCanonicalIPv4Prefix(intent.PoolPrefix)
		if err != nil {
			return nil, nil, err
		}
		if !scope.Contains(address.Addr()) {
			return nil, nil, fmt.Errorf("held local capture intent %q address %q is outside poolPrefix %q", intent.ID, intent.Address, intent.PoolPrefix)
		}
		if err := sam.ValidateCaptureInterface(intent.CaptureInterface); err != nil {
			return nil, nil, err
		}
		id := intent.ID
		if _, exists := held[id]; exists {
			return nil, nil, fmt.Errorf("duplicate held local capture intent %q", id)
		}
		capture := samHeldCapture{
			id:               id,
			poolRef:          intent.PoolRef,
			poolPrefix:       intent.PoolPrefix,
			address:          address.String(),
			captureType:      intent.CaptureType,
			captureInterface: intent.CaptureInterface,
			tunnels:          map[string]bool{},
		}
		for _, tunnel := range intent.TunnelInterfaces {
			capture.tunnels[tunnel] = true
		}
		held[id] = capture
		if intent.CaptureType == "proxy-arp" {
			proxyARP[id] = true
		}
	}
	return held, proxyARP, nil
}

// cleanupReleasedProxyNeighbors removes recorded entries no longer needed by capture intents.
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
	for _, applied := range state.previousProxyNeighbors {
		if desired[key(applied.Address, applied.Interface)] {
			continue
		}
		if err := applier.DeleteProxyNeighbor(ctx, applied.Address, applied.Interface); err != nil {
			return fmt.Errorf("remove stale SAM proxy neighbor %s dev %s: %w", applied.Address, applied.Interface, err)
		}
	}
	return nil
}

func (c SAMController) saveAppliedDataplane(actions []sam.CaptureAction, state samReconcileState) error {
	merger, ok := c.Store.(objectStatusMerger)
	if !ok || c.DryRun {
		if c.DryRun {
			return nil
		}
		return fmt.Errorf("SAM capture effects require an applied-effect status merger")
	}
	neighborsByKey := map[string]samAppliedProxyNeighbor{}
	for _, action := range actions {
		if action.Kind == "proxy-neighbor" {
			neighbor := samAppliedProxyNeighbor{ID: action.IntentID, PoolRef: action.PoolRef, PoolPrefix: action.PoolPrefix, Address: action.Address, Interface: action.Interface}
			neighborsByKey[samProxyNeighborKey(neighbor.ID, neighbor.Address, neighbor.Interface)] = neighbor
		}
	}
	for _, applied := range state.heldProxyNeighbors {
		neighborsByKey[samProxyNeighborKey(applied.ID, applied.Address, applied.Interface)] = applied
	}
	neighbors := make([]samAppliedProxyNeighbor, 0, len(neighborsByKey))
	for _, neighbor := range neighborsByKey {
		neighbors = append(neighbors, neighbor)
	}
	sort.Slice(neighbors, func(i, j int) bool {
		return samProxyNeighborKey(neighbors[i].ID, neighbors[i].Address, neighbors[i].Interface) < samProxyNeighborKey(neighbors[j].ID, neighbors[j].Address, neighbors[j].Interface)
	})

	pathsByKey := map[string]samAppliedForwardPath{}
	for _, path := range state.forwardPaths {
		applied := samAppliedForwardPath{ID: path.IntentID, PoolRef: path.PoolRef, PoolPrefix: path.PoolPrefix, Kind: path.Kind, Address: path.Address, Interface: path.Interface, PeerInterface: path.PeerInterface}
		pathsByKey[samForwardPathKey(applied)] = applied
	}
	paths := make([]samAppliedForwardPath, 0, len(pathsByKey))
	for _, path := range pathsByKey {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool { return samForwardPathKey(paths[i]) < samForwardPathKey(paths[j]) })
	proxyARP := make([]samAppliedProxyARP, 0, len(state.nextProxyARP))
	for _, setting := range state.nextProxyARP {
		proxyARP = append(proxyARP, setting)
	}
	sort.Slice(proxyARP, func(i, j int) bool { return proxyARP[i].Interface < proxyARP[j].Interface })
	return merger.MergeObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName, map[string]any{
		"appliedProxyNeighbors": neighbors,
		"appliedForwardPaths":   paths,
		"appliedProxyARP":       proxyARP,
	})
}

func samDecodeRecords[T any](value any, present bool, validate func(T) error) ([]T, error) {
	if !present {
		return nil, nil
	}
	items, ok := decodeAppliedDataplaneStatus[T](value)
	if !ok || items == nil {
		return nil, fmt.Errorf("must be a non-null JSON array")
	}
	for _, item := range items {
		if err := validate(item); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func validateSAMAppliedProxyNeighbor(neighbor samAppliedProxyNeighbor) error {
	if err := validateSAMAppliedLedgerScope(neighbor.ID, neighbor.PoolRef, neighbor.PoolPrefix, neighbor.Address); err != nil {
		return err
	}
	if err := sam.ValidateCaptureInterface(neighbor.Interface); err != nil {
		return err
	}
	return nil
}

func validateSAMAppliedForwardPath(path samAppliedForwardPath) error {
	if path.Kind != "forward-path" && path.Kind != "forward-local-path" {
		return fmt.Errorf("unsupported applied forward-path kind %q", path.Kind)
	}
	if err := validateSAMAppliedLedgerScope(path.ID, path.PoolRef, path.PoolPrefix, path.Address); err != nil {
		return err
	}
	if err := sam.ValidateCaptureInterface(path.Interface); err != nil {
		return err
	}
	return sam.ValidateCaptureInterface(path.PeerInterface)
}

func validateSAMAppliedLedgerScope(id, poolRef, poolPrefix, address string) error {
	if id == "" || strings.TrimSpace(id) != id || strings.ContainsRune(id, '\x00') ||
		poolRef == "" || strings.TrimSpace(poolRef) != poolRef || strings.ContainsRune(poolRef, '\x00') {
		return fmt.Errorf("id and poolRef must be non-empty canonical tokens")
	}
	scope, err := dynamicconfig.ParseCanonicalIPv4Prefix(poolPrefix)
	if err != nil {
		return fmt.Errorf("poolPrefix: %w", err)
	}
	host, err := canonicalSAMIPv4Host(address)
	if err != nil || !scope.Contains(host.Addr()) {
		return fmt.Errorf("address %q must be a canonical IPv4 host within %q", address, poolPrefix)
	}
	return nil
}

func canonicalSAMIPv4Host(value string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(value)
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 || prefix.Masked().String() != value {
		return netip.Prefix{}, fmt.Errorf("%q must be a canonical IPv4 host prefix", value)
	}
	return prefix, nil
}

func statusValuePresent(status map[string]any, key string) bool {
	_, present := status[key]
	return present
}

// decodeAppliedDataplaneStatus is the one codec for observed local dataplane
// effects. Desired state never crosses this boundary: it comes only from the
// typed MobilityDataplane plan. JSON keeps SQLite-decoded map values and
// in-memory typed test/runtime values on the same small, validated path.
func decodeAppliedDataplaneStatus[T any](value any) ([]T, bool) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var decoded []T
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, false
	}
	return decoded, decoder.Decode(&struct{}{}) == io.EOF
}

func samProxyNeighborKey(id, address, ifname string) string {
	return strings.TrimSpace(id) + "\x00" + strings.TrimSpace(address) + "\x00" + strings.TrimSpace(ifname)
}

func samForwardPathKey(path samAppliedForwardPath) string {
	return strings.TrimSpace(path.ID) + "\x00" + strings.TrimSpace(path.PoolRef) + "\x00" + strings.TrimSpace(path.PoolPrefix) + "\x00" + strings.TrimSpace(path.Kind) + "\x00" + strings.TrimSpace(path.Address) + "\x00" + strings.TrimSpace(path.Interface) + "\x00" + strings.TrimSpace(path.PeerInterface)
}

func reconciledSAMForwardPaths(actions []sam.CaptureAction, held []samAppliedForwardPath) []sam.CaptureAction {
	byKey := map[string]sam.CaptureAction{}
	for _, action := range actions {
		if action.Kind != "forward-path" && action.Kind != "forward-local-path" {
			continue
		}
		path := samAppliedForwardPath{ID: action.IntentID, PoolRef: action.PoolRef, PoolPrefix: action.PoolPrefix, Kind: action.Kind, Address: action.Address, Interface: action.Interface, PeerInterface: action.PeerInterface}
		byKey[samForwardPathKey(path)] = action
	}
	for _, applied := range held {
		key := samForwardPathKey(applied)
		if _, exists := byKey[key]; !exists {
			byKey[key] = sam.CaptureAction{Kind: applied.Kind, IntentID: applied.ID, PoolRef: applied.PoolRef, PoolPrefix: applied.PoolPrefix, Address: applied.Address, Interface: applied.Interface, PeerInterface: applied.PeerInterface}
		}
	}
	paths := make([]sam.CaptureAction, 0, len(byKey))
	for _, path := range byKey {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		left := samAppliedForwardPath{ID: paths[i].IntentID, PoolRef: paths[i].PoolRef, PoolPrefix: paths[i].PoolPrefix, Kind: paths[i].Kind, Address: paths[i].Address, Interface: paths[i].Interface, PeerInterface: paths[i].PeerInterface}
		right := samAppliedForwardPath{ID: paths[j].IntentID, PoolRef: paths[j].PoolRef, PoolPrefix: paths[j].PoolPrefix, Kind: paths[j].Kind, Address: paths[j].Address, Interface: paths[j].Interface, PeerInterface: paths[j].PeerInterface}
		return samForwardPathKey(left) < samForwardPathKey(right)
	})
	return paths
}

func (c SAMController) reconcileSAMIPForwarding(ctx context.Context, targetOS platform.OS, actions []sam.CaptureAction, state samReconcileState) error {
	if c.DryRun {
		return nil
	}
	forwardPath, forwardingIntent := false, false
	key := "net.ipv4.ip_forward"
	if targetOS == platform.OSFreeBSD {
		key = "net.inet.ip.forwarding"
	}
	for _, action := range state.forwardPaths {
		forwardPath = forwardPath || action.Kind == "forward-path" || action.Kind == "forward-local-path"
	}
	for _, action := range actions {
		forwardingIntent = forwardingIntent || action.Kind == "sysctl" && action.Key == key && action.Value == "1"
	}
	if !forwardPath {
		return nil
	}
	if !forwardingIntent && len(state.heldForwardPaths) == 0 {
		return fmt.Errorf("SAM forwarding path is missing planned %s=1", key)
	}
	applier := defaultSAMProxyNeighborApplier()
	if err := applier.SetIPForwarding(ctx, true); err != nil {
		return fmt.Errorf("enable SAM IP forwarding: %w", err)
	}
	return nil
}

func (c SAMController) reconcileForwardPaths(ctx context.Context, state samReconcileState) error {
	if c.DryRun {
		return nil
	}
	applier := defaultSAMProxyNeighborApplier()
	return applier.ReconcileForwardPaths(ctx, state.forwardPaths)
}

func (c SAMController) reconcileProxyARPSysctls(ctx context.Context, actions []sam.CaptureAction, state *samReconcileState) error {
	if c.DryRun {
		return nil
	}
	targetOS := platform.CurrentOS()
	applier := defaultSAMProxyNeighborApplier()
	desired := map[string]bool{}
	if targetOS == platform.OSFreeBSD {
		if freeBSDProxyARPEnabled(actions) || len(state.heldProxyNeighbors) != 0 {
			desired[""] = true
		}
	} else {
		for _, action := range actions {
			if action.Kind == "sysctl" && strings.HasSuffix(action.Key, ".proxy_arp") && action.Value == "1" && action.Interface != "" {
				desired[action.Interface] = true
			}
		}
		for _, applied := range state.heldProxyNeighbors {
			if state.heldProxyARPIDs[applied.ID] {
				desired[applied.Interface] = true
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
	}
	interfaces := map[string]bool{}
	for iface := range desired {
		interfaces[iface] = true
	}
	for iface := range state.previousProxyARP {
		interfaces[iface] = true
	}
	ordered := make([]string, 0, len(interfaces))
	for iface := range interfaces {
		ordered = append(ordered, iface)
	}
	sort.Strings(ordered)
	state.nextProxyARP = map[string]samAppliedProxyARP{}
	for _, iface := range ordered {
		if err := applier.SetProxyARP(ctx, iface, desired[iface]); err != nil {
			return fmt.Errorf("set SAM proxy_arp %q=%t: %w", iface, desired[iface], err)
		}
		if desired[iface] {
			state.nextProxyARP[iface] = samAppliedProxyARP{Interface: iface}
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
