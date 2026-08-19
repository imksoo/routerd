// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"fmt"
	"net/netip"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/hybrid"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/sam"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type dynamicConfigPartLister interface {
	ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error)
}

type dynamicRouteSAMView struct {
	EffectiveRouter   *api.Router
	RouteRouter       *api.Router
	HybridLowerings   []hybrid.HybridLowering
	MobilityDataplane dynamicconfig.MobilityDataplanePlan
}

// BuildDynamicRouteSAMEffectiveRouter returns the route-facing effective router
// used by generic route controllers after dynamic config and Hybrid lowerings.
// Mobility effects are deliberately returned separately as a typed plan.
func BuildDynamicRouteSAMEffectiveRouter(startup *api.Router, store any, now time.Time, targetOS platform.OS) (*api.Router, error) {
	view, err := buildDynamicRouteSAMView(startup, store, now, targetOS)
	if err != nil {
		return nil, err
	}
	return view.RouteRouter, nil
}

// BuildDynamicRouteSAMObjectStatusRouters returns the effective resource views
// whose objects can legitimately own status rows. EffectiveRouter keeps
// dynamic config resources visible to their controllers; RouteRouter includes
// generic route-facing lowerings such as Hybrid routes. Mobility effects never
// become generic resources and are instead held in MobilityDataplane.
func BuildDynamicRouteSAMObjectStatusRouters(startup *api.Router, store any, now time.Time, targetOS platform.OS) ([]*api.Router, error) {
	view, err := buildDynamicRouteSAMView(startup, store, now, targetOS)
	if err != nil {
		return nil, err
	}
	return []*api.Router{view.EffectiveRouter, view.RouteRouter}, nil
}

func buildDynamicRouteSAMView(startup *api.Router, store any, now time.Time, targetOS platform.OS) (dynamicRouteSAMView, error) {
	if startup == nil {
		return dynamicRouteSAMView{}, fmt.Errorf("startup router is required")
	}
	effective := *startup
	var records []routerstate.DynamicConfigPartRecord
	if lister, ok := store.(dynamicConfigPartLister); ok {
		var err error
		records, err = lister.ListDynamicConfigParts()
		if err != nil {
			return dynamicRouteSAMView{}, fmt.Errorf("list dynamic config parts: %w", err)
		}
		if hasDynamicConfigResources(records, now) {
			parts, err := genericDynamicConfigPartsFromRecords(records)
			if err != nil {
				return dynamicRouteSAMView{}, err
			}
			policies, err := dynamicconfig.ExtractDynamicOverridePolicies(*startup)
			if err != nil {
				return dynamicRouteSAMView{}, err
			}
			merged, _, err := dynamicconfig.BuildEffectiveConfigForOS(*startup, parts, policies, now.UTC(), targetOS)
			if err != nil {
				return dynamicRouteSAMView{}, err
			}
			effective = merged
		}
	}
	resolved, err := resolveWireGuardSAMResources(&effective)
	if err != nil {
		return dynamicRouteSAMView{}, err
	}
	effective = *resolved

	mobilityDataplane, err := mobilityDataplanePlanFromRecords(records, now, targetOS)
	if err != nil {
		return dynamicRouteSAMView{}, err
	}

	routeRouter := effective
	hybridLowerings := []hybrid.HybridLowering(nil)
	if hybrid.HasHybridRoutes(&effective) {
		expanded, lowerings, err := hybrid.ExpandHybridRoutes(routeRouter)
		if err != nil {
			return dynamicRouteSAMView{}, err
		}
		routeRouter = expanded
		hybridLowerings = lowerings
	}

	return dynamicRouteSAMView{
		EffectiveRouter:   &effective,
		RouteRouter:       &routeRouter,
		HybridLowerings:   hybridLowerings,
		MobilityDataplane: mobilityDataplane,
	}, nil
}

func hasDynamicConfigResources(records []routerstate.DynamicConfigPartRecord, now time.Time) bool {
	for _, record := range records {
		if record.EffectiveStatus(now.UTC()) == "expired" {
			continue
		}
		if dynamicconfig.IsMobilityPoolReservedSource(record.Source) {
			// MobilityPool has a typed, non-generic effect channel.  Ignore an
			// upgrade-stale ResourcesJSON or DirectivesJSON payload before the
			// first current planner reconcile can replace it.  Replaying it would
			// restore exactly the legacy generic-resource bridge this boundary
			// removes.
			continue
		}
		if strings.TrimSpace(record.ResourcesJSON) != "" || strings.TrimSpace(record.DirectivesJSON) != "" {
			return true
		}
	}
	return false
}

// mobilityDataplanePlanFromRecords is the sole deserialization boundary for
// the mobility-to-dataplane desired-state channel. It intentionally does not
// consult MobilityPool status or the BGP RIB.
func mobilityDataplanePlanFromRecords(records []routerstate.DynamicConfigPartRecord, now time.Time, targetOS platform.OS) (dynamicconfig.MobilityDataplanePlan, error) {
	capturesByID := map[string]dynamicconfig.LocalCaptureIntent{}
	routesByID := map[string]dynamicconfig.MobilityIPv4RouteIntent{}
	addressesByID := map[string]dynamicconfig.MobilityIPv4AddressIntent{}
	poolScopes := map[string]netip.Prefix{}
	activeRecords, invalidPools := codec.ActiveMobilityPoolPlanRecords(records, now)
	if len(invalidPools) != 0 {
		return dynamicconfig.MobilityDataplanePlan{}, fmt.Errorf("invalid active MobilityPool typed plan record for pool(s): %s", sortedMobilityPoolNames(invalidPools))
	}
	for _, active := range activeRecords {
		record, source := active.Record, active.Source
		if source.ARPObserver {
			continue
		}
		plan, err := codec.DecodeMobilityDataplanePlan(record.MobilityDataplaneJSON)
		if err != nil {
			return dynamicconfig.MobilityDataplanePlan{}, fmt.Errorf("decode mobility dataplane plan for %q: %w", record.Source, err)
		}
		if !plan.IsEmpty() {
			if err := dynamicconfig.ValidateMobilityDataplanePlanScope(plan, source.PoolRef); err != nil {
				return dynamicconfig.MobilityDataplanePlan{}, fmt.Errorf("validate mobility dataplane scope for %q: %w", record.Source, err)
			}
			scope, _ := netip.ParsePrefix(plan.PoolPrefix)
			if previous, found := poolScopes[source.PoolRef]; found && previous != scope {
				return dynamicconfig.MobilityDataplanePlan{}, fmt.Errorf("MobilityPool/%s has conflicting typed plan scopes %q and %q", source.PoolRef, previous, scope)
			}
			for otherPool, otherScope := range poolScopes {
				if otherPool != source.PoolRef && mobilityPlanScopesOverlap(scope, otherScope) {
					return dynamicconfig.MobilityDataplanePlan{}, fmt.Errorf("MobilityPool/%s typed plan scope %q overlaps MobilityPool/%s scope %q", source.PoolRef, scope, otherPool, otherScope)
				}
			}
			poolScopes[source.PoolRef] = scope
			for i := range plan.Captures {
				plan.Captures[i].PoolPrefix = plan.PoolPrefix
			}
			for i := range plan.Routes {
				plan.Routes[i].PoolPrefix = plan.PoolPrefix
			}
			for i := range plan.StaticAddresses {
				plan.StaticAddresses[i].PoolPrefix = plan.PoolPrefix
			}
			if err := validateMobilityDataplanePlan(plan, targetOS); err != nil {
				return dynamicconfig.MobilityDataplanePlan{}, fmt.Errorf("validate mobility dataplane plan for %q: %w", record.Source, err)
			}
		}
		for _, intent := range plan.Captures {
			intent.ID = strings.TrimSpace(intent.ID)
			if err := addMobilityPlanEntry(capturesByID, record.Source, source.PoolRef, "capture", intent.ID, intent.PoolRef, intent); err != nil {
				return dynamicconfig.MobilityDataplanePlan{}, err
			}
		}
		for _, route := range plan.Routes {
			route.ID = strings.TrimSpace(route.ID)
			if err := addMobilityPlanEntry(routesByID, record.Source, source.PoolRef, "route", route.ID, route.PoolRef, route); err != nil {
				return dynamicconfig.MobilityDataplanePlan{}, err
			}
		}
		for _, address := range plan.StaticAddresses {
			address.ID = strings.TrimSpace(address.ID)
			if err := addMobilityPlanEntry(addressesByID, record.Source, source.PoolRef, "static address", address.ID, address.PoolRef, address); err != nil {
				return dynamicconfig.MobilityDataplanePlan{}, err
			}
		}
	}
	plan := dynamicconfig.MobilityDataplanePlan{
		Captures:        make([]dynamicconfig.LocalCaptureIntent, 0, len(capturesByID)),
		Routes:          make([]dynamicconfig.MobilityIPv4RouteIntent, 0, len(routesByID)),
		StaticAddresses: make([]dynamicconfig.MobilityIPv4AddressIntent, 0, len(addressesByID)),
	}
	for _, capture := range capturesByID {
		plan.Captures = append(plan.Captures, capture)
	}
	for _, route := range routesByID {
		plan.Routes = append(plan.Routes, route)
	}
	for _, address := range addressesByID {
		plan.StaticAddresses = append(plan.StaticAddresses, address)
	}
	sort.Slice(plan.Captures, func(i, j int) bool { return plan.Captures[i].ID < plan.Captures[j].ID })
	sort.Slice(plan.Routes, func(i, j int) bool { return plan.Routes[i].ID < plan.Routes[j].ID })
	sort.Slice(plan.StaticAddresses, func(i, j int) bool { return plan.StaticAddresses[i].ID < plan.StaticAddresses[j].ID })
	return plan, nil
}

func sortedMobilityPoolNames(pools map[string]bool) string {
	names := make([]string, 0, len(pools))
	for name := range pools {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func mobilityPlanScopesOverlap(left, right netip.Prefix) bool {
	return left.Contains(right.Addr()) || right.Contains(left.Addr())
}

// validateMobilityDataplanePlan checks every typed effect before any
// controller receives the aggregate. This prevents a malformed capture from
// allowing an otherwise-valid route or static address to be applied earlier
// in the reconcile order.
func validateMobilityDataplanePlan(plan dynamicconfig.MobilityDataplanePlan, targetOS platform.OS) error {
	if _, err := sam.PlanLocalCaptureIntents(plan.Captures, targetOS); err != nil {
		return fmt.Errorf("validate mobility captures: %w", err)
	}
	if _, err := normalizedMobilityRouteIntents(plan.Routes); err != nil {
		return fmt.Errorf("validate mobility routes: %w", err)
	}
	if _, err := normalizedMobilityStaticAddressIntents(plan.StaticAddresses); err != nil {
		return fmt.Errorf("validate mobility static addresses: %w", err)
	}
	return nil
}

func addMobilityPlanEntry[T any](entries map[string]T, source, sourcePoolRef, kind, id, poolRef string, value T) error {
	if id == "" {
		return fmt.Errorf("mobility %s in %q is missing id", kind, source)
	}
	if strings.TrimSpace(poolRef) != sourcePoolRef {
		return fmt.Errorf("mobility %s %q in %q belongs to pool %q, want %q", kind, id, source, strings.TrimSpace(poolRef), sourcePoolRef)
	}
	if existing, found := entries[id]; found && !reflect.DeepEqual(existing, value) {
		return fmt.Errorf("conflicting mobility %s id %q from %q", kind, id, source)
	}
	entries[id] = value
	return nil
}

// mobilityARPObserverIntentsFromRecords is the sole deserialization boundary
// for discovery-owned ARP observer bootstrap. Like the local dataplane plan,
// it consumes only an active typed DynamicConfigPart and never reopens raw
// MobilityPool configuration.
func mobilityARPObserverIntentsFromRecords(records []routerstate.DynamicConfigPartRecord, now time.Time) []dynamicconfig.ARPObserverIntent {
	intentsByName := map[string]dynamicconfig.ARPObserverIntent{}
	conflictingNames := map[string]bool{}
	activeRecords, invalidPools := codec.ActiveMobilityPoolPlanRecords(records, now)
	for _, active := range activeRecords {
		record, source := active.Record, active.Source
		if !source.ARPObserver || invalidPools[source.PoolRef] {
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
			if conflictingNames[intent.ResourceName] {
				continue
			}
			if existing, found := intentsByName[intent.ResourceName]; found {
				if !reflect.DeepEqual(existing, intent) {
					delete(intentsByName, intent.ResourceName)
					conflictingNames[intent.ResourceName] = true
				}
				continue
			}
			intentsByName[intent.ResourceName] = intent
		}
	}
	intentsOut := make([]dynamicconfig.ARPObserverIntent, 0, len(intentsByName))
	for _, intent := range intentsByName {
		intentsOut = append(intentsOut, intent)
	}
	sort.Slice(intentsOut, func(i, j int) bool {
		return intentsOut[i].ResourceName < intentsOut[j].ResourceName
	})
	return intentsOut
}

// genericDynamicConfigPartsFromRecords decodes only payloads that are allowed
// to enter the generic effective-router merge. MobilityPool effects are an
// explicit typed channel; any historical generic payload from that source is
// intentionally ignored rather than migrated or replayed.
func genericDynamicConfigPartsFromRecords(records []routerstate.DynamicConfigPartRecord) ([]dynamicconfig.DynamicConfigPart, error) {
	generic := make([]routerstate.DynamicConfigPartRecord, 0, len(records))
	for _, record := range records {
		if dynamicconfig.IsMobilityPoolReservedSource(record.Source) {
			continue
		}
		generic = append(generic, record)
	}
	return codec.DecodeAll(generic)
}
