// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/providerinventory"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type discoverySelfInventory struct {
	NICRef            string
	SubnetRef         string
	ResourceRef       string
	PrivateIPs        []string
	CapturedAddresses []string
	PrimaryObserved   bool
	ForwardingEnabled *bool
}

func resolvedDiscoverySelfInventory(pool NormalizedMobilityPool, pluginSelf *providerinventory.PrivateIPSelf) discoverySelfInventory {
	self := pool.Self
	discovery := self.OwnershipDiscovery
	out := discoverySelfInventory{}
	if pluginSelf != nil {
		out.NICRef = strings.TrimSpace(pluginSelf.NICRef)
		out.SubnetRef = strings.TrimSpace(pluginSelf.SubnetRef)
		out.ResourceRef = strings.TrimSpace(pluginSelf.ResourceRef)
		out.PrivateIPs = cleanStrings(pluginSelf.PrivateIPs)
		out.CapturedAddresses = cleanStrings(pluginSelf.CapturedAddresses)
		out.ForwardingEnabled = pluginSelf.ForwardingEnabled
	}
	if explicit := strings.TrimSpace(self.Capture.NICRef); explicit != "" {
		out.NICRef = explicit
	}
	if explicit := strings.TrimSpace(discovery.SubnetRef); explicit != "" {
		out.SubnetRef = explicit
	}
	return out
}

// filterProviderInventoryRecords applies the shared provider/subnet filter to
// both inventory and candidate observations. A valid pool prefix additionally
// constrains inventory records; candidates deliberately remain unfiltered by
// address here so the caller can expire previously observed out-of-scope
// addresses.
func filterProviderInventoryRecords(records []providerinventory.PrivateIPRecord, providerRef, subnetRef string, poolPrefix netip.Prefix) []providerinventory.PrivateIPRecord {
	providerRef = strings.TrimSpace(providerRef)
	subnetRef = strings.TrimSpace(subnetRef)
	var out []providerinventory.PrivateIPRecord
	for _, rec := range records {
		if poolPrefix.IsValid() {
			if _, ok := normalizeLeaseAddress(rec.Address, poolPrefix); !ok {
				continue
			}
		}
		if recProvider := strings.TrimSpace(rec.ProviderRef); recProvider != "" && providerRef != "" && recProvider != providerRef {
			continue
		}
		if recSubnet := strings.TrimSpace(rec.SubnetRef); recSubnet != "" && subnetRef != "" && recSubnet != subnetRef {
			continue
		}
		out = append(out, rec)
	}
	return out
}

func scopedOutProviderCandidateAddresses(raw, scoped []providerinventory.PrivateIPRecord, poolPrefix netip.Prefix) map[string]bool {
	out := map[string]bool{}
	for _, rec := range raw {
		address, ok := normalizeLeaseAddress(rec.Address, poolPrefix)
		if !ok {
			continue
		}
		out[address] = true
	}
	for _, rec := range scoped {
		address, ok := normalizeLeaseAddress(rec.Address, poolPrefix)
		if !ok {
			continue
		}
		delete(out, address)
	}
	return out
}

func scopedDiscoverySelfInventory(self discoverySelfInventory, localInventory []providerinventory.PrivateIPRecord, poolPrefix netip.Prefix) discoverySelfInventory {
	selfNIC := strings.TrimSpace(self.NICRef)
	selfResource := strings.TrimSpace(self.ResourceRef)
	if selfNIC == "" && selfResource == "" {
		self.PrivateIPs = normalizedDiscoveryAddresses(self.PrivateIPs, poolPrefix)
		self.CapturedAddresses = normalizedDiscoveryAddresses(self.CapturedAddresses, poolPrefix)
		return self
	}
	var privateIPs []string
	var captured []string
	matched := false
	primaryObserved := false
	for _, rec := range localInventory {
		recNIC := strings.TrimSpace(rec.NICRef)
		recResource := strings.TrimSpace(rec.ResourceRef)
		if (selfNIC == "" || recNIC != selfNIC) && (selfResource == "" || recResource != selfResource) {
			continue
		}
		address, ok := normalizeLeaseAddress(rec.Address, poolPrefix)
		if !ok {
			continue
		}
		matched = true
		if self.ResourceRef == "" && recResource != "" {
			self.ResourceRef = recResource
			selfResource = recResource
		}
		if rec.Primary {
			primaryObserved = true
			privateIPs = append(privateIPs, address)
		} else {
			captured = append(captured, address)
		}
	}
	if matched {
		if primaryObserved {
			self.PrivateIPs = cleanStrings(privateIPs)
			self.PrimaryObserved = true
		} else {
			self.PrivateIPs = normalizedDiscoveryAddresses(self.PrivateIPs, poolPrefix)
		}
		self.CapturedAddresses = cleanStrings(append(captured, normalizedDiscoveryAddresses(self.CapturedAddresses, poolPrefix)...))
		return self
	}
	self.PrivateIPs = normalizedDiscoveryAddresses(self.PrivateIPs, poolPrefix)
	self.CapturedAddresses = normalizedDiscoveryAddresses(self.CapturedAddresses, poolPrefix)
	return self
}

func trustedDiscoverySelfSubnetRef(self discoverySelfInventory, localInventory []providerinventory.PrivateIPRecord) string {
	selfNIC := strings.TrimSpace(self.NICRef)
	selfSubnet := strings.TrimSpace(self.SubnetRef)
	if selfNIC == "" || selfSubnet == "" {
		return ""
	}
	for _, rec := range localInventory {
		if strings.TrimSpace(rec.NICRef) == selfNIC && strings.TrimSpace(rec.SubnetRef) == selfSubnet {
			return selfSubnet
		}
	}
	return ""
}

func normalizedDiscoveryAddresses(values []string, poolPrefix netip.Prefix) []string {
	var out []string
	for _, raw := range values {
		address, ok := normalizeLeaseAddress(raw, poolPrefix)
		if ok {
			out = append(out, address)
		}
	}
	return cleanStrings(out)
}

// objectStatusStore is the common persistence subset shared by the planner
// and discovery shells. The functional core never touches it: this is solely
// the compatibility boundary for status projection.
type objectStatusStore interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

// saveMergedObjectStatus preserves fields produced by sibling controllers
// when a store does not provide an atomic merge operation. Callers choose
// whether an error is actionable; planner propagation and discovery's
// best-effort status reporting intentionally retain their existing behavior.
func saveMergedObjectStatus(store objectStatusStore, apiVersion, kind, name string, updates map[string]any) error {
	if merger, ok := store.(objectStatusMerger); ok {
		return merger.MergeObjectStatus(apiVersion, kind, name, updates)
	}
	status := map[string]any{}
	for key, value := range store.ObjectStatus(apiVersion, kind, name) {
		status[key] = value
	}
	for key, value := range updates {
		status[key] = value
	}
	return store.SaveObjectStatus(apiVersion, kind, name, status)
}

// scanDue is intentionally a pure scheduling decision. The discovery shell
// has already assembled the typed snapshot from controller continuations and
// durable facts; reopening either persistence boundary here would let the
// controller reinterpret state a second time.
func scanDue(previous PreviousPoolState, ownership OwnershipFacts, interval time.Duration, now time.Time, requireSelfNICRef bool, placement PlacementDecision) bool {
	if discoveryPlacementChanged(ownership.Placement, placement) {
		return true
	}
	if requireSelfNICRef {
		if strings.TrimSpace(ownership.SelfNICRef) == "" {
			return true
		}
	}
	if previous.Placement.SeizeHoldDownActive && !previous.Placement.SeizeHoldDownUntil.IsZero() && !previous.Placement.SeizeHoldDownUntil.After(now) {
		return true
	}
	if ownership.DiscoveryLastScanAt.IsZero() {
		return true
	}
	return !ownership.DiscoveryLastScanAt.Add(interval).After(now)
}

func discoveryPlacementChanged(previous discoveryPlacementObservation, placement PlacementDecision) bool {
	if !placement.LivenessObserved && !previous.LivenessObserved {
		return false
	}
	return previous.LivenessObserved != placement.LivenessObserved ||
		previous.ActiveNode != placement.ActiveNode ||
		previous.Active != placement.Active ||
		previous.Seize != placement.Seize ||
		previous.SelfMarkerPresent != placement.SelfMarkerPresent ||
		previous.ActiveMarkerPresent != placement.ActiveMarkerPresent ||
		previous.SelfMarker != placement.SelfMarker ||
		previous.ActiveMarker != placement.ActiveMarker
}

func (c DiscoveryController) resolveInventoryPlugin(provider string, discovery api.MobilityOwnershipDiscovery) (api.PluginSpec, string, error) {
	pluginRef := strings.TrimSpace(discovery.PluginRef)
	var candidates []api.Resource
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.PluginAPIVersion || res.Kind != "Plugin" {
			continue
		}
		if pluginRef != "" && res.Metadata.Name != pluginRef {
			continue
		}
		spec, err := res.PluginSpec()
		if err != nil {
			return api.PluginSpec{}, "", err
		}
		if !pluginHasCapability(spec.Capabilities, providerinventory.CapabilityObserveProviderPrivateIPs) {
			continue
		}
		if pluginRef != "" {
			return spec, res.Metadata.Name, nil
		}
		candidates = append(candidates, res)
	}
	if pluginRef != "" {
		return api.PluginSpec{}, "", fmt.Errorf("Plugin/%s with capability %q not found", pluginRef, providerinventory.CapabilityObserveProviderPrivateIPs)
	}
	wantName := strings.TrimSpace(provider) + "-inventory"
	for _, res := range candidates {
		if res.Metadata.Name == wantName {
			spec, err := res.PluginSpec()
			return spec, res.Metadata.Name, err
		}
	}
	if len(candidates) == 1 {
		spec, err := candidates[0].PluginSpec()
		return spec, candidates[0].Metadata.Name, err
	}
	if len(candidates) == 0 {
		return api.PluginSpec{}, "", fmt.Errorf("no Plugin with capability %q found for provider %q", providerinventory.CapabilityObserveProviderPrivateIPs, provider)
	}
	return api.PluginSpec{}, "", fmt.Errorf("ambiguous provider inventory plugin for provider %q: %d candidates found, none named %q", provider, len(candidates), wantName)
}

func (c DiscoveryController) runner() providerinventory.Runner {
	if c.Runner != nil {
		return c.Runner
	}
	return providerinventory.RunInventory
}

func (c DiscoveryController) saveDiscoveryStatus(poolName string, updates map[string]any) {
	if c.Store == nil {
		return
	}
	_ = saveMergedObjectStatus(c.Store, api.MobilityAPIVersion, "MobilityPool", poolName, updates)
}

func (c DiscoveryController) saveDiscoveryError(poolName string, err error) {
	c.saveDiscoveryStatus(poolName, map[string]any{
		"discoveryPhase":  "Degraded",
		"discoveryReason": err.Error(),
	})
}

func discoveryScanInterval(discovery api.MobilityOwnershipDiscovery) time.Duration {
	interval := durationDefault(discovery.ScanInterval, defaultDiscoveryScanInterval)
	if interval < minDiscoveryScanInterval {
		return minDiscoveryScanInterval
	}
	return interval
}

func discoveryLeaseTTL(discovery api.MobilityOwnershipDiscovery) time.Duration {
	return durationDefault(discovery.LeaseTTL, DefaultLeaseTTL)
}

func discoveryScopeAllowsAddress(scope api.MobilityOwnershipDiscoveryScope, address string) bool {
	addr, err := netip.ParsePrefix(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	if len(scope.IncludeAddresses) > 0 {
		matched := false
		for _, raw := range scope.IncludeAddresses {
			prefix, ok := parseDiscoveryScopePrefix(raw)
			if ok && prefix.Contains(addr.Addr()) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for _, raw := range scope.ExcludeAddresses {
		prefix, ok := parseDiscoveryScopePrefix(raw)
		if ok && prefix.Contains(addr.Addr()) {
			return false
		}
	}
	return true
}

func parseDiscoveryScopePrefix(raw string) (netip.Prefix, bool) {
	prefix, ok := parseIPv4AddressOrPrefix(raw)
	if !ok {
		return netip.Prefix{}, false
	}
	return prefix.Masked(), true
}

// discoveryRouterNICRefs returns only the local controller's configured NIC
// references. Remote capture overlays are rejected during normalization, so
// carrying every member's provider configuration here would recreate the
// pre-normalization interpretation that discovery is meant to avoid. Provider
// records marked router-nic are still rejected independently.
func discoveryRouterNICRefs(pool NormalizedMobilityPool) map[string]bool {
	out := map[string]bool{}
	self := pool.Self
	nic := strings.TrimSpace(self.Capture.NICRef)
	if self.Capture.Type == "provider-secondary-ip" && nic != "" {
		out[nic] = true
	}
	return out
}

// discoverySelfPrivateIPSet uses the already-normalized self inventory. The
// inventory projection is normalized exactly once by scopedDiscoverySelfInventory.
func discoverySelfPrivateIPSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, address := range values {
		out[address] = true
	}
	return out
}

func discoveryCurrentTrapAddresses(history ProviderActionHistory, pool NormalizedMobilityPool, providerRef string, poolPrefix netip.Prefix) map[string]bool {
	out := map[string]bool{}
	captureRef := providerCaptureRefFromCapture(pool.Self.Capture)
	add := func(provider string, target, params map[string]string, requireCaptureRef bool) {
		capture := ""
		if requireCaptureRef {
			capture = captureRef
		}
		if !discoveryTrapPlanMatchesSelf(provider, target, params, pool.SelfNode, providerRef, capture) {
			return
		}
		if address, ok := normalizeLeaseAddress(target["address"], poolPrefix); ok {
			out[address] = true
		}
	}
	for _, plan := range history.previousCaptureAssigns {
		add(plan.ProviderRef, plan.Target, plan.Parameters, false)
	}
	// The journal is already decoded into the history's typed records. Do not
	// recreate an ActionPlan merely for Discovery to immediately reinterpret it.
	for _, action := range history.latestByKey {
		if isProviderCaptureAssignAction(action.action) && providerActionStatusPending(action.status) {
			add(action.providerRef, action.target, action.parameters, true)
		}
	}
	return out
}

func discoveryTrapPlanMatchesSelf(actionProviderRef string, target, params map[string]string, selfNode, providerRef, captureRef string) bool {
	providerRef = strings.TrimSpace(providerRef)
	captureRef = strings.TrimSpace(captureRef)
	actionProviderRef = firstNonEmpty(actionProviderRef, target["providerRef"])
	if providerRef != "" && strings.TrimSpace(actionProviderRef) != "" && strings.TrimSpace(actionProviderRef) != providerRef {
		return false
	}
	if captureRef != "" && providerCaptureRefFromTarget(target) != captureRef {
		return false
	}
	holder := strings.TrimSpace(params[captureParamHolder])
	return holder == "" || holder == strings.TrimSpace(selfNode)
}

func stoppedInstancePolicy(discovery api.MobilityOwnershipDiscovery) string {
	p := strings.TrimSpace(discovery.StoppedInstancePolicy)
	if p == "release" {
		return "release"
	}
	return "hold"
}

func discoverySelectorMatches(selector api.MobilityOwnershipDiscoverySelector, tags map[string]string) bool {
	for key, want := range selector.Tags {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if strings.TrimSpace(tags[key]) != strings.TrimSpace(want) {
			return false
		}
	}
	return true
}

func onPremDiscoveryObservedEvent(poolName, group, nodeRef, address string, observation onPremObservation, now time.Time, ttl time.Duration) routerstate.EventRecord {
	observedAt := now.UTC()
	payload := onPremDiscoveryPayload(poolName, address, observation)
	dedupeKey := onPremDiscoveryDedupeKey(poolName, nodeRef, address, observation.SourceType)
	return discoveryEvent(onPremDiscoveryEventID(poolName, nodeRef, address, observation.SourceType, observedAt), group, nodeRef, address, dedupeKey, ObservedEventType, payload, observedAt, ttl)
}

func onPremDiscoveryExpiredEvent(poolName, group, nodeRef, address string, observation onPremObservation, now time.Time, ttl time.Duration) routerstate.EventRecord {
	observedAt := now.UTC()
	payload := onPremDiscoveryPayload(poolName, address, observation)
	if ttl <= 0 {
		ttl = DefaultLeaseTTL
	}
	dedupeKey := onPremDiscoveryDedupeKey(poolName, nodeRef, address, observation.SourceType)
	return discoveryEvent(dedupeKey+":expired:"+strconv.FormatInt(observedAt.UnixNano(), 10), group, nodeRef, address, dedupeKey, ExpiredEventType, payload, observedAt, ttl)
}

// discoveryEvent owns the common durable-event envelope. Source-specific
// constructors above retain payload construction and key selection, which are
// the safety identities of the two discovery protocols.
func discoveryEvent(id, group, nodeRef, address, dedupeKey, eventType string, payload map[string]string, observedAt time.Time, ttl time.Duration) routerstate.EventRecord {
	observedAt = observedAt.UTC()
	return routerstate.EventRecord{
		ID:         id,
		Group:      strings.TrimSpace(group),
		SourceNode: strings.TrimSpace(nodeRef),
		Type:       eventType,
		Subject:    address,
		DedupeKey:  dedupeKey,
		Payload:    payload,
		ObservedAt: observedAt,
		ExpiresAt:  observedAt.Add(ttl),
		RecordedAt: observedAt,
	}
}

func onPremDiscoveryPayload(poolName, address string, observation onPremObservation) map[string]string {
	payload := map[string]string{
		"address":    address,
		"pool":       strings.TrimSpace(poolName),
		"source":     onPremDiscoverySource,
		"sourceType": strings.TrimSpace(observation.SourceType),
	}
	if value := strings.TrimSpace(observation.MAC); value != "" {
		payload["mac"] = value
	}
	if value := strings.TrimSpace(observation.Interface); value != "" {
		payload["interface"] = value
	}
	if value := strings.TrimSpace(observation.Network); value != "" {
		payload["network"] = value
	}
	if value := strings.TrimSpace(observation.Bridge); value != "" {
		payload["bridge"] = value
	}
	return payload
}

func onPremDiscoveryEventID(poolName, nodeRef, address, sourceType string, observedAt time.Time) string {
	return onPremDiscoveryDedupeKey(poolName, nodeRef, address, sourceType) + ":" + strconv.FormatInt(observedAt.UTC().UnixNano(), 10)
}

func onPremDiscoveryDedupeKey(poolName, nodeRef, address, sourceType string) string {
	return strings.Join([]string{"mobility", onPremDiscoverySource, strings.TrimSpace(sourceType), strings.TrimSpace(poolName), strings.TrimSpace(nodeRef), strings.ReplaceAll(strings.TrimSpace(address), "/", "_")}, ":")
}

func pluginHasCapability(capabilities []string, want string) bool {
	for _, capability := range capabilities {
		if strings.TrimSpace(capability) == want {
			return true
		}
	}
	return false
}

func sortedPrivateIPs(records []providerinventory.PrivateIPRecord) []providerinventory.PrivateIPRecord {
	out := append([]providerinventory.PrivateIPRecord(nil), records...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Address == out[j].Address {
			return out[i].NICRef < out[j].NICRef
		}
		return out[i].Address < out[j].Address
	})
	return out
}
