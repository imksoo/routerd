// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/mapsort"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/providerinventory"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	providerDiscoveryRuntimeFactType = "runtime"
	onPremDiscoveryArmedFactType     = "armed"
)

// providerDiscoveryRuntimeFact is the durable, typed result of one successful
// provider inventory scan.  It deliberately travels as one federation event:
// accepting a partial scan would otherwise mix a newer self observation with
// an older inventory/ownership set.  The event payload is the persistence
// boundary; planning only receives the decoded typed form below.
type providerDiscoveryRuntimeFact struct {
	Self      discoverySelfInventory         `json:"self"`
	Addresses []providerDiscoveryAddressFact `json:"addresses"`
	Placement discoveryPlacementObservation  `json:"placement"`
}

// providerDiscoveryAddressFact is the only durable provider ownership record
// for one address. A provider scan produces one atomic runtime fact, while
// each address retains its own lease and missing-hold clocks so a partial scan
// cannot withdraw a confirmed home prematurely.
type providerDiscoveryAddressFact struct {
	Address          string    `json:"address"`
	Provider         string    `json:"provider"`
	ProviderRef      string    `json:"providerRef"`
	SubnetRef        string    `json:"subnetRef"`
	NICRef           string    `json:"nicRef"`
	ResourceRef      string    `json:"resourceRef,omitempty"`
	ResourceType     string    `json:"resourceType,omitempty"`
	InstanceState    string    `json:"instanceState,omitempty"`
	ObservedAt       time.Time `json:"observedAt"`
	ExpiresAt        time.Time `json:"expiresAt"`
	MissingHoldUntil time.Time `json:"missingHoldUntil,omitempty"`
}

func providerDiscoveryAddressFactFromRecord(address, provider, providerRef string, record providerinventory.PrivateIPRecord, observedAt time.Time, ttl time.Duration) providerDiscoveryAddressFact {
	if ttl <= 0 {
		ttl = DefaultLeaseTTL
	}
	observedAt = observedAt.UTC()
	return providerDiscoveryAddressFact{
		Address:       strings.TrimSpace(address),
		Provider:      strings.TrimSpace(provider),
		ProviderRef:   strings.TrimSpace(providerRef),
		SubnetRef:     strings.TrimSpace(record.SubnetRef),
		NICRef:        strings.TrimSpace(record.NICRef),
		ResourceRef:   strings.TrimSpace(record.ResourceRef),
		ResourceType:  strings.TrimSpace(record.ResourceType),
		InstanceState: strings.TrimSpace(record.InstanceState),
		ObservedAt:    observedAt,
		ExpiresAt:     observedAt.Add(ttl),
	}
}

func providerDiscoveryAddressFactActive(fact providerDiscoveryAddressFact, now time.Time) bool {
	if strings.TrimSpace(fact.Address) == "" || fact.ExpiresAt.IsZero() || !fact.ExpiresAt.After(now) {
		return false
	}
	return fact.MissingHoldUntil.IsZero() || fact.MissingHoldUntil.After(now)
}

func sortedProviderDiscoveryAddressFacts(facts []providerDiscoveryAddressFact) []providerDiscoveryAddressFact {
	out := append([]providerDiscoveryAddressFact(nil), facts...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Address == out[j].Address {
			return out[i].ObservedAt.After(out[j].ObservedAt)
		}
		return out[i].Address < out[j].Address
	})
	return out
}

func mergeProviderDiscoveryAddressFacts(previous []providerDiscoveryAddressFact, observed map[string]providerDiscoveryAddressFact, invalid map[string]bool, now time.Time, missingHold time.Duration) []providerDiscoveryAddressFact {
	merged := make(map[string]providerDiscoveryAddressFact, len(previous)+len(observed))
	for address, fact := range observed {
		address = strings.TrimSpace(address)
		if address != "" && providerDiscoveryAddressFactActive(fact, now) {
			merged[address] = fact
		}
	}
	for _, fact := range previous {
		address := strings.TrimSpace(fact.Address)
		if address == "" || invalid[address] || merged[address].Address != "" || !providerDiscoveryAddressFactActive(fact, now) {
			continue
		}
		if fact.MissingHoldUntil.IsZero() {
			fact.MissingHoldUntil = fact.ObservedAt.UTC().Add(missingHold)
		}
		if missingHold <= 0 || !fact.MissingHoldUntil.After(now) {
			continue
		}
		merged[address] = fact
	}
	out := make([]providerDiscoveryAddressFact, 0, len(merged))
	for _, fact := range merged {
		out = append(out, fact)
	}
	return sortedProviderDiscoveryAddressFacts(out)
}

func providerDiscoveryRuntimeEvent(pool NormalizedMobilityPool, self discoverySelfInventory, addresses []providerDiscoveryAddressFact, placement PlacementDecision, now time.Time, ttl time.Duration) (routerstate.EventRecord, error) {
	if ttl <= 0 {
		ttl = DefaultLeaseTTL
	}
	observedAt := now.UTC()
	fact := providerDiscoveryRuntimeFact{
		Self:      self,
		Addresses: sortedProviderDiscoveryAddressFacts(addresses),
		Placement: discoveryPlacementFromDecision(placement),
	}
	payload, err := json.Marshal(fact)
	if err != nil {
		return routerstate.EventRecord{}, err
	}
	return routerstate.EventRecord{
		ID:         providerDiscoveryRuntimeEventID(pool.Name, pool.SelfNode, observedAt),
		Group:      strings.TrimSpace(pool.Spec.GroupRef),
		SourceNode: strings.TrimSpace(pool.SelfNode),
		Type:       ObservedEventType,
		Subject:    "MobilityPool/" + strings.TrimSpace(pool.Name) + "/provider-runtime",
		DedupeKey:  providerDiscoveryRuntimeDedupeKey(pool.Name, pool.SelfNode),
		Payload: map[string]string{
			"pool":       strings.TrimSpace(pool.Name),
			"source":     providerDiscoverySource,
			"sourceType": providerDiscoveryRuntimeFactType,
			"snapshot":   string(payload),
		},
		ObservedAt: observedAt,
		ExpiresAt:  observedAt.Add(ttl),
		RecordedAt: observedAt,
	}, nil
}

func discoveryPlacementFromDecision(placement PlacementDecision) discoveryPlacementObservation {
	return discoveryPlacementObservation{
		LivenessObserved:    placement.LivenessObserved,
		ActiveNode:          placement.ActiveNode,
		Active:              placement.Active,
		Seize:               placement.Seize,
		SelfMarkerPresent:   placement.SelfMarkerPresent,
		ActiveMarkerPresent: placement.ActiveMarkerPresent,
		SelfMarker:          placement.SelfMarker,
		ActiveMarker:        placement.ActiveMarker,
	}
}

func providerDiscoveryRuntimeObservation(pool NormalizedMobilityPool, records []providerDiscoveryRuntimeRecord, prefix netip.Prefix) poolDiscoveryObservation {
	observation := poolDiscoveryObservation{
		providerRuntime: append([]providerDiscoveryRuntimeRecord(nil), records...),
		SelfPrivateIPs:  map[string]bool{},
		SelfCapturedIPs: map[string]bool{},
	}
	record, found := observation.providerRuntimeForNode(pool.SelfNode)
	if !found {
		return observation
	}
	fact := record.RuntimeFact
	for _, raw := range fact.Self.PrivateIPs {
		if address, ok := normalizeLeaseAddress(raw, prefix); ok {
			observation.SelfPrivateIPs[address] = true
		}
	}
	for _, raw := range fact.Self.CapturedAddresses {
		if address, ok := normalizeLeaseAddress(raw, prefix); ok {
			observation.SelfCapturedIPs[address] = true
		}
	}
	observedAt := record.ObservedAt.UTC()
	observation.SelfInventoryKnown = true
	observation.SelfPrimaryKnown = fact.Self.PrimaryObserved
	observation.DiscoveryLastScanAt = observedAt
	if fact.Self.ForwardingEnabled != nil {
		observation.ForwardingKnown = true
		observation.ForwardingOn = *fact.Self.ForwardingEnabled
	}
	observation.SelfNICRef = strings.TrimSpace(fact.Self.NICRef)
	observation.SelfSubnetRef = strings.TrimSpace(fact.Self.SubnetRef)
	observation.Placement = fact.Placement
	return observation
}

// poolPreviousStateAndOwnershipFacts preserves the few controller-owned status
// continuations (placement hold-down, stale-capture clock, and transitions),
// then decodes the current discovery/ownership input from durable federation
// facts. This is the one explicit boundary between persisted status
// presentation and the typed planning snapshot.
func poolPreviousStateAndOwnershipFacts(status map[string]any, pool NormalizedMobilityPool, events []routerstate.EventRecord, prefix netip.Prefix, onPremIntentReady bool, now time.Time) (PreviousPoolState, OwnershipFacts) {
	previous := decodePoolPreviousState(status)
	ownership := providerDiscoveryRuntimeObservation(pool, providerDiscoveryRuntimeRecords(pool, events, now), prefix)
	if strings.TrimSpace(pool.Self.OwnershipDiscovery.Mode) == "onprem-l2" {
		previous.OnPremDiscovery = onPremDiscoveryStateFromFacts(pool, prefix, events, onPremIntentReady, now)
	}
	return previous, ownership
}

func decodeProviderDiscoveryRuntimeEvent(pool NormalizedMobilityPool, event routerstate.EventRecord, now time.Time) (providerDiscoveryRuntimeFact, bool) {
	if event.Group != pool.Spec.GroupRef || event.Type != ObservedEventType {
		return providerDiscoveryRuntimeFact{}, false
	}
	if strings.TrimSpace(event.Payload["source"]) != providerDiscoverySource || strings.TrimSpace(event.Payload["sourceType"]) != providerDiscoveryRuntimeFactType || strings.TrimSpace(event.Payload["pool"]) != strings.TrimSpace(pool.Name) {
		return providerDiscoveryRuntimeFact{}, false
	}
	if !event.ExpiresAt.IsZero() && !now.Before(event.ExpiresAt) {
		return providerDiscoveryRuntimeFact{}, false
	}
	var fact providerDiscoveryRuntimeFact
	if json.Unmarshal([]byte(event.Payload["snapshot"]), &fact) != nil {
		return providerDiscoveryRuntimeFact{}, false
	}
	return fact, true
}

type providerDiscoveryRuntimeRecord struct {
	NodeRef     string
	ObservedAt  time.Time
	RuntimeFact providerDiscoveryRuntimeFact
}

func (observation poolDiscoveryObservation) providerRuntimeForNode(nodeRef string) (providerDiscoveryRuntimeRecord, bool) {
	for _, record := range observation.providerRuntime {
		if strings.TrimSpace(record.NodeRef) == strings.TrimSpace(nodeRef) {
			return record, true
		}
	}
	return providerDiscoveryRuntimeRecord{}, false
}

func providerDiscoveryRuntimeRecords(pool NormalizedMobilityPool, events []routerstate.EventRecord, now time.Time) []providerDiscoveryRuntimeRecord {
	latest := map[string]providerDiscoveryRuntimeRecord{}
	for _, event := range events {
		fact, ok := decodeProviderDiscoveryRuntimeEvent(pool, event, now)
		if !ok {
			continue
		}
		nodeRef := strings.TrimSpace(event.SourceNode)
		if nodeRef == "" {
			continue
		}
		candidate := eventWithFallbackObservedAt(event, now)
		record := providerDiscoveryRuntimeRecord{NodeRef: nodeRef, ObservedAt: candidate.ObservedAt.UTC(), RuntimeFact: fact}
		current, found := latest[nodeRef]
		if !found || record.ObservedAt.After(current.ObservedAt) {
			latest[nodeRef] = record
		}
	}
	nodes := mapsort.Keys(latest)
	out := make([]providerDiscoveryRuntimeRecord, 0, len(nodes))
	for _, nodeRef := range nodes {
		out = append(out, latest[nodeRef])
	}
	return out
}

func providerDiscoveryActiveAddressFacts(facts []providerDiscoveryAddressFact, prefix netip.Prefix, now time.Time) map[string]providerDiscoveryAddressFact {
	owned := map[string]providerDiscoveryAddressFact{}
	for _, fact := range facts {
		address, ok := normalizeLeaseAddress(fact.Address, prefix)
		if !ok || !providerDiscoveryAddressFactActive(fact, now) {
			continue
		}
		fact.Address = address
		current, exists := owned[address]
		if !exists || fact.ObservedAt.After(current.ObservedAt) {
			owned[address] = fact
		}
	}
	return owned
}

func providerDiscoveryRuntimeEventID(poolName, nodeRef string, observedAt time.Time) string {
	return providerDiscoveryRuntimeDedupeKey(poolName, nodeRef) + ":" + observedAt.UTC().Format("20060102150405.999999999")
}

func providerDiscoveryRuntimeDedupeKey(poolName, nodeRef string) string {
	return strings.Join([]string{"mobility", providerDiscoverySource, providerDiscoveryRuntimeFactType, strings.TrimSpace(poolName), strings.TrimSpace(nodeRef)}, ":")
}

// onPremDiscoveryStateFromFacts derives the capture gate from durable event
// leases.  The ARP observer DynamicConfigPart is consulted only to ensure that
// daemon-backed sources have an active typed bootstrap intent; it is never
// reconstructed from MobilityPool status.
func onPremDiscoveryStateFromFacts(pool NormalizedMobilityPool, prefix netip.Prefix, events []routerstate.EventRecord, intentReady bool, now time.Time) onPremDiscoveryState {
	state := onPremDiscoveryState{}
	if !intentReady {
		return state
	}
	armed, ok := latestOnPremDiscoveryArmedEvent(pool, events, now)
	if !ok {
		return state
	}
	state.ArmedAt = parseOnPremDiscoveryArmedAt(armed)
	if state.ArmedAt.IsZero() {
		state.ArmedAt = armed.ObservedAt.UTC()
	}
	state.FreshUntil = armed.ExpiresAt.UTC()
	for _, event := range events {
		if onPremDiscoveryEventMatches(pool, prefix, event, now) {
			state.ResultCount++
		}
	}
	if state.ResultCount > 0 {
		state.Phase = "Observed"
		return state
	}
	if allowEmptyAfter := durationDefault(pool.Self.OwnershipDiscovery.AllowEmptyAfter, 0); allowEmptyAfter > 0 && !now.Before(state.ArmedAt.Add(allowEmptyAfter)) {
		state.Phase = "Complete"
		return state
	}
	state.Phase = "Armed"
	return state
}

func (c DiscoveryController) ensureOnPremDiscoveryArmedFact(pool NormalizedMobilityPool, events []routerstate.EventRecord, now time.Time) (routerstate.EventRecord, error) {
	key := onPremDiscoveryConfigKey(pool)
	armedAt := now.UTC()
	if previous, ok := latestOnPremDiscoveryArmedEvent(pool, events, now); ok {
		if previousKey := strings.TrimSpace(previous.Payload["configKey"]); previousKey == key {
			if parsed := parseOnPremDiscoveryArmedAt(previous); !parsed.IsZero() {
				armedAt = parsed
			} else if !previous.ObservedAt.IsZero() {
				armedAt = previous.ObservedAt.UTC()
			}
		}
	}
	ttl := discoveryLeaseTTL(pool.Self.OwnershipDiscovery)
	event := routerstate.EventRecord{
		ID:         onPremDiscoveryArmedDedupeKey(pool.Name, pool.SelfNode, key),
		Group:      strings.TrimSpace(pool.Spec.GroupRef),
		SourceNode: strings.TrimSpace(pool.SelfNode),
		Type:       ObservedEventType,
		Subject:    "MobilityPool/" + strings.TrimSpace(pool.Name) + "/onprem-armed",
		DedupeKey:  onPremDiscoveryArmedDedupeKey(pool.Name, pool.SelfNode, key),
		Payload: map[string]string{
			"pool":       strings.TrimSpace(pool.Name),
			"source":     onPremDiscoverySource,
			"sourceType": onPremDiscoveryArmedFactType,
			"configKey":  key,
			"armedAt":    armedAt.UTC().Format(time.RFC3339Nano),
		},
		ObservedAt: now.UTC(),
		ExpiresAt:  now.UTC().Add(ttl),
		RecordedAt: now.UTC(),
	}
	if err := c.Store.RecordFederationEvent(event); err != nil {
		return routerstate.EventRecord{}, err
	}
	return event, nil
}

func onPremDiscoveryIntentReady(store interface {
	GetDynamicConfigPartsBySource(string) ([]routerstate.DynamicConfigPartRecord, error)
}, pool NormalizedMobilityPool, now time.Time) (bool, error) {
	required := map[string]bool{}
	for _, source := range onPremDiscoverySources(pool.Self.OwnershipDiscovery) {
		switch strings.TrimSpace(source.Type) {
		case OnPremSourceARPObserver, OnPremSourceOnDemandARP, OnPremSourcePVESVNet:
			required[strings.TrimSpace(source.Type)] = true
		}
	}
	if len(required) == 0 {
		return true, nil
	}
	records, err := store.GetDynamicConfigPartsBySource(ARPObserverDynamicSource(pool.Name, pool.SelfNode))
	if err != nil {
		return false, err
	}
	activeRecords, invalidPools := codec.ActiveMobilityPoolPlanRecords(records, now)
	if invalidPools[strings.TrimSpace(pool.Name)] {
		return false, nil
	}
	active := map[string]bool{}
	for _, activeRecord := range activeRecords {
		record, source := activeRecord.Record, activeRecord.Source
		if !source.ARPObserver || source.PoolRef != strings.TrimSpace(pool.Name) || strings.TrimSpace(record.ARPObserverIntentsJSON) == "" {
			continue
		}
		intents, err := codec.DecodeARPObserverIntents(record.ARPObserverIntentsJSON)
		if err != nil {
			continue
		}
		valid := true
		for _, intent := range intents {
			if err := dynamicconfig.ValidateARPObserverIntent(intent, source.PoolRef); err != nil {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		for _, intent := range intents {
			active[intent.SourceType] = true
		}
	}
	for sourceType := range required {
		if !active[sourceType] {
			return false, nil
		}
	}
	return true, nil
}

func latestOnPremDiscoveryArmedEvent(pool NormalizedMobilityPool, events []routerstate.EventRecord, now time.Time) (routerstate.EventRecord, bool) {
	key := onPremDiscoveryConfigKey(pool)
	var (
		latest routerstate.EventRecord
		found  bool
	)
	for _, event := range events {
		if event.Group != pool.Spec.GroupRef || strings.TrimSpace(event.SourceNode) != strings.TrimSpace(pool.SelfNode) || event.Type != ObservedEventType {
			continue
		}
		if strings.TrimSpace(event.Payload["source"]) != onPremDiscoverySource || strings.TrimSpace(event.Payload["sourceType"]) != onPremDiscoveryArmedFactType || strings.TrimSpace(event.Payload["pool"]) != strings.TrimSpace(pool.Name) || strings.TrimSpace(event.Payload["configKey"]) != key {
			continue
		}
		if !event.ExpiresAt.IsZero() && !now.Before(event.ExpiresAt) {
			continue
		}
		if !found || eventRecordGreater(event, latest) {
			latest, found = event, true
		}
	}
	return latest, found
}

func parseOnPremDiscoveryArmedAt(event routerstate.EventRecord) time.Time {
	value, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(event.Payload["armedAt"]))
	if err != nil {
		return time.Time{}
	}
	return value.UTC()
}

func onPremDiscoveryConfigKey(pool NormalizedMobilityPool) string {
	input := struct {
		Pool      string                         `json:"pool"`
		Node      string                         `json:"node"`
		Prefix    string                         `json:"prefix"`
		Capture   api.MobilityMemberCapture      `json:"capture"`
		Discovery api.MobilityOwnershipDiscovery `json:"discovery"`
	}{
		Pool:      strings.TrimSpace(pool.Name),
		Node:      strings.TrimSpace(pool.SelfNode),
		Prefix:    pool.Prefix.String(),
		Capture:   pool.Self.Capture,
		Discovery: pool.Self.OwnershipDiscovery,
	}
	data, _ := json.Marshal(input)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func onPremDiscoveryArmedDedupeKey(poolName, nodeRef, configKey string) string {
	return strings.Join([]string{"mobility", onPremDiscoverySource, onPremDiscoveryArmedFactType, strings.TrimSpace(poolName), strings.TrimSpace(nodeRef), strings.TrimSpace(configKey)}, ":")
}
