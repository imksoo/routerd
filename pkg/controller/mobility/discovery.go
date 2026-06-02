// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/plugin"
	"github.com/imksoo/routerd/pkg/providerinventory"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	providerDiscoverySource      = "provider-discovery"
	defaultDiscoveryScanInterval = 60 * time.Second
	minDiscoveryScanInterval     = 30 * time.Second
)

type DiscoveryStore interface {
	RecordFederationEvent(routerstate.EventRecord) error
	ListFederationEvents(group string, includeExpired bool, now int64) ([]routerstate.EventRecord, error)
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type DiscoveryController struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  DiscoveryStore
	Runner providerinventory.Runner
	Now    func() time.Time
}

func (c DiscoveryController) HandleEvent(ctx context.Context, _ daemonapi.DaemonEvent) error {
	return c.Reconcile(ctx)
}

func (c DiscoveryController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	now := c.now()
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "MobilityPool" {
			continue
		}
		spec, err := res.MobilityPoolSpec()
		if err != nil {
			c.saveDiscoveryStatus(res.Metadata.Name, map[string]any{"discoveryPhase": "Degraded", "discoveryReason": err.Error()})
			continue
		}
		if !mobilityBGPMode(spec) {
			continue
		}
		if err := c.reconcilePoolDiscovery(ctx, res.Metadata.Name, spec, now); err != nil {
			c.saveDiscoveryStatus(res.Metadata.Name, map[string]any{
				"discoveryPhase":  "Degraded",
				"discoveryReason": err.Error(),
			})
		}
	}
	return nil
}

func (c DiscoveryController) reconcilePoolDiscovery(ctx context.Context, poolName string, spec api.MobilityPoolSpec, now time.Time) error {
	selfNode, err := routerSelfNode(c.Router, spec.GroupRef)
	if err != nil {
		return err
	}
	members := plannerMembers(spec.Members)
	self, ok := members[selfNode]
	if !ok {
		return fmt.Errorf("self node %q is not a member of MobilityPool/%s", selfNode, poolName)
	}
	discovery := self.OwnershipDiscovery
	if strings.TrimSpace(discovery.Mode) != "provider-private-ip" {
		return nil
	}
	interval := discoveryScanInterval(discovery)
	if !c.scanDue(poolName, interval, now) {
		return nil
	}
	if self.Role != "cloud" || self.Capture.Type != "provider-secondary-ip" {
		return fmt.Errorf("ownershipDiscovery requires cloud provider-secondary-ip member %q", self.NodeRef)
	}
	liveness, err := ownershipLivenessFromStore(c.Store, poolName, spec, now)
	if err != nil {
		return err
	}
	placement := evaluatePlacementWithLiveness(self, members, spec.IPOwnershipPolicy, liveness)
	if !placement.Active {
		c.saveDiscoveryStatus(poolName, map[string]any{
			"discoveryPhase":      "Standby",
			"discoveryReason":     placement.Reason,
			"discoveryLastScanAt": now.Format(time.RFC3339Nano),
		})
		return nil
	}
	profileRef := strings.TrimSpace(discovery.ProviderRef)
	if profileRef == "" {
		profileRef = strings.TrimSpace(self.Capture.ProviderRef)
	}
	profile, ok := cloudProviderProfiles(c.Router)[profileRef]
	if !ok {
		return fmt.Errorf("CloudProviderProfile/%s not found for MobilityPool/%s member %q ownershipDiscovery", profileRef, poolName, self.NodeRef)
	}
	pluginSpec, pluginName, err := c.resolveInventoryPlugin(profile.Provider, discovery)
	if err != nil {
		return err
	}
	pluginContext, err := plugin.BuildPluginContext(pluginSpec.Context.Resources, c.Router.Spec.Resources)
	if err != nil {
		return err
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return fmt.Errorf("parse pool prefix: %w", err)
	}
	prefix = prefix.Masked()
	req := providerinventory.NewObservePrivateIPsRequest(providerinventory.ObservePrivateIPsRequestSpec{
		Provider:    strings.TrimSpace(profile.Provider),
		ProviderRef: profileRef,
		SelfNode:    self.NodeRef,
		Pool:        poolName,
		Prefix:      prefix.String(),
		SelfNICRef:  strings.TrimSpace(self.Capture.NICRef),
		SubnetRef:   strings.TrimSpace(discovery.SubnetRef),
		Target:      copyStringMap(self.CaptureTarget),
		Selector:    providerinventory.InventorySelector{Tags: copyStringMap(discovery.Selector.Tags)},
		Context:     pluginContext,
	})
	result, _, err := c.runner()(ctx, pluginSpec, req)
	if err != nil {
		return fmt.Errorf("run provider inventory plugin %q: %w", pluginName, err)
	}
	switch result.Status.Status {
	case providerinventory.ResultSucceeded:
	case providerinventory.ResultSkipped:
		c.saveDiscoveryStatus(poolName, map[string]any{
			"discoveryPhase":      "Skipped",
			"discoveryReason":     result.Status.Message,
			"discoveryLastScanAt": now.Format(time.RFC3339Nano),
		})
		return nil
	case providerinventory.ResultFailed:
		return fmt.Errorf("provider inventory plugin %q failed: %s", pluginName, firstNonEmpty(result.Status.Error, result.Status.Message))
	default:
		return fmt.Errorf("provider inventory plugin %q returned invalid status %q", pluginName, result.Status.Status)
	}
	excludedNICs := mobilityRouterNICRefs(spec.Members)
	ttl := discoveryLeaseTTL(discovery, spec)
	var emitted, excluded int
	for _, rec := range sortedPrivateIPs(result.Status.IPs) {
		address, ok := normalizeDiscoveredAddress(rec.Address, prefix)
		if !ok {
			excluded++
			continue
		}
		if strings.TrimSpace(rec.NICRef) != "" && excludedNICs[strings.TrimSpace(rec.NICRef)] {
			excluded++
			continue
		}
		if !discoverySelectorMatches(discovery.Selector, rec.Tags) {
			excluded++
			continue
		}
		ev := providerDiscoveryObservedEvent(poolName, spec.GroupRef, self.NodeRef, address, profile.Provider, profileRef, rec, now, ttl)
		if err := c.Store.RecordFederationEvent(ev); err != nil {
			return err
		}
		emitted++
	}
	c.saveDiscoveryStatus(poolName, map[string]any{
		"discoveryPhase":       "Observed",
		"discoveryReason":      "",
		"discoveryProvider":    profile.Provider,
		"discoveryProviderRef": profileRef,
		"discoveryPlugin":      pluginName,
		"discoveryObserved":    emitted,
		"discoveryExcluded":    excluded,
		"discoveryLastScanAt":  now.Format(time.RFC3339Nano),
		"discoveryNextScanAt":  now.Add(interval).Format(time.RFC3339Nano),
	})
	return nil
}

func (c DiscoveryController) scanDue(poolName string, interval time.Duration, now time.Time) bool {
	status := c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName)
	last, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(fmt.Sprint(status["discoveryLastScanAt"])))
	if err != nil || last.IsZero() {
		return true
	}
	return !last.Add(interval).After(now)
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
	status := map[string]any{}
	for k, v := range c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName) {
		status[k] = v
	}
	for k, v := range updates {
		status[k] = v
	}
	_ = c.Store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName, status)
}

func (c DiscoveryController) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func discoveryScanInterval(discovery api.MobilityOwnershipDiscovery) time.Duration {
	interval := durationDefault(discovery.ScanInterval, defaultDiscoveryScanInterval)
	if interval < minDiscoveryScanInterval {
		return minDiscoveryScanInterval
	}
	return interval
}

func discoveryLeaseTTL(discovery api.MobilityOwnershipDiscovery, spec api.MobilityPoolSpec) time.Duration {
	return durationDefault(firstNonEmpty(discovery.LeaseTTL, spec.LeasePolicy.TTL), DefaultLeaseTTL)
}

func normalizeDiscoveredAddress(value string, poolPrefix netip.Prefix) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if strings.Contains(value, "/") {
		return normalizeLeaseAddress(value, poolPrefix)
	}
	addr, err := netip.ParseAddr(value)
	if err != nil || !addr.Is4() || !poolPrefix.Contains(addr) {
		return "", false
	}
	return netip.PrefixFrom(addr, 32).String(), true
}

func mobilityRouterNICRefs(members []api.MobilityPoolMember) map[string]bool {
	out := map[string]bool{}
	for _, member := range members {
		nic := strings.TrimSpace(member.Capture.NICRef)
		if member.Capture.Type == "provider-secondary-ip" && nic != "" {
			out[nic] = true
		}
	}
	return out
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

func providerDiscoveryObservedEvent(poolName, group, nodeRef, address, provider, providerRef string, rec providerinventory.PrivateIPRecord, now time.Time, ttl time.Duration) routerstate.EventRecord {
	observedAt := now.UTC()
	payload := map[string]string{
		"address":     address,
		"pool":        poolName,
		"source":      providerDiscoverySource,
		"provider":    strings.TrimSpace(provider),
		"providerRef": strings.TrimSpace(providerRef),
	}
	if value := strings.TrimSpace(rec.NICRef); value != "" {
		payload["nicRef"] = value
	}
	if value := strings.TrimSpace(rec.SubnetRef); value != "" {
		payload["subnetRef"] = value
	}
	if rec.Primary {
		payload["primary"] = "true"
	}
	return routerstate.EventRecord{
		ID:         providerDiscoveryEventID(poolName, nodeRef, address, observedAt),
		Group:      strings.TrimSpace(group),
		SourceNode: strings.TrimSpace(nodeRef),
		Type:       ObservedEventType,
		Subject:    address,
		DedupeKey:  providerDiscoveryDedupeKey(poolName, nodeRef, address),
		Payload:    payload,
		ObservedAt: observedAt,
		ExpiresAt:  observedAt.Add(ttl),
		RecordedAt: observedAt,
	}
}

func providerDiscoveryEventID(poolName, nodeRef, address string, observedAt time.Time) string {
	return providerDiscoveryDedupeKey(poolName, nodeRef, address) + ":" + strconv.FormatInt(observedAt.UTC().UnixNano(), 10)
}

func providerDiscoveryDedupeKey(poolName, nodeRef, address string) string {
	return strings.Join([]string{"mobility", providerDiscoverySource, strings.TrimSpace(poolName), strings.TrimSpace(nodeRef), strings.ReplaceAll(strings.TrimSpace(address), "/", "_")}, ":")
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
