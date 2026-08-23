// SPDX-License-Identifier: BSD-3-Clause
package mobility

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// resolveCaptureSourceAddress resolves a capture address from a configured
// value or from the configured status source. It is an input-resolution helper
// for the controller shell; address ownership remains in the planner.
func resolveCaptureSourceAddress(reader resourcequery.Store, configured string, source api.StatusValueSourceSpec, pool netip.Prefix) string {
	configured = strings.TrimSpace(configured)
	if configured != "" || reader == nil {
		return configured
	}
	if !pool.IsValid() || !pool.Addr().Is4() {
		return ""
	}
	for _, raw := range resourcequery.Values(reader, source) {
		if prefix, ok := captureSourcePrefix(raw, pool); ok {
			return prefix.Addr().String()
		}
	}
	return ""
}

func bgpPathSigsByAddress(plans []dynamicconfig.ActionPlan) map[string]string {
	out := map[string]string{}
	for _, plan := range plans {
		if !isProviderCaptureAssignAction(plan.Action) {
			continue
		}
		address := normalizeAddressString(plan.Target["address"])
		if address == "" {
			continue
		}
		if sig := strings.TrimSpace(plan.Parameters[bgpPathSigParam]); sig != "" {
			out[address] = sig
		}
	}
	return out
}

func nonNegativeCeilSeconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64((d + time.Second - 1) / time.Second)
}

const onPremL2DiscoveryWarmup = 30 * time.Second

func onPremL2OwnershipPending(self memberPlanInfo, placement PlacementDecision, decisions []ownershipDecision, discovery onPremDiscoveryState, now time.Time) (bool, string) {
	if strings.TrimSpace(self.Role) != "onprem" || strings.TrimSpace(self.OwnershipDiscovery.Mode) != "onprem-l2" {
		return false, ""
	}
	if !placement.Active {
		return false, ""
	}
	if len(onPremDiscoverySources(self.OwnershipDiscovery)) == 0 {
		return false, ""
	}
	phase := strings.TrimSpace(discovery.Phase)
	if phase != "Observed" && phase != "Complete" {
		return true, "onprem-l2 ownership discovery has not completed an initial observation"
	}
	if phase == "Complete" && discovery.ResultCount == 0 {
		if !discovery.FreshUntil.IsZero() && now.Before(discovery.FreshUntil) {
			return false, ""
		}
		return true, "onprem-l2 empty ownership discovery is not fresh"
	}
	if !discovery.ArmedAt.IsZero() && now.Sub(discovery.ArmedAt) < onPremL2DiscoveryWarmup {
		return true, fmt.Sprintf("onprem-l2 ownership discovery is warming up for %s", onPremL2DiscoveryWarmup)
	}
	for _, decision := range decisions {
		if strings.TrimSpace(decision.HomeOwnerNode) != strings.TrimSpace(self.NodeRef) {
			continue
		}
		switch decision.Class {
		case ownershipClassLocalHomeOwned, ownershipClassStaticOwned, ownershipClassStaticHandover:
			return false, ""
		}
	}
	return true, "onprem-l2 ownership discovery has not observed any local clients"
}

func (c Controller) recordBGPStaticHandoverReleaseEvents(pool NormalizedMobilityPool, events []routerstate.EventRecord, now time.Time) ([]routerstate.EventRecord, error) {
	if len(pool.Spec.StaticHandovers) == 0 {
		return nil, nil
	}
	if !pool.Prefix.IsValid() {
		return nil, fmt.Errorf("MobilityPool/%s normalized prefix is required", pool.Name)
	}
	existing := map[string]bool{}
	for _, ev := range events {
		if ev.Type != ExpiredEventType || strings.TrimSpace(ev.SourceNode) != strings.TrimSpace(pool.SelfNode) {
			continue
		}
		address, ok := normalizeLeaseAddress(firstNonEmpty(ev.Payload["address"], ev.Subject), pool.Prefix)
		if ok {
			existing[address] = true
		}
	}
	var emitted []routerstate.EventRecord
	for _, handover := range pool.Spec.StaticHandovers {
		if strings.TrimSpace(handover.FromNodeRef) != strings.TrimSpace(pool.SelfNode) {
			continue
		}
		address, ok := normalizeLeaseAddress(handover.Address, pool.Prefix)
		if !ok || existing[address] {
			continue
		}
		ev := routerstate.EventRecord{
			ID:         "mobility:bgp-static-release:" + pool.Name + ":" + pool.SelfNode + ":" + safeName(address),
			Group:      pool.Spec.GroupRef,
			SourceNode: pool.SelfNode,
			Type:       ExpiredEventType,
			Subject:    address,
			DedupeKey:  "mobility:bgp-static-release:" + pool.Name + ":" + pool.SelfNode + ":" + address,
			Payload: map[string]string{
				"address":    address,
				"pool":       pool.Name,
				"sourceType": staticHandoverType,
			},
			ObservedAt: now.UTC(),
			RecordedAt: now.UTC(),
			ExpiresAt:  now.UTC().Add(DefaultLeaseTTL),
		}
		if err := c.Store.RecordFederationEvent(ev); err != nil {
			return nil, fmt.Errorf("record BGP static handover release %q: %w", ev.ID, err)
		}
		emitted = append(emitted, ev)
	}
	return emitted, nil
}

// poolWithDiscoveredSelfCapture applies the typed discovery observation only
// to the normalized local overlay. The API spec remains immutable after the
// normalizer boundary, so no second spec-to-planner conversion is needed.
func poolWithDiscoveredSelfCapture(pool NormalizedMobilityPool, observation poolDiscoveryObservation) (NormalizedMobilityPool, bool, string) {
	self := pool.Self
	if self.Capture.Type != "provider-secondary-ip" || self.OwnershipDiscovery.Mode != "provider-private-ip" {
		return pool, true, ""
	}
	if strings.TrimSpace(self.Capture.NICRef) != "" {
		return pool, true, ""
	}
	nicRef := strings.TrimSpace(observation.SelfNICRef)
	if nicRef == "" {
		return pool, false, "provider inventory self NIC is unresolved"
	}
	self.Capture.NICRef = nicRef
	if strings.TrimSpace(self.OwnershipDiscovery.SubnetRef) == "" {
		if subnetRef := strings.TrimSpace(observation.SelfSubnetRef); subnetRef != "" {
			self.OwnershipDiscovery.SubnetRef = subnetRef
			target := copyStringMap(self.Capture.Target)
			if target == nil {
				target = map[string]string{}
			}
			if strings.TrimSpace(target["subnetRef"]) == "" {
				target["subnetRef"] = subnetRef
			}
			self.Capture.Target = target
		}
	}
	return poolWithSelf(pool, self), true, ""
}

func (c Controller) upsertBGPPlan(ctx context.Context, poolName, selfNode string, actionPlans []dynamicconfig.ActionPlan, localDataplane dynamicconfig.MobilityDataplanePlan, fibVerdicts []dynamicconfig.FIBVerdict, now time.Time) error {
	source := DynamicSource(poolName, selfNode)
	previous, err := c.Store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return fmt.Errorf("get previous MobilityPool dynamic config part %s: %w", source, err)
	}
	part := dynamicconfig.NewPart(safeName("mobility-"+poolName+"-"+selfNode), DynamicSource(poolName, selfNode), []api.OwnerRef{{
		APIVersion: api.MobilityAPIVersion,
		Kind:       "MobilityPool",
		Name:       poolName,
	}}, dynamicGeneration, now, now.Add(DefaultLeaseTTL))
	// Mobility effects are not generic Router resources. Keep the ordinary
	// dynamic resource payload explicitly empty so a new plan also clears the
	// older synthetic IPv4Route projection from this source.
	part.Spec.Resources = []api.Resource{}
	part.Spec.ActionPlans = actionPlans
	part.Spec.MobilityDataplane = localDataplane
	part.Spec.FIBVerdicts = fibVerdicts
	part.Spec.Digest = digestDynamicPart(part)
	record, err := codec.Encode(part)
	if err != nil {
		return err
	}
	changed := dynamicPartContentChanged(previous, record, now)
	if err := c.Store.UpsertDynamicConfigPart(record); err != nil {
		return err
	}
	if changed {
		c.publishPoolPlanChanged(ctx, poolName, source, record.Digest, now)
	}
	return nil
}

// deprovisionStaleMobilitySources retires a complete local MobilityPool plan
// when its current node or Pool disappears.  BGP is withdrawn before the
// typed plan is expired, so a prior node cannot retain a host capture or a
// /32 advertisement until the normal lease timeout. A membersFrom-pending
// Pool is explicitly retained to preserve its fail-static contract.
func (c Controller) deprovisionStaleMobilitySources(ctx context.Context, desiredSources, retainPoolSources map[string]bool, now time.Time) error {
	if c.BGPPaths == nil {
		return fmt.Errorf("cannot retire stale MobilityPool sources without routerd-bgp control client")
	}
	localNodes := c.localMobilityNodeRefs()
	// Reconcile is a local controller operation. A missing local EventGroup
	// identity must never turn a store scan into a cross-node withdrawal. The
	// controller must fail closed instead of guessing ownership from a stale
	// DynamicConfigPart source.
	if len(localNodes) == 0 {
		return nil
	}
	parts, err := c.Store.ListDynamicConfigParts()
	if err != nil {
		return fmt.Errorf("list dynamic config parts for MobilityPool source retirement: %w", err)
	}
	now = now.UTC()
	seen := map[string]bool{}
	for _, record := range parts {
		parsed, ok := dynamicconfig.ParseMobilityPoolPlanSource(record.Source)
		if !ok || seen[record.Source] || retainPoolSources[parsed.PoolRef] {
			continue
		}
		// A scoped graceful handoff intentionally reconciles only its target
		// Pools. Keep an already-persisted source for every other Pool even
		// when that Pool is absent from the current Router spec: this narrow
		// controller invocation must not turn that absence into a withdrawal.
		if len(c.ReconcilePools) > 0 && !c.reconcilesPool(parsed.PoolRef) {
			continue
		}
		if !localNodes[parsed.NodeRef] {
			continue
		}
		mainSource := DynamicSource(parsed.PoolRef, parsed.NodeRef)
		if desiredSources[mainSource] {
			continue
		}
		seen[record.Source] = true
		if !parsed.ARPObserver {
			if err := c.applyBGPPaths(ctx, record.Source, nil); err != nil {
				return fmt.Errorf("withdraw stale MobilityPool BGP source %q: %w", record.Source, err)
			}
		}
		if err := c.expireMobilityPlanSource(ctx, record.Source, parsed, now); err != nil {
			return err
		}
	}
	return nil
}

// localMobilityNodeRefs returns the local EventGroup identities this router is
// authorized to reconcile. DynamicConfigPart source names carry a nodeRef, so
// this is the ownership boundary for source retirement as well as planning.
func (c Controller) localMobilityNodeRefs() map[string]bool {
	local := map[string]bool{}
	if c.Router == nil {
		return local
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.FederationAPIVersion || resource.Kind != "EventGroup" {
			continue
		}
		spec, err := resource.EventGroupSpec()
		if err != nil {
			continue
		}
		if nodeRef := strings.TrimSpace(spec.NodeName); nodeRef != "" {
			local[nodeRef] = true
		}
	}
	return local
}

func (c Controller) expireMobilityPlanSource(ctx context.Context, source string, parsed dynamicconfig.MobilityPoolPlanSource, now time.Time) error {
	previous, err := c.Store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return fmt.Errorf("get stale MobilityPool dynamic config part %q: %w", source, err)
	}
	if mobilityPlanSourceAlreadyExpired(previous, now) {
		return nil
	}
	part := dynamicconfig.NewPart(
		safeName("mobility-"+parsed.PoolRef+"-"+parsed.NodeRef),
		source,
		[]api.OwnerRef{{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool", Name: parsed.PoolRef}},
		dynamicGeneration,
		now,
		now,
	)
	part.Spec.Resources = []api.Resource{}
	part.Spec.ActionPlans = []dynamicconfig.ActionPlan{}
	part.Spec.MobilityDataplane = dynamicconfig.MobilityDataplanePlan{}
	part.Spec.ARPObserverIntents = []dynamicconfig.ARPObserverIntent{}
	part.Spec.FIBVerdicts = []dynamicconfig.FIBVerdict{}
	part.Spec.Digest = digestDynamicPart(part)
	record, err := codec.Encode(part)
	if err != nil {
		return fmt.Errorf("encode stale MobilityPool source %q withdrawal: %w", source, err)
	}
	changed := dynamicPartContentChanged(previous, record, now)
	if err := c.Store.UpsertDynamicConfigPart(record); err != nil {
		return fmt.Errorf("expire stale MobilityPool source %q: %w", source, err)
	}
	if changed {
		c.publishPoolPlanChanged(ctx, parsed.PoolRef, source, record.Digest, now)
	}
	return nil
}

func mobilityPlanSourceAlreadyExpired(parts []routerstate.DynamicConfigPartRecord, now time.Time) bool {
	if len(parts) == 0 {
		return true
	}
	for _, part := range parts {
		if part.EffectiveStatus(now) != "expired" || strings.TrimSpace(part.ActionPlansJSON) != "" ||
			strings.TrimSpace(part.MobilityDataplaneJSON) != "" || strings.TrimSpace(part.ARPObserverIntentsJSON) != "" ||
			strings.TrimSpace(part.FIBVerdictsJSON) != "" {
			return false
		}
	}
	return true
}

// dynamicPartContentChanged reports whether a durable DynamicConfigPart has
// changed desired content or effective lifetime. Observed/updated timestamps
// deliberately do not count: refreshing an unchanged lease must not create a
// reconcile storm for its consumers.
func dynamicPartContentChanged(previous []routerstate.DynamicConfigPartRecord, next routerstate.DynamicConfigPartRecord, now time.Time) bool {
	for _, current := range previous {
		if current.Generation != next.Generation {
			continue
		}
		return strings.TrimSpace(current.Digest) != strings.TrimSpace(next.Digest) ||
			current.EffectiveStatus(now.UTC()) != next.EffectiveStatus(now.UTC())
	}
	return true
}

func mobilityTunnelInterfaces(router *api.Router) []string {
	if router == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "TunnelInterface" {
			continue
		}
		if name := strings.TrimSpace(resource.Metadata.Name); name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
