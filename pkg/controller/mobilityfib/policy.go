// SPDX-License-Identifier: BSD-3-Clause

// Package mobilityfib admits only the typed MobilityPool FIB plan produced by
// the mobility controller. It must not normalize MobilityPool configuration or
// recover desired state from status.
package mobilityfib

import (
	"net/netip"
	"sort"
	"strings"

	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

type Snapshot struct {
	pools           []poolSnapshot
	invalidPrefixes []netip.Prefix
}

type poolSnapshot struct {
	name            string
	prefix          netip.Prefix
	verdicts        map[string]Verdict
	remoteReturn    map[string]bool
	preferredSource netip.Prefix
}

type scopedPool struct {
	name            string
	prefix          netip.Prefix
	remoteReturn    map[string]bool
	preferredSource netip.Prefix
}

const (
	ActionDeliverRemote = "deliver-remote"
	ActionLocalRoute    = "local-route"
	ActionWithhold      = "withhold"
)

const (
	communityMobilityOwner          = bgpstate.MobilityCommunityOwner
	communityMobilityRoleOnPrem     = "64512:101"
	communityMobilityRoleCloud      = "64512:102"
	communityMobilitySourceObserved = "64512:110"
	communityMobilitySourceStatic   = "64512:111"
	communityMobilitySourceHandover = "64512:112"
	communityMobilityReturnRoute    = bgpstate.MobilityCommunityReturnRoute
	communityMobilityFailover       = "64512:120"
	communityMobilityActiveHolder   = "64512:121"
	communityMobilityNodeLiveness   = bgpstate.MobilityCommunityNodeLiveness
)

type Verdict struct {
	Address   string
	Action    string
	Class     string
	OwnerNode string
	Reason    string
}

// PreferredSource is a direct projection of a normalized FIBPoolScope. The
// BGP controller consumes it without reopening MobilityPool configuration.
type PreferredSource struct {
	PoolRef       string
	Prefix        netip.Prefix
	Address       string
	AddressPrefix string
}

// NewSnapshotFromVerdicts builds the BGP admission policy solely from the
// typed mobility plan. A valid Pool scope is mandatory: old or malformed
// unscoped mobility data is deliberately fail-closed rather than falling back
// to a second MobilityPool normalization.
func NewSnapshotFromVerdicts(verdicts []dynamicconfig.FIBVerdict) Snapshot {
	validatedVerdicts, invalidPrefixes := validatedMobilityFIBVerdicts(verdicts)
	scopes, scopeInvalidPrefixes := poolScopesFromVerdicts(validatedVerdicts)
	invalidPrefixes = appendInvalidPrefixes(invalidPrefixes, scopeInvalidPrefixes)
	if len(scopes) == 0 {
		return Snapshot{invalidPrefixes: invalidPrefixes}
	}
	pools := make([]poolSnapshot, 0, len(scopes))
	poolIndexes := make(map[string]int, len(scopes))
	for _, scope := range scopes {
		poolIndexes[scope.name] = len(pools)
		pools = append(pools, poolSnapshot{
			name:            scope.name,
			prefix:          scope.prefix,
			verdicts:        map[string]Verdict{},
			remoteReturn:    scope.remoteReturn,
			preferredSource: scope.preferredSource,
		})
	}
	invalidPools := map[string]bool{}
	for _, verdict := range validatedVerdicts {
		poolIndex, ok := poolIndexes[strings.TrimSpace(verdict.PoolRef)]
		if !ok || strings.TrimSpace(verdict.Address) == "" {
			continue
		}
		pool := &pools[poolIndex]
		address, ok := normalizePoolAddress(verdict.Address, pool.prefix)
		if !ok {
			continue
		}
		next := Verdict{
			Address:   address,
			Action:    strings.TrimSpace(verdict.Action),
			Class:     strings.TrimSpace(verdict.Class),
			OwnerNode: strings.TrimSpace(verdict.OwnerNode),
			Reason:    strings.TrimSpace(verdict.Reason),
		}
		if previous, found := pool.verdicts[address]; found && previous != next {
			invalidPools[pool.name] = true
			continue
		}
		pool.verdicts[address] = next
	}
	if len(invalidPools) > 0 {
		remaining := pools[:0]
		invalidByPrefix := map[string]netip.Prefix{}
		for _, prefix := range invalidPrefixes {
			invalidByPrefix[prefix.String()] = prefix
		}
		for _, pool := range pools {
			if invalidPools[pool.name] {
				invalidByPrefix[pool.prefix.String()] = pool.prefix
				continue
			}
			remaining = append(remaining, pool)
		}
		pools = remaining
		invalidPrefixes = invalidPrefixes[:0]
		for _, prefix := range invalidByPrefix {
			invalidPrefixes = append(invalidPrefixes, prefix)
		}
		sort.Slice(invalidPrefixes, func(i, j int) bool { return invalidPrefixes[i].String() < invalidPrefixes[j].String() })
	}
	return Snapshot{pools: pools, invalidPrefixes: invalidPrefixes}
}

// validatedMobilityFIBVerdicts keeps the in-process policy boundary as strict
// as the persistence decoder. Controllers normally validate one record before
// calling this package, but this second check ensures a direct caller cannot
// make NewSnapshotFromVerdicts silently normalize or partially use a malformed
// Pool's address table.
func validatedMobilityFIBVerdicts(verdicts []dynamicconfig.FIBVerdict) ([]dynamicconfig.FIBVerdict, []netip.Prefix) {
	byPool := map[string][]dynamicconfig.FIBVerdict{}
	order := make([]string, 0)
	knownPools := map[string]bool{}
	for _, verdict := range verdicts {
		name := verdict.PoolRef
		if name == "" || name != strings.TrimSpace(name) {
			continue
		}
		if _, found := byPool[name]; !found {
			order = append(order, name)
		}
		byPool[name] = append(byPool[name], verdict)
		if verdict.Scope != nil {
			if _, err := dynamicconfig.ParseCanonicalIPv4Prefix(verdict.Scope.Prefix); err == nil {
				knownPools[name] = true
			}
		}
	}
	invalidPools := map[string]bool{}
	for _, verdict := range verdicts {
		if verdict.PoolRef == strings.TrimSpace(verdict.PoolRef) {
			continue
		}
		if knownPools[strings.TrimSpace(verdict.PoolRef)] {
			invalidPools[strings.TrimSpace(verdict.PoolRef)] = true
		}
	}
	valid := make([]dynamicconfig.FIBVerdict, 0, len(verdicts))
	invalidPrefixes := []netip.Prefix{}
	for _, name := range order {
		poolVerdicts := byPool[name]
		if err := dynamicconfig.ValidateMobilityFIBVerdicts(poolVerdicts, name); err != nil {
			invalidPools[name] = true
		}
		if invalidPools[name] {
			invalidPrefixes = appendInvalidPrefixes(invalidPrefixes, fibScopePrefixes(poolVerdicts))
			continue
		}
		valid = append(valid, poolVerdicts...)
	}
	return valid, invalidPrefixes
}

func fibScopePrefixes(verdicts []dynamicconfig.FIBVerdict) []netip.Prefix {
	var prefixes []netip.Prefix
	for _, verdict := range verdicts {
		if verdict.Scope == nil {
			continue
		}
		prefix, err := dynamicconfig.ParseCanonicalIPv4Prefix(verdict.Scope.Prefix)
		if err == nil {
			prefixes = appendInvalidPrefixes(prefixes, []netip.Prefix{prefix})
		}
	}
	return prefixes
}

func appendInvalidPrefixes(existing, more []netip.Prefix) []netip.Prefix {
	byPrefix := make(map[string]netip.Prefix, len(existing)+len(more))
	for _, prefix := range existing {
		if prefix.IsValid() {
			byPrefix[prefix.String()] = prefix
		}
	}
	for _, prefix := range more {
		if prefix.IsValid() {
			byPrefix[prefix.String()] = prefix
		}
	}
	out := make([]netip.Prefix, 0, len(byPrefix))
	for _, prefix := range byPrefix {
		out = append(out, prefix)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func poolScopesFromVerdicts(verdicts []dynamicconfig.FIBVerdict) ([]scopedPool, []netip.Prefix) {
	byName := map[string]scopedPool{}
	invalidNames := map[string]bool{}
	invalidPrefixes := map[string]netip.Prefix{}
	for _, verdict := range verdicts {
		if verdict.Scope == nil {
			continue
		}
		name := strings.TrimSpace(verdict.PoolRef)
		scope, ok := parsePoolScope(name, *verdict.Scope)
		if !ok {
			if name != "" {
				invalidNames[name] = true
			}
			continue
		}
		if previous, found := byName[name]; found && !samePoolScope(previous, scope) {
			invalidNames[name] = true
			invalidPrefixes[previous.prefix.String()] = previous.prefix
			invalidPrefixes[scope.prefix.String()] = scope.prefix
			continue
		}
		byName[name] = scope
	}
	for name, scope := range byName {
		if invalidNames[name] {
			invalidPrefixes[scope.prefix.String()] = scope.prefix
			delete(byName, name)
		}
	}
	for _, scope := range byName {
		for _, other := range byName {
			if scope.name >= other.name || !scope.prefix.Overlaps(other.prefix) {
				continue
			}
			invalidNames[scope.name] = true
			invalidNames[other.name] = true
			invalidPrefixes[scope.prefix.String()] = scope.prefix
			invalidPrefixes[other.prefix.String()] = other.prefix
		}
	}
	for name, scope := range byName {
		if invalidNames[name] {
			invalidPrefixes[scope.prefix.String()] = scope.prefix
			delete(byName, name)
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	scopes := make([]scopedPool, 0, len(names))
	for _, name := range names {
		scopes = append(scopes, byName[name])
	}
	invalid := make([]netip.Prefix, 0, len(invalidPrefixes))
	for _, prefix := range invalidPrefixes {
		invalid = append(invalid, prefix)
	}
	sort.Slice(invalid, func(i, j int) bool { return invalid[i].String() < invalid[j].String() })
	return scopes, invalid
}

func parsePoolScope(name string, scope dynamicconfig.FIBPoolScope) (scopedPool, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return scopedPool{}, false
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(scope.Prefix))
	if err != nil || !prefix.Addr().Is4() {
		return scopedPool{}, false
	}
	prefix = prefix.Masked()
	remoteReturn := map[string]bool{}
	for _, community := range scope.RemoteReturnCommunities {
		community = strings.TrimSpace(community)
		if !bgpstate.IsMobilityNodeIdentityCommunity(community) {
			return scopedPool{}, false
		}
		remoteReturn[community] = true
	}
	preferredSource := netip.Prefix{}
	if raw := strings.TrimSpace(scope.PreferredSource); raw != "" {
		address, ok := normalizePoolAddress(raw, prefix)
		if !ok {
			return scopedPool{}, false
		}
		preferredSource, err = netip.ParsePrefix(address)
		if err != nil {
			return scopedPool{}, false
		}
	}
	return scopedPool{name: name, prefix: prefix, remoteReturn: remoteReturn, preferredSource: preferredSource}, true
}

func samePoolScope(a, b scopedPool) bool {
	if a.name != b.name || a.prefix != b.prefix || a.preferredSource != b.preferredSource || len(a.remoteReturn) != len(b.remoteReturn) {
		return false
	}
	for community := range a.remoteReturn {
		if !b.remoteReturn[community] {
			return false
		}
	}
	return true
}

func (s Snapshot) AdmitBGPPath(prefix netip.Prefix, communities []string) bool {
	prefix = prefix.Masked()
	if s.invalidFor(prefix) {
		return false
	}
	pool, ok := s.poolFor(prefix)
	if !ok {
		// Without a valid scope, an old FIB verdict cannot establish either
		// the Pool boundary or return-route topology. Fail only mobility-tagged
		// paths closed; ordinary BGP remains governed by its import policy.
		return !hasMobilityRoutingCommunity(communities)
	}
	if prefix.Bits() != 32 {
		return false
	}
	address := prefix.String()
	if hasCommunity(communities, communityMobilityReturnRoute) {
		return pool.admitPlannedReturnRoute(communities)
	}
	if verdict, ok := pool.verdicts[address]; ok {
		return strings.TrimSpace(verdict.Action) == ActionDeliverRemote
	}
	return false
}

func (s Snapshot) invalidFor(prefix netip.Prefix) bool {
	if !prefix.Addr().Is4() {
		return false
	}
	for _, invalid := range s.invalidPrefixes {
		if invalid.Contains(prefix.Addr()) {
			return true
		}
	}
	return false
}

func (p poolSnapshot) admitPlannedReturnRoute(communities []string) bool {
	for _, community := range communities {
		community = strings.TrimSpace(community)
		if p.remoteReturn[community] {
			return true
		}
	}
	return false
}

func (s Snapshot) PreferredSources() []PreferredSource {
	out := make([]PreferredSource, 0, len(s.pools))
	for _, pool := range s.pools {
		if !pool.preferredSource.IsValid() {
			continue
		}
		out = append(out, PreferredSource{
			PoolRef:       pool.name,
			Prefix:        pool.prefix,
			Address:       pool.preferredSource.Addr().String(),
			AddressPrefix: pool.preferredSource.String(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Prefix.Bits() != out[j].Prefix.Bits() {
			return out[i].Prefix.Bits() > out[j].Prefix.Bits()
		}
		if out[i].Prefix != out[j].Prefix {
			return out[i].Prefix.String() < out[j].Prefix.String()
		}
		return out[i].PoolRef < out[j].PoolRef
	})
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

func hasMobilityRoutingCommunity(values []string) bool {
	for _, value := range values {
		switch strings.TrimSpace(value) {
		case communityMobilityOwner,
			communityMobilityRoleOnPrem,
			communityMobilityRoleCloud,
			communityMobilitySourceObserved,
			communityMobilitySourceStatic,
			communityMobilitySourceHandover,
			communityMobilityReturnRoute,
			communityMobilityFailover,
			communityMobilityActiveHolder,
			communityMobilityNodeLiveness:
			return true
		}
	}
	return false
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
