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
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/sam"
)

type samProxyNeighborApplier interface {
	SetProxyARP(ctx context.Context, ifname string, enabled, owned bool, beforeAcquire func() error) (samProxyARPApplyResult, error)
	SetAcceptLocal(ctx context.Context, ifname string, enabled, owned bool, beforeAcquire func() error) (samProxyARPApplyResult, error)
	SetIPForwarding(ctx context.Context, enabled bool) error
	EnsureProxyNeighbor(ctx context.Context, address, ifname string, owned bool, beforeAcquire func() error) error
	DeleteProxyNeighbor(ctx context.Context, address, ifname string) error
	EnsureOSAddressAbsent(ctx context.Context, address string) error
	ReconcileForwardPaths(ctx context.Context, paths []sam.CaptureAction, setAcceptLocal func(context.Context, string, bool) error) error
}

func recordSAMAcquisition(beforeAcquire func() error) error {
	if beforeAcquire == nil {
		return fmt.Errorf("SAM native acquisition requires durable ownership recording")
	}
	return beforeAcquire()
}

// applySAMOwnedBinarySetting observes before claiming a setting. Recording the
// transition from zero must commit before the write; an existing unowned one
// is an operator setting and can authorize neither adoption nor deletion.
func applySAMOwnedBinarySetting(key string, enabled, owned bool, read func() (string, error), write func(string) error, beforeAcquire func() error) (samProxyARPApplyResult, error) {
	current, err := read()
	if err != nil {
		return samProxyARPApplyResult{}, err
	}
	if current != "0" && current != "1" {
		return samProxyARPApplyResult{}, fmt.Errorf("%s has unexpected value %q", key, current)
	}
	if current == "1" && !owned {
		operation := "disable"
		if enabled {
			operation = "adoption"
		}
		return samProxyARPApplyResult{}, fmt.Errorf("foreign %s=1: refusing %s", key, operation)
	}
	want := "0"
	if enabled {
		want = "1"
	}
	if current == want {
		return samProxyARPApplyResult{}, nil
	}
	if enabled {
		if err := recordSAMAcquisition(beforeAcquire); err != nil {
			return samProxyARPApplyResult{}, err
		}
	}
	if err := write(want); err != nil {
		return samProxyARPApplyResult{}, err
	}
	return samProxyARPApplyResult{changedByRouterd: enabled}, nil
}

// samProxyARPApplyResult records whether this reconcile changed a sysctl from
// its safe disabled value.  The applied-effect ledger persists only that
// ownership proof; an already-enabled operator setting is never adopted.
type samProxyARPApplyResult struct {
	changedByRouterd bool
}

type samGratuitousARPAnnouncer interface {
	SendGratuitousARP(ctx context.Context, address, ifname string) error
}

// SAMController applies only the typed local capture intents emitted by a
// MobilityPool plan. It deliberately does not inspect status or legacy SAM
// resources to rediscover desired state.
type SAMController struct {
	Router              *api.Router
	Store               Store
	LocalCaptureIntents []dynamicconfig.LocalCaptureIntent
	Withdrawals         []dynamicconfig.MobilityWithdrawalRequest
	DryRun              bool
	OS                  platform.OS
	Applier             samProxyNeighborApplier
	GARP                samGratuitousARPAnnouncer
}

func (c SAMController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	targetOS := c.OS
	if targetOS == "" {
		targetOS = platform.CurrentOS()
	}
	if targetOS != platform.OSLinux && targetOS != platform.OSFreeBSD {
		return fmt.Errorf("SAM native capture is unsupported on %s", targetOS)
	}
	for _, request := range c.Withdrawals {
		if err := dynamicconfig.ValidateMobilityWithdrawalRequest(request); err != nil {
			return err
		}
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
	if err := c.beginPendingAcquisitions(actions, &state); err != nil {
		return err
	}
	blocked := map[string]bool{}
	var failures []string
	applier := c.Applier
	if applier == nil {
		applier = defaultSAMProxyNeighborApplier()
	}
	for _, action := range actions {
		if action.Kind != "deassign-os-address" || c.DryRun {
			continue
		}
		if err := applier.EnsureOSAddressAbsent(ctx, action.Address); err != nil {
			blocked[action.IntentID] = true
			failures = append(failures, fmt.Sprintf("%s deassign %s: %v", action.IntentID, action.Address, err))
			continue
		}
	}
	if len(failures) != 0 {
		return fmt.Errorf("SAM capture failed: %s", strings.Join(failures, "; "))
	}
	if targetOS == platform.OSLinux {
		if err := c.reconcileProxyARPSysctls(ctx, actions, nil, &state); err != nil {
			return err
		}
	}
	if targetOS == platform.OSFreeBSD {
		if err := c.reconcileProxyARPSysctls(ctx, actions, blocked, &state); err != nil {
			return err
		}
	}
	if err := c.reconcileSAMIPForwarding(ctx, targetOS, actions, state); err != nil {
		return err
	}
	if err := c.reconcileForwardPaths(ctx, &state); err != nil {
		return err
	}
	if err := c.cleanupReleasedProxyNeighbors(ctx, actions, state); err != nil {
		return err
	}
	for _, action := range actions {
		if action.Kind != "proxy-neighbor" || blocked[action.IntentID] || c.DryRun {
			continue
		}
		key := sam.ProxyNeighborKey(action.IntentID, action.Address, action.Interface)
		pending := state.pendingProxyNeighbors[key]
		if err := applier.EnsureProxyNeighbor(ctx, action.Address, action.Interface, pending.ProxyEntryOwned, func() error {
			pending.ProxyEntryOwned = targetOS == platform.OSLinux
			pending.GratuitousARP = action.GratuitousARP
			state.pendingProxyNeighbors[key] = pending
			return c.savePendingAcquisitions(state)
		}); err != nil {
			failures = append(failures, fmt.Sprintf("%s %s dev %s: %v", action.IntentID, action.Address, action.Interface, err))
			continue
		}
		if action.GratuitousARP && state.pendingProxyNeighbors[key].GratuitousARP {
			announcer := c.GARP
			if announcer == nil {
				announcer = defaultSAMGratuitousARPAnnouncer()
			}
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

// samAppliedProxyARP is a narrow ownership ledger for a sysctl that routerd
// itself moved from disabled to enabled.  It intentionally has no desired
// semantics: absence means "do not change this host setting".
type samAppliedProxyARP struct {
	Interface string `json:"interface"`
	Owned     bool   `json:"owned"`
}

const samDataplaneStatusName = dynamicconfig.MobilityNativeStatusName

// samReconcileState is one observed snapshot for a local SAM reconcile. The
// applied status is used only to retain or remove effects that routerd already
// installed; it never creates new desired capture.
type samReconcileState struct {
	previousProxyNeighbors []sam.AppliedProxyNeighbor
	heldProxyNeighbors     []sam.AppliedProxyNeighbor
	heldForwardPaths       []sam.AppliedForwardPath
	heldProxyARPIDs        map[string]bool
	previousProxyARP       map[string]samAppliedProxyARP
	nextProxyARP           map[string]samAppliedProxyARP
	pendingProxyNeighbors  map[string]sam.PendingProxyNeighbor
	pendingForwardPaths    map[string]sam.AppliedForwardPath
	pendingProxyARP        map[string]samAppliedProxyARP
	previousAcceptLocal    map[string]samAppliedProxyARP
	pendingAcceptLocal     map[string]samAppliedProxyARP
	nextAcceptLocal        map[string]samAppliedProxyARP
	forwardPaths           []sam.CaptureAction
}

func (c SAMController) reconcileState(actions []sam.CaptureAction, targetOS platform.OS) (samReconcileState, error) {
	status, err := readMobilityNativeStatus(c.Store)
	if err != nil {
		return samReconcileState{}, err
	}
	appliedNeighbors, err := sam.DecodeAppliedProxyNeighbors(status["appliedProxyNeighbors"], statusValuePresent(status, "appliedProxyNeighbors"))
	if err != nil {
		return samReconcileState{}, fmt.Errorf("decode applied SAM proxy-neighbor ledger: %w", err)
	}
	appliedPaths, err := sam.DecodeAppliedForwardPaths(status["appliedForwardPaths"], statusValuePresent(status, "appliedForwardPaths"))
	if err != nil {
		return samReconcileState{}, fmt.Errorf("decode applied SAM forward-path ledger: %w", err)
	}
	previousProxyARP, err := samAppliedProxyARPSettings(status["appliedProxyARP"], statusValuePresent(status, "appliedProxyARP"), targetOS)
	if err != nil {
		return samReconcileState{}, fmt.Errorf("decode applied SAM proxy_arp ledger: %w", err)
	}
	pendingNeighbors, err := sam.DecodePendingProxyNeighbors(status["pendingProxyNeighbors"], statusValuePresent(status, "pendingProxyNeighbors"))
	if err != nil {
		return samReconcileState{}, fmt.Errorf("decode pending SAM neighbor acquisitions: %w", err)
	}
	pendingPaths, err := sam.DecodeAppliedForwardPaths(status["pendingForwardPaths"], statusValuePresent(status, "pendingForwardPaths"))
	if err != nil {
		return samReconcileState{}, fmt.Errorf("decode pending SAM forward paths: %w", err)
	}
	pendingProxyARP, err := samAppliedProxyARPSettings(status["pendingProxyARP"], statusValuePresent(status, "pendingProxyARP"), targetOS)
	if err != nil {
		return samReconcileState{}, fmt.Errorf("decode pending SAM proxy_arp: %w", err)
	}
	previousAcceptLocal, err := samAppliedProxyARPSettings(status["appliedAcceptLocal"], statusValuePresent(status, "appliedAcceptLocal"), platform.OSLinux)
	if err != nil {
		return samReconcileState{}, fmt.Errorf("decode applied SAM accept_local: %w", err)
	}
	pendingAcceptLocal, err := samAppliedProxyARPSettings(status["pendingAcceptLocal"], statusValuePresent(status, "pendingAcceptLocal"), platform.OSLinux)
	if err != nil {
		return samReconcileState{}, fmt.Errorf("decode pending SAM accept_local: %w", err)
	}
	neighbors := map[string]sam.AppliedProxyNeighbor{}
	for _, row := range appliedNeighbors {
		neighbors[sam.ProxyNeighborKey(row.ID, row.Address, row.Interface)] = row
	}
	for key, row := range pendingNeighbors {
		if row.ProxyEntryOwned {
			neighbors[key] = row.AppliedProxyNeighbor
		}
	}
	previousNeighbors := make([]sam.AppliedProxyNeighbor, 0, len(neighbors))
	for _, row := range neighbors {
		previousNeighbors = append(previousNeighbors, row)
	}
	for iface, row := range pendingProxyARP {
		previousProxyARP[iface] = row
	}
	for iface, row := range pendingAcceptLocal {
		previousAcceptLocal[iface] = row
	}
	pendingPathMap := map[string]sam.AppliedForwardPath{}
	for _, row := range pendingPaths {
		pendingPathMap[sam.ForwardPathKey(row)] = row
	}
	held, heldProxyARPIDs, err := samHeldCaptures(c.LocalCaptureIntents)
	if err != nil {
		return samReconcileState{}, err
	}
	state := samReconcileState{
		previousProxyNeighbors: previousNeighbors,
		heldProxyARPIDs:        heldProxyARPIDs,
		previousProxyARP:       previousProxyARP,
		nextProxyARP:           map[string]samAppliedProxyARP{},
		pendingProxyNeighbors:  pendingNeighbors,
		pendingForwardPaths:    pendingPathMap,
		pendingProxyARP:        pendingProxyARP,
		previousAcceptLocal:    previousAcceptLocal,
		pendingAcceptLocal:     pendingAcceptLocal,
		nextAcceptLocal:        map[string]samAppliedProxyARP{},
	}
	for _, applied := range appliedNeighbors {
		if hold, ok := held[applied.ID]; ok && hold.matchesProxyNeighbor(applied) {
			state.heldProxyNeighbors = append(state.heldProxyNeighbors, applied)
		}
	}
	for _, applied := range appliedPaths {
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

func (h samHeldCapture) matchesProxyNeighbor(applied sam.AppliedProxyNeighbor) bool {
	return h.id == applied.ID && h.poolRef == applied.PoolRef && h.poolPrefix == applied.PoolPrefix &&
		h.address == applied.Address && h.captureInterface == applied.Interface
}

func (h samHeldCapture) matchesForwardPath(applied sam.AppliedForwardPath) bool {
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

// cleanupReleasedProxyNeighbors uses applied and pending acquisition receipts.
// Desired capture remains exclusively in LocalCaptureIntents; status is never
// consulted to recreate it.
func (c SAMController) cleanupReleasedProxyNeighbors(ctx context.Context, actions []sam.CaptureAction, state samReconcileState) error {
	desired := map[string]bool{}
	for _, action := range actions {
		if action.Kind == "proxy-neighbor" {
			desired[sam.ProxyNeighborKey(action.IntentID, action.Address, action.Interface)] = true
		}
	}
	// CaptureHold is deliberately not lowered to new actions. Retain only an
	// effect we previously recorded as applied for that same intent; it must not
	// turn an uncertain observation into a new capture.
	for _, applied := range state.heldProxyNeighbors {
		desired[sam.ProxyNeighborKey(applied.ID, applied.Address, applied.Interface)] = true
	}
	applier := c.Applier
	if applier == nil {
		applier = defaultSAMProxyNeighborApplier()
	}
	for _, applied := range state.previousProxyNeighbors {
		key := sam.ProxyNeighborKey(applied.ID, applied.Address, applied.Interface)
		if desired[key] || c.DryRun {
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
	neighborsByKey := map[string]sam.AppliedProxyNeighbor{}
	for _, action := range actions {
		if action.Kind == "proxy-neighbor" {
			neighbor := sam.AppliedProxyNeighbor{ID: action.IntentID, PoolRef: action.PoolRef, PoolPrefix: action.PoolPrefix, Address: action.Address, Interface: action.Interface}
			neighborsByKey[sam.ProxyNeighborKey(neighbor.ID, neighbor.Address, neighbor.Interface)] = neighbor
		}
	}
	for _, applied := range state.heldProxyNeighbors {
		neighborsByKey[sam.ProxyNeighborKey(applied.ID, applied.Address, applied.Interface)] = applied
	}
	neighbors := make([]sam.AppliedProxyNeighbor, 0, len(neighborsByKey))
	for _, neighbor := range neighborsByKey {
		neighbors = append(neighbors, neighbor)
	}
	sort.Slice(neighbors, func(i, j int) bool {
		return sam.ProxyNeighborKey(neighbors[i].ID, neighbors[i].Address, neighbors[i].Interface) < sam.ProxyNeighborKey(neighbors[j].ID, neighbors[j].Address, neighbors[j].Interface)
	})

	pathsByKey := map[string]sam.AppliedForwardPath{}
	for _, path := range state.forwardPaths {
		applied := sam.AppliedForwardPath{ID: path.IntentID, PoolRef: path.PoolRef, PoolPrefix: path.PoolPrefix, Kind: path.Kind, Address: path.Address, Interface: path.Interface, PeerInterface: path.PeerInterface}
		pathsByKey[sam.ForwardPathKey(applied)] = applied
	}
	paths := make([]sam.AppliedForwardPath, 0, len(pathsByKey))
	for _, path := range pathsByKey {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool { return sam.ForwardPathKey(paths[i]) < sam.ForwardPathKey(paths[j]) })
	proxyARP := make([]samAppliedProxyARP, 0, len(state.nextProxyARP))
	for _, setting := range state.nextProxyARP {
		proxyARP = append(proxyARP, setting)
	}
	sort.Slice(proxyARP, func(i, j int) bool { return proxyARP[i].Interface < proxyARP[j].Interface })
	return merger.MergeObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName, map[string]any{
		"appliedProxyNeighbors":                 neighbors,
		"appliedForwardPaths":                   paths,
		"appliedProxyARP":                       proxyARP,
		"appliedAcceptLocal":                    samProxyARPSettingRows(state.nextAcceptLocal),
		"pendingProxyNeighbors":                 []sam.PendingProxyNeighbor{},
		"pendingForwardPaths":                   []sam.AppliedForwardPath{},
		"pendingProxyARP":                       []samAppliedProxyARP{},
		"pendingAcceptLocal":                    []samAppliedProxyARP{},
		dynamicconfig.MobilitySAMWithdrawalAcks: c.samWithdrawalAcks(neighbors, paths),
	})
}

func samAppliedProxyARPSettings(value any, present bool, targetOS platform.OS) (map[string]samAppliedProxyARP, error) {
	if !present {
		return map[string]samAppliedProxyARP{}, nil
	}
	var decoded []samAppliedProxyARP
	if !decodeAppliedDataplaneStatus(value, &decoded) || decoded == nil {
		return nil, fmt.Errorf("must be a non-null JSON array")
	}
	settings := make(map[string]samAppliedProxyARP, len(decoded))
	for _, setting := range decoded {
		if !setting.Owned {
			return nil, fmt.Errorf("proxy_arp ledger may retain only routerd-owned settings")
		}
		if targetOS == platform.OSFreeBSD {
			if setting.Interface != "" {
				return nil, fmt.Errorf("FreeBSD proxyall ledger interface must be empty")
			}
		} else if err := sam.ValidateCaptureInterface(setting.Interface); err != nil {
			return nil, fmt.Errorf("proxy_arp ledger interface: %w", err)
		}
		if previous, exists := settings[setting.Interface]; exists && previous != setting {
			return nil, fmt.Errorf("conflicting proxy_arp ledger entries for interface %q", setting.Interface)
		}
		settings[setting.Interface] = setting
	}
	return settings, nil
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
func decodeAppliedDataplaneStatus(value any, target any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

func reconciledSAMForwardPaths(actions []sam.CaptureAction, held []sam.AppliedForwardPath) []sam.CaptureAction {
	byKey := map[string]sam.CaptureAction{}
	for _, action := range actions {
		if action.Kind != "forward-path" && action.Kind != "forward-local-path" {
			continue
		}
		path := sam.AppliedForwardPath{ID: action.IntentID, PoolRef: action.PoolRef, PoolPrefix: action.PoolPrefix, Kind: action.Kind, Address: action.Address, Interface: action.Interface, PeerInterface: action.PeerInterface}
		byKey[sam.ForwardPathKey(path)] = action
	}
	for _, applied := range held {
		key := sam.ForwardPathKey(applied)
		if _, exists := byKey[key]; !exists {
			byKey[key] = sam.CaptureAction{Kind: applied.Kind, IntentID: applied.ID, PoolRef: applied.PoolRef, PoolPrefix: applied.PoolPrefix, Address: applied.Address, Interface: applied.Interface, PeerInterface: applied.PeerInterface}
		}
	}
	paths := make([]sam.CaptureAction, 0, len(byKey))
	for _, path := range byKey {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		left := sam.AppliedForwardPath{ID: paths[i].IntentID, PoolRef: paths[i].PoolRef, PoolPrefix: paths[i].PoolPrefix, Kind: paths[i].Kind, Address: paths[i].Address, Interface: paths[i].Interface, PeerInterface: paths[i].PeerInterface}
		right := sam.AppliedForwardPath{ID: paths[j].IntentID, PoolRef: paths[j].PoolRef, PoolPrefix: paths[j].PoolPrefix, Kind: paths[j].Kind, Address: paths[j].Address, Interface: paths[j].Interface, PeerInterface: paths[j].PeerInterface}
		return sam.ForwardPathKey(left) < sam.ForwardPathKey(right)
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
	applier := c.Applier
	if applier == nil {
		applier = defaultSAMProxyNeighborApplier()
	}
	if err := applier.SetIPForwarding(ctx, true); err != nil {
		return fmt.Errorf("enable SAM IP forwarding: %w", err)
	}
	return nil
}

func (c SAMController) reconcileForwardPaths(ctx context.Context, state *samReconcileState) error {
	if c.DryRun {
		return nil
	}
	applier := c.Applier
	if applier == nil {
		applier = defaultSAMProxyNeighborApplier()
	}
	setAcceptLocal := func(ctx context.Context, iface string, enabled bool) error {
		_, owned := state.previousAcceptLocal[iface]
		if _, pending := state.pendingAcceptLocal[iface]; pending {
			owned = true
		}
		result, err := applier.SetAcceptLocal(ctx, iface, enabled, owned, func() error {
			state.pendingAcceptLocal[iface] = samAppliedProxyARP{Interface: iface, Owned: true}
			return c.savePendingAcquisitions(*state)
		})
		if err != nil {
			return err
		}
		if enabled {
			if !owned && !result.changedByRouterd {
				return fmt.Errorf("accept_local %s enabled without ownership proof", iface)
			}
			state.nextAcceptLocal[iface] = samAppliedProxyARP{Interface: iface, Owned: true}
		} else {
			delete(state.nextAcceptLocal, iface)
		}
		return nil
	}
	if err := applier.ReconcileForwardPaths(ctx, state.forwardPaths, setAcceptLocal); err != nil {
		return err
	}
	desired := map[string]bool{}
	for _, path := range state.forwardPaths {
		desired[path.Interface] = true
		desired[path.PeerInterface] = true
	}
	for iface := range state.previousAcceptLocal {
		if !desired[iface] {
			if err := setAcceptLocal(ctx, iface, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c SAMController) reconcileProxyARPSysctls(ctx context.Context, actions []sam.CaptureAction, blocked map[string]bool, state *samReconcileState) error {
	if c.DryRun {
		return nil
	}
	targetOS := c.OS
	if targetOS == "" {
		targetOS = platform.CurrentOS()
	}
	applier := c.Applier
	if applier == nil {
		applier = defaultSAMProxyNeighborApplier()
	}
	desired := map[string]bool{}
	if targetOS == platform.OSFreeBSD {
		if freeBSDProxyARPEnabled(actions, blocked) || len(state.heldProxyNeighbors) != 0 {
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
		_, previouslyOwned := state.previousProxyARP[iface]
		result, err := applier.SetProxyARP(ctx, iface, desired[iface], previouslyOwned, func() error {
			state.pendingProxyARP[iface] = samAppliedProxyARP{Interface: iface, Owned: true}
			return c.savePendingAcquisitions(*state)
		})
		if err != nil {
			return fmt.Errorf("set SAM proxy_arp %q=%t: %w", iface, desired[iface], err)
		}
		if desired[iface] {
			if !previouslyOwned && !result.changedByRouterd {
				return fmt.Errorf("set SAM proxy_arp %q enabled without ownership proof", iface)
			}
			state.nextProxyARP[iface] = samAppliedProxyARP{Interface: iface, Owned: true}
		}
	}
	return nil
}

func freeBSDProxyARPEnabled(actions []sam.CaptureAction, blocked map[string]bool) bool {
	for _, action := range actions {
		if action.Kind == "proxy-neighbor" && !blocked[action.IntentID] {
			return true
		}
	}
	return false
}
