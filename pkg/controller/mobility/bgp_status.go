// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"net/netip"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
)

// bgpStatusStore is deliberately the small status-store boundary needed to
// collect a BGP observation.  No planner receives this interface: it receives
// the resulting BGPSnapshot instead.
type bgpStatusStore interface {
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

// BGPSnapshot is the typed BGP observation used by both Mobility and
// Discovery.  It is collected with one pass over BGPRouter status so the two
// controllers cannot drift in their liveness, holder, RIB, owner, or return
// route interpretation.
type BGPSnapshot struct {
	InstalledNextHops  map[string][]string
	CaptureNextHops    map[string][]string
	HomeOwnerNodes     map[string]string
	ReturnRoutes       map[string]bool
	PrefixCommunities  map[string][]string
	LivenessMarkers    map[string]string
	InstalledObserved  bool
	CaptureRIBObserved bool
	LivenessObserved   bool
}

// RIBObserved reports whether either installed-next-hop or prefix observation
// was present.  This preserves the startup-fence meaning used before the
// observations were collected as one snapshot.
func (s BGPSnapshot) RIBObserved() bool {
	return s.InstalledObserved || s.CaptureRIBObserved
}

// collectBGPSnapshot is the sole BGPRouter status decode for one MobilityPool
// reconciliation. Every value returned here is a fact, never desired state;
// member identity comes from the already-normalized Pool rather than a second
// projection of MobilityPoolSpec.
func collectBGPSnapshot(router *api.Router, store bgpStatusStore, pool NormalizedMobilityPool) BGPSnapshot {
	snapshot := BGPSnapshot{
		InstalledNextHops: map[string][]string{},
		CaptureNextHops:   map[string][]string{},
		HomeOwnerNodes:    map[string]string{},
		ReturnRoutes:      map[string]bool{},
		PrefixCommunities: map[string][]string{},
		LivenessMarkers:   map[string]string{},
	}
	if router == nil || store == nil {
		return snapshot
	}
	poolPrefix := pool.Prefix
	if !poolPrefix.IsValid() {
		return snapshot
	}
	communityOwners := map[string]string{}
	for _, member := range pool.Members {
		if community := bgpstate.MobilityNodeIdentityCommunity(strings.TrimSpace(member.NodeRef)); community != "" {
			communityOwners[community] = member.NodeRef
		}
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		status := store.ObjectStatus(api.NetAPIVersion, "BGPRouter", resource.Metadata.Name)
		if raw, ok := status["livenessMarkers"]; ok {
			snapshot.LivenessObserved = true
			for community, prefix := range bgpLivenessMarkersValue(raw) {
				snapshot.LivenessMarkers[community] = prefix
			}
		}
		if raw, ok := status["installedNextHops"]; ok {
			snapshot.InstalledObserved = true
			for prefix, nextHops := range bgpstate.InstalledNextHopsFromStatus(raw) {
				snapshot.InstalledNextHops[prefix] = mergeStringSet(snapshot.InstalledNextHops[prefix], nextHops)
			}
		}
		rawPrefixes, ok := status["prefixes"]
		if !ok {
			continue
		}
		snapshot.CaptureRIBObserved = true
		for _, prefix := range bgpStatusPrefixesValue(rawPrefixes) {
			if !prefix.Valid || prefix.Stale {
				continue
			}
			address, addressOK := normalizeBGPTrapPrefix(prefix.Prefix, poolPrefix)
			if !addressOK {
				continue
			}
			if prefix.Best {
				snapshot.PrefixCommunities[address] = mergeStringSet(snapshot.PrefixCommunities[address], prefix.Communities)
			}
			if bgpstate.HasCommunity(prefix.Communities, bgpstate.MobilityCommunityReturnRoute) {
				snapshot.ReturnRoutes[address] = true
			}
			if bgpstate.HasCommunity(prefix.Communities, bgpstate.MobilityCommunityOwner) {
				for _, community := range prefix.Communities {
					if owner := strings.TrimSpace(communityOwners[strings.TrimSpace(community)]); owner != "" {
						snapshot.HomeOwnerNodes[address] = owner
						break
					}
				}
			}
			if !prefix.Best || bgpstate.HasCommunity(prefix.Communities, bgpstate.MobilityCommunityNodeLiveness) || bgpstate.HasCommunity(prefix.Communities, bgpstate.MobilityCommunityReturnRoute) {
				continue
			}
			nextHop := strings.TrimSpace(prefix.NextHop)
			if nextHop == "" || nextHop == "0.0.0.0" || nextHop == "::" {
				continue
			}
			snapshot.CaptureNextHops[address] = mergeStringSet(snapshot.CaptureNextHops[address], []string{nextHop})
		}
	}
	return snapshot
}

func bgpStatusPrefixesValue(value any) []bgpstate.Prefix {
	// BGPRouter writes a typed []bgpstate.Prefix and SQLite returns the same
	// JSON shape as []any/map[string]any. Keep their one store-boundary codec
	// instead of maintaining a second field-by-field status parser here.
	out := decodeStatusValue[[]bgpstate.Prefix](value)
	for i := range out {
		out[i].Prefix = strings.TrimSpace(out[i].Prefix)
		out[i].NextHop = strings.TrimSpace(out[i].NextHop)
		out[i].Communities = cleanStrings(out[i].Communities)
	}
	return out
}

func bgpLivenessMarkersValue(value any) map[string]string {
	out := map[string]string{}
	for community, prefix := range decodeStatusValue[map[string]string](value) {
		community = strings.TrimSpace(community)
		prefix = normalizeObservedBGPPrefix(prefix)
		if community != "" && prefix != "" {
			out[community] = prefix
		}
	}
	return out
}

func normalizeObservedBGPPrefix(value string) string {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return prefix.Masked().String()
}
