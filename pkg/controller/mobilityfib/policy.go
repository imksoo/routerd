// SPDX-License-Identifier: BSD-3-Clause

package mobilityfib

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
)

type StatusReader interface {
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type Snapshot struct {
	pools []poolSnapshot
}

type poolSnapshot struct {
	name        string
	selfNode    string
	prefix      netip.Prefix
	verdicts    map[string]Verdict
	staticLocal map[string]bool
	localReturn map[string]bool
}

const (
	ActionDeliverRemote = "deliver-remote"
	ActionLocalRoute    = "local-route"
	ActionWithhold      = "withhold"
)

const (
	communityMobilityOwner          = "64512:100"
	communityMobilitySourceObserved = "64512:110"
	communityMobilitySourceStatic   = "64512:111"
	communityMobilitySourceHandover = "64512:112"
	communityMobilityReturnRoute    = bgpstate.MobilityCommunityReturnRoute
)

type Verdict struct {
	Address   string
	Action    string
	Class     string
	OwnerNode string
	Reason    string
}

func NewSnapshot(router *api.Router, reader StatusReader) Snapshot {
	if router == nil {
		return Snapshot{}
	}
	selfByGroup := eventGroupSelfNodes(router)
	if len(selfByGroup) == 0 {
		return Snapshot{}
	}
	var pools []poolSnapshot
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "MobilityPool" {
			continue
		}
		spec, err := resource.MobilityPoolSpec()
		if err != nil || mobilityDeliveryMode(spec) != "bgp" {
			continue
		}
		selfNode := strings.TrimSpace(selfByGroup[strings.TrimSpace(spec.GroupRef)])
		if selfNode == "" {
			continue
		}
		spec, _, err = mobilityconfig.NormalizeMobilityPool(spec, selfNode)
		if err != nil {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
		if err != nil || !prefix.Addr().Is4() {
			continue
		}
		pool := poolSnapshot{
			name:        resource.Metadata.Name,
			selfNode:    selfNode,
			prefix:      prefix.Masked(),
			verdicts:    map[string]Verdict{},
			staticLocal: map[string]bool{},
			localReturn: map[string]bool{},
		}
		var selfMember api.MobilityPoolMember
		for _, member := range spec.Members {
			if strings.TrimSpace(member.NodeRef) == selfNode {
				selfMember = member
				break
			}
		}
		for _, member := range spec.Members {
			nodeRef := strings.TrimSpace(member.NodeRef)
			if nodeRef == "" {
				continue
			}
			if sameReturnRouteSite(selfMember, member) {
				if community := bgpstate.MobilityNodeIdentityCommunity(canonicalNodeIdentity(nodeRef)); community != "" {
					pool.localReturn[community] = true
				}
			}
			if nodeRef != selfNode {
				continue
			}
			for _, raw := range member.StaticOwnedAddresses {
				if address, ok := normalizePoolAddress(raw, pool.prefix); ok {
					pool.staticLocal[address] = true
				}
			}
		}
		if reader != nil {
			status := reader.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", resource.Metadata.Name)
			for _, row := range statusMapSlice(status["ownershipResolverFIBVerdicts"]) {
				parsed := verdictFromMap(row)
				address, ok := normalizePoolAddress(parsed.Address, pool.prefix)
				if !ok {
					continue
				}
				parsed.Address = address
				pool.verdicts[address] = parsed
			}
		}
		pools = append(pools, pool)
	}
	return Snapshot{pools: pools}
}

func (s Snapshot) AdmitBGPPath(prefix netip.Prefix, communities []string) bool {
	prefix = prefix.Masked()
	pool, ok := s.poolFor(prefix)
	if !ok {
		return true
	}
	if prefix.Bits() != 32 {
		return false
	}
	address := prefix.String()
	if pool.staticLocal[address] {
		return false
	}
	if verdict, ok := pool.verdicts[address]; ok {
		return strings.TrimSpace(verdict.Action) == ActionDeliverRemote
	}
	if hasCommunity(communities, communityMobilityReturnRoute) {
		return pool.admitReturnRoute(communities)
	}
	if !hasCommunity(communities, communityMobilityOwner) {
		return false
	}
	return hasCommunity(communities, communityMobilitySourceObserved) ||
		hasCommunity(communities, communityMobilitySourceStatic) ||
		hasCommunity(communities, communityMobilitySourceHandover)
}

func (p poolSnapshot) admitReturnRoute(communities []string) bool {
	for _, community := range communities {
		community = strings.TrimSpace(community)
		if !bgpstate.IsMobilityNodeIdentityCommunity(community) {
			continue
		}
		return !p.localReturn[community]
	}
	return false
}

func (s Snapshot) LocalRouteAddressesForPool(poolName string) []string {
	poolName = strings.TrimSpace(poolName)
	seen := map[string]bool{}
	for _, pool := range s.pools {
		if pool.name != poolName {
			continue
		}
		for _, address := range pool.localRouteAddresses() {
			seen[address] = true
		}
	}
	return sortedKeys(seen)
}

func (s Snapshot) LocalRouteVerdictsForPool(poolName string) []Verdict {
	poolName = strings.TrimSpace(poolName)
	seen := map[string]Verdict{}
	for _, pool := range s.pools {
		if pool.name != poolName {
			continue
		}
		for _, verdict := range pool.localRouteVerdicts() {
			seen[verdict.Address] = verdict
		}
	}
	keys := sortedVerdictKeys(seen)
	out := make([]Verdict, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}
	return out
}

func (p poolSnapshot) localRouteAddresses() []string {
	seen := map[string]bool{}
	for address := range p.staticLocal {
		seen[address] = true
	}
	for address, verdict := range p.verdicts {
		if strings.TrimSpace(verdict.Action) == ActionLocalRoute {
			seen[address] = true
		}
	}
	return sortedKeys(seen)
}

func (p poolSnapshot) localRouteVerdicts() []Verdict {
	seen := map[string]Verdict{}
	for address := range p.staticLocal {
		seen[address] = Verdict{
			Address:   address,
			Action:    ActionLocalRoute,
			Class:     "StaticOwned",
			OwnerNode: p.selfNode,
			Reason:    "static-local",
		}
	}
	for address, verdict := range p.verdicts {
		if strings.TrimSpace(verdict.Action) == ActionLocalRoute {
			seen[address] = verdict
		}
	}
	keys := sortedVerdictKeys(seen)
	out := make([]Verdict, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}
	return out
}

func (s Snapshot) poolFor(prefix netip.Prefix) (poolSnapshot, bool) {
	if !prefix.Addr().Is4() {
		return poolSnapshot{}, false
	}
	for _, pool := range s.pools {
		if pool.prefix.Contains(prefix.Addr()) {
			return pool, true
		}
	}
	return poolSnapshot{}, false
}

func verdictFromMap(row map[string]string) Verdict {
	return Verdict{
		Address:   row["address"],
		Action:    row["action"],
		Class:     row["class"],
		OwnerNode: row["ownerNode"],
		Reason:    row["reason"],
	}
}

func eventGroupSelfNodes(router *api.Router) map[string]string {
	out := map[string]string{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.FederationAPIVersion || resource.Kind != "EventGroup" {
			continue
		}
		spec, err := resource.EventGroupSpec()
		if err == nil {
			out[resource.Metadata.Name] = strings.TrimSpace(spec.NodeName)
		}
	}
	return out
}

func mobilityDeliveryMode(spec api.MobilityPoolSpec) string {
	switch strings.TrimSpace(spec.DeliveryPolicy.Mode) {
	case "":
		return "bgp"
	default:
		return strings.TrimSpace(spec.DeliveryPolicy.Mode)
	}
}

func sameReturnRouteSite(a, b api.MobilityPoolMember) bool {
	aNode := strings.TrimSpace(a.NodeRef)
	bNode := strings.TrimSpace(b.NodeRef)
	if aNode == "" || bNode == "" {
		return false
	}
	if aNode == bNode {
		return true
	}
	aSite := strings.TrimSpace(a.Site)
	bSite := strings.TrimSpace(b.Site)
	return aSite != "" && aSite == bSite
}

func canonicalNodeIdentity(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "Node/") {
		return strings.TrimSpace(strings.TrimPrefix(value, "Node/"))
	}
	return value
}

func normalizePoolAddress(value string, pool netip.Prefix) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || !pool.Addr().Is4() {
		return "", false
	}
	var addr netip.Addr
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !prefix.Addr().Is4() {
			return "", false
		}
		addr = prefix.Addr()
	} else {
		parsed, err := netip.ParseAddr(value)
		if err != nil || !parsed.Is4() {
			return "", false
		}
		addr = parsed
	}
	if !pool.Contains(addr) {
		return "", false
	}
	return netip.PrefixFrom(addr, 32).String(), true
}

func statusMapSlice(value any) []map[string]string {
	var out []map[string]string
	switch typed := value.(type) {
	case []map[string]string:
		return append([]map[string]string(nil), typed...)
	case []map[string]any:
		for _, item := range typed {
			out = append(out, statusAnyMapToStringMap(item))
		}
	case []any:
		for _, item := range typed {
			switch v := item.(type) {
			case map[string]string:
				out = append(out, v)
			case map[string]any:
				out = append(out, statusAnyMapToStringMap(v))
			}
		}
	}
	return out
}

func statusAnyMapToStringMap(values map[string]any) map[string]string {
	out := map[string]string{}
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "<nil>" {
			text = ""
		}
		out[key] = text
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortedVerdictKeys(values map[string]Verdict) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func hasCommunity(values []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}
