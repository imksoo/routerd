// SPDX-License-Identifier: BSD-3-Clause

package bgpdaemon

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/imksoo/routerd/internal/stringutil"
	routerapi "github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	gobgpapi "github.com/osrg/gobgp/v4/api"
)

// PolicyIntent is the complete configured policy boundary. Local path facts are
// supplied by the daemon's journal, rather than copied into this static intent.
type PolicyIntent struct {
	RouterName string        `json:"routerName"`
	Scopes     []PolicyScope `json:"scopes,omitempty"`
}

type PolicyScope struct {
	Name                   string                        `json:"name"`
	Neighbors              []string                      `json:"neighbors"`
	Spec                   routerapi.BGPImportPolicySpec `json:"importPolicy,omitempty"`
	PreserveImportPrefixes bool                          `json:"preserveImportPrefixes,omitempty"`
	RejectImportAll        bool                          `json:"rejectImportAll,omitempty"`
	ExportPolicyName       string                        `json:"exportPolicyName"`
	ExportPolicy           routerapi.BGPExportPolicySpec `json:"exportPolicy,omitempty"`
}

const PolicyGuardName = "routerd-policy-update-guard"

type PolicyPlan struct {
	SetPolicies            *gobgpapi.SetPoliciesRequest
	GlobalImportAssignment *gobgpapi.PolicyAssignment
	GlobalExportAssignment *gobgpapi.PolicyAssignment
	ImportPolicyNames      map[string]bool
}

func PolicyGuard() *gobgpapi.Policy {
	return &gobgpapi.Policy{Name: PolicyGuardName, Statements: []*gobgpapi.Statement{{Name: PolicyGuardName + "-reject", Actions: &gobgpapi.Actions{RouteAction: gobgpapi.RouteAction_ROUTE_ACTION_REJECT}}}}
}

func BuildPolicyPlan(intent PolicyIntent, paths []AppliedPath) PolicyPlan {
	intent = NormalizePolicyIntent(intent)
	req := &gobgpapi.SetPoliciesRequest{Policies: []*gobgpapi.Policy{PolicyGuard()}}
	var dynamic []string
	for _, path := range paths {
		if IsMobilityPathSource(path.Source) {
			dynamic = append(dynamic, path.Prefix)
		}
	}
	var imports []PolicyScope
	exportAssignment := &gobgpapi.PolicyAssignment{Name: "global", Direction: gobgpapi.PolicyDirection_POLICY_DIRECTION_EXPORT, DefaultAction: gobgpapi.RouteAction_ROUTE_ACTION_REJECT}
	for _, configured := range intent.Scopes {
		scope := configured
		scope.Spec.AllowedPrefixes = append([]string(nil), scope.Spec.AllowedPrefixes...)
		if !scope.PreserveImportPrefixes && peerHasImportPolicy(scope.Spec) {
			scope.Spec.AllowedPrefixes = stringutil.UniqueTrimmedSorted(append(scope.Spec.AllowedPrefixes, dynamic...))
		}
		imports = append(imports, scope)
		prefixes := exportPolicyPrefixes(routerapi.BGPExportPolicySpec{AllowedPrefixes: stringutil.UniqueTrimmedSorted(append(append([]string(nil), scope.ExportPolicy.AllowedPrefixes...), dynamic...))})
		if scope.ExportPolicyName == "" {
			continue
		}
		name := scope.ExportPolicyName
		policy := &gobgpapi.Policy{Name: name}
		neighbor := func() *gobgpapi.MatchSet {
			return &gobgpapi.MatchSet{Type: gobgpapi.MatchSet_TYPE_ANY, Name: scope.Name + "-neighbors"}
		}
		prefixSetName := name + "-prefixes"
		for _, set := range bgpPolicyPrefixSets(prefixSetName, prefixes) {
			req.DefinedSets = append(req.DefinedSets, set)
			suffix := strings.TrimPrefix(set.Name, prefixSetName)
			policy.Statements = append(policy.Statements, &gobgpapi.Statement{Name: bgpPolicyStatementName(name, "allow-export"+suffix), Conditions: &gobgpapi.Conditions{NeighborSet: neighbor(), PrefixSet: &gobgpapi.MatchSet{Type: gobgpapi.MatchSet_TYPE_ANY, Name: set.Name}}, Actions: &gobgpapi.Actions{RouteAction: gobgpapi.RouteAction_ROUTE_ACTION_ACCEPT}})
		}
		policy.Statements = append(policy.Statements, &gobgpapi.Statement{Name: bgpPolicyStatementName(name, "reject-unmatched-export"), Conditions: &gobgpapi.Conditions{NeighborSet: neighbor()}, Actions: &gobgpapi.Actions{RouteAction: gobgpapi.RouteAction_ROUTE_ACTION_REJECT}})
		req.Policies = append(req.Policies, policy)
		exportAssignment.Policies = append(exportAssignment.Policies, &gobgpapi.Policy{Name: name})
	}
	name := "routerd-" + stringutil.ConservativeName(intent.RouterName, "default") + "-import-effective"
	appendEffectiveImportPolicy(req, name, imports)
	req.Policies[len(req.Policies)-1].Statements = append(req.Policies[len(req.Policies)-1].Statements, &gobgpapi.Statement{Name: name + "-allow-local", Conditions: &gobgpapi.Conditions{RouteType: gobgpapi.Conditions_ROUTE_TYPE_LOCAL}, Actions: &gobgpapi.Actions{RouteAction: gobgpapi.RouteAction_ROUTE_ACTION_ACCEPT}})
	assignment := globalImportPolicyAssignment(name, true)
	assignment.DefaultAction = gobgpapi.RouteAction_ROUTE_ACTION_REJECT
	return PolicyPlan{SetPolicies: req, GlobalImportAssignment: assignment, GlobalExportAssignment: exportAssignment, ImportPolicyNames: map[string]bool{name: true}}
}

func ValidatePolicyIntent(intent PolicyIntent) error {
	if strings.TrimSpace(intent.RouterName) == "" {
		return fmt.Errorf("BGP policy router name is required")
	}
	names, exports := map[string]bool{}, map[string]bool{}
	for _, scope := range intent.Scopes {
		name, exportName := strings.TrimSpace(scope.Name), strings.TrimSpace(scope.ExportPolicyName)
		if name == "" || names[name] {
			return fmt.Errorf("BGP policy scope name %q is empty or duplicated", scope.Name)
		}
		names[name] = true
		if len(scope.Neighbors) == 0 {
			return fmt.Errorf("BGP policy scope %s has no neighbors", scope.Name)
		}
		for _, value := range scope.Neighbors {
			if _, err := netip.ParsePrefix(strings.TrimSpace(value)); err != nil {
				return fmt.Errorf("BGP policy scope %s neighbor %q: %w", scope.Name, value, err)
			}
		}
		for _, value := range append(append([]string(nil), scope.Spec.AllowedPrefixes...), scope.ExportPolicy.AllowedPrefixes...) {
			_, err := netip.ParsePrefix(strings.TrimSpace(value))
			if err != nil {
				return fmt.Errorf("BGP policy scope %s prefix %q is invalid", scope.Name, value)
			}
		}
		minLen, maxLen := scope.Spec.AllowedPrefixLengthMin, scope.Spec.AllowedPrefixLengthMax
		if minLen < 0 || maxLen < 0 || minLen > 128 || maxLen > 128 || (minLen > 0 && maxLen > 0 && minLen > maxLen) {
			return fmt.Errorf("BGP policy scope %s prefix lengths are invalid", scope.Name)
		}
		if len(scope.Spec.AllowedPrefixes) == 0 && (minLen != 0 || maxLen != 0) {
			return fmt.Errorf("BGP policy scope %s allowedPrefixes is required when prefix length bounds are configured", scope.Name)
		}
		for _, value := range scope.Spec.AllowedPrefixes {
			prefix, _ := netip.ParsePrefix(strings.TrimSpace(value))
			if minLen > prefix.Addr().BitLen() || maxLen > prefix.Addr().BitLen() || (minLen > 0 && minLen < prefix.Bits()) || (maxLen > 0 && maxLen < prefix.Bits()) {
				return fmt.Errorf("BGP policy scope %s prefix lengths are invalid", scope.Name)
			}
		}
		rewrite := strings.TrimSpace(scope.Spec.NextHopRewrite)
		if rewrite != "" && rewrite != "peer-address" && rewrite != "unchanged" {
			return fmt.Errorf("BGP policy scope %s next hop rewrite is unsupported", scope.Name)
		}
		if _, err := bgpstate.StandardCommunities(append(append([]string(nil), scope.Spec.RequiredCommunities...), scope.Spec.ForbiddenCommunities...)); err != nil {
			return fmt.Errorf("BGP policy scope %s communities: %w", scope.Name, err)
		}
		if exportName == "" || exports[exportName] {
			return fmt.Errorf("BGP export policy name %q is empty or duplicated", scope.ExportPolicyName)
		}
		exports[exportName] = true
	}
	return nil
}

func NormalizePolicyIntent(intent PolicyIntent) PolicyIntent {
	out := PolicyIntent{RouterName: strings.TrimSpace(intent.RouterName), Scopes: make([]PolicyScope, len(intent.Scopes))}
	normalizePrefixes := func(values []string) []string {
		out := make([]string, 0, len(values))
		for _, value := range values {
			value = strings.TrimSpace(value)
			if prefix, err := netip.ParsePrefix(value); err == nil {
				value = prefix.Masked().String()
			}
			out = append(out, value)
		}
		return stringutil.UniqueTrimmedSorted(out)
	}
	for i, scope := range intent.Scopes {
		scope.Name = strings.TrimSpace(scope.Name)
		scope.Neighbors = normalizePrefixes(scope.Neighbors)
		scope.Spec.AllowedPrefixes = normalizePrefixes(scope.Spec.AllowedPrefixes)
		scope.Spec.RequiredCommunities = stringutil.UniqueTrimmedSorted(scope.Spec.RequiredCommunities)
		scope.Spec.ForbiddenCommunities = stringutil.UniqueTrimmedSorted(scope.Spec.ForbiddenCommunities)
		scope.Spec.NextHopRewrite = strings.TrimSpace(scope.Spec.NextHopRewrite)
		scope.ExportPolicyName = strings.TrimSpace(scope.ExportPolicyName)
		scope.ExportPolicy.AllowedPrefixes = normalizePrefixes(scope.ExportPolicy.AllowedPrefixes)
		out.Scopes[i] = scope
	}
	return out
}

func appendEffectiveImportPolicy(req *gobgpapi.SetPoliciesRequest, policyName string, scopes []PolicyScope) {
	statements := make([]*gobgpapi.Statement, 0, len(scopes)*3)
	for _, scope := range scopes {
		neighborSetName := scope.Name + "-neighbors"
		req.DefinedSets = append(req.DefinedSets, &gobgpapi.DefinedSet{
			DefinedType: gobgpapi.DefinedType_DEFINED_TYPE_NEIGHBOR,
			Name:        neighborSetName,
			List:        append([]string(nil), scope.Neighbors...),
		})
		statements = append(statements, effectiveImportPolicyStatements(req, scope, neighborSetName)...)
	}
	req.Policies = append(req.Policies, &gobgpapi.Policy{Name: policyName, Statements: statements})
}

func effectiveImportPolicyStatements(req *gobgpapi.SetPoliciesRequest, scope PolicyScope, neighborSetName string) []*gobgpapi.Statement {
	prefixes := importPolicyPrefixes(scope.Spec)
	prefixSetName := scope.Name + "-prefixes"
	prefixSetNames := []string{""}
	if sets := bgpPolicyPrefixSets(prefixSetName, prefixes); len(sets) > 0 {
		req.DefinedSets = append(req.DefinedSets, sets...)
		prefixSetNames = nil
		for _, set := range sets {
			prefixSetNames = append(prefixSetNames, set.Name)
		}
	}
	requiredSetName := scope.Name + "-required-communities"
	requiredCommunities := cleanCommunityPolicyValues(scope.Spec.RequiredCommunities)
	if len(requiredCommunities) > 0 {
		req.DefinedSets = append(req.DefinedSets, &gobgpapi.DefinedSet{
			DefinedType: gobgpapi.DefinedType_DEFINED_TYPE_COMMUNITY,
			Name:        requiredSetName,
			List:        requiredCommunities,
		})
	}
	forbiddenSetName := scope.Name + "-forbidden-communities"
	forbiddenCommunities := cleanCommunityPolicyValues(scope.Spec.ForbiddenCommunities)
	if len(forbiddenCommunities) > 0 {
		req.DefinedSets = append(req.DefinedSets, &gobgpapi.DefinedSet{
			DefinedType: gobgpapi.DefinedType_DEFINED_TYPE_COMMUNITY,
			Name:        forbiddenSetName,
			List:        forbiddenCommunities,
		})
	}
	neighborSet := func() *gobgpapi.MatchSet {
		return &gobgpapi.MatchSet{Type: gobgpapi.MatchSet_TYPE_ANY, Name: neighborSetName}
	}
	if scope.RejectImportAll {
		return []*gobgpapi.Statement{{
			Name:       bgpPolicyStatementName(scope.Name, "reject-all-import"),
			Conditions: &gobgpapi.Conditions{NeighborSet: neighborSet()},
			Actions:    &gobgpapi.Actions{RouteAction: gobgpapi.RouteAction_ROUTE_ACTION_REJECT},
		}}
	}
	var statements []*gobgpapi.Statement
	if len(forbiddenCommunities) > 0 {
		statements = append(statements, &gobgpapi.Statement{
			Name: bgpPolicyStatementName(scope.Name, "reject-forbidden-community"),
			Conditions: &gobgpapi.Conditions{
				NeighborSet:  neighborSet(),
				CommunitySet: &gobgpapi.MatchSet{Type: gobgpapi.MatchSet_TYPE_ANY, Name: forbiddenSetName},
			},
			Actions: &gobgpapi.Actions{RouteAction: gobgpapi.RouteAction_ROUTE_ACTION_REJECT},
		})
	}
	for _, setName := range prefixSetNames {
		acceptConditions := &gobgpapi.Conditions{NeighborSet: neighborSet()}
		if setName != "" {
			acceptConditions.PrefixSet = &gobgpapi.MatchSet{Type: gobgpapi.MatchSet_TYPE_ANY, Name: setName}
		}
		if len(requiredCommunities) > 0 {
			acceptConditions.CommunitySet = &gobgpapi.MatchSet{Type: gobgpapi.MatchSet_TYPE_ALL, Name: requiredSetName}
		}
		suffix := strings.TrimPrefix(setName, prefixSetName)
		statements = append(statements, &gobgpapi.Statement{
			Name:       bgpPolicyStatementName(scope.Name, "allow-import"+suffix),
			Conditions: acceptConditions,
			Actions: &gobgpapi.Actions{
				RouteAction: gobgpapi.RouteAction_ROUTE_ACTION_ACCEPT,
				Nexthop:     nextHopRewriteAction(scope.Spec),
				LocalPref:   importLocalPreferenceAction(scope.Spec),
			},
		})
	}
	statements = append(statements, &gobgpapi.Statement{
		Name:       bgpPolicyStatementName(scope.Name, "reject-unmatched-import"),
		Conditions: &gobgpapi.Conditions{NeighborSet: neighborSet()},
		Actions:    &gobgpapi.Actions{RouteAction: gobgpapi.RouteAction_ROUTE_ACTION_REJECT},
	})
	return statements
}

func importPolicyLengthBounds(spec routerapi.BGPImportPolicySpec, prefix netip.Prefix) (int, int) {
	minLen := prefix.Bits()
	maxLen := int(bgpPrefixMaxLength(prefix))
	if spec.AllowedPrefixLengthMin > 0 {
		minLen = spec.AllowedPrefixLengthMin
	}
	if spec.AllowedPrefixLengthMax > 0 {
		maxLen = spec.AllowedPrefixLengthMax
	}
	return minLen, maxLen
}

func importNextHopRewrite(spec routerapi.BGPImportPolicySpec) string {
	switch strings.TrimSpace(spec.NextHopRewrite) {
	case "unchanged":
		return "unchanged"
	default:
		return "peer-address"
	}
}

func nextHopRewriteAction(spec routerapi.BGPImportPolicySpec) *gobgpapi.NexthopAction {
	if !peerHasImportPolicy(spec) {
		return nil
	}
	if importNextHopRewrite(spec) == "unchanged" {
		return &gobgpapi.NexthopAction{Unchanged: true}
	}
	return &gobgpapi.NexthopAction{PeerAddress: true}
}

func importLocalPreferenceAction(spec routerapi.BGPImportPolicySpec) *gobgpapi.LocalPrefAction {
	if spec.LocalPreference == 0 {
		return nil
	}
	return &gobgpapi.LocalPrefAction{Value: spec.LocalPreference}
}

func importPolicyPrefixes(spec routerapi.BGPImportPolicySpec) []*gobgpapi.Prefix {
	return bgpImportPolicyPrefixes(spec)
}

func exportPolicyPrefixes(spec routerapi.BGPExportPolicySpec) []*gobgpapi.Prefix {
	return bgpImportPolicyPrefixes(routerapi.BGPImportPolicySpec{AllowedPrefixes: spec.AllowedPrefixes})
}

// GoBGP prefix sets contain exactly one address family. Keep single-family
// names unchanged; mixed policies get one accept statement for each set.
func bgpPolicyPrefixSets(name string, prefixes []*gobgpapi.Prefix) []*gobgpapi.DefinedSet {
	var ipv4, ipv6 []*gobgpapi.Prefix
	for _, prefix := range prefixes {
		parsed, err := netip.ParsePrefix(prefix.GetIpPrefix())
		if err != nil {
			continue
		}
		if parsed.Addr().Is4() {
			ipv4 = append(ipv4, prefix)
		} else {
			ipv6 = append(ipv6, prefix)
		}
	}
	if len(ipv4) == 0 && len(ipv6) == 0 {
		return nil
	}
	set := func(name string, values []*gobgpapi.Prefix) *gobgpapi.DefinedSet {
		return &gobgpapi.DefinedSet{DefinedType: gobgpapi.DefinedType_DEFINED_TYPE_PREFIX, Name: name, Prefixes: values}
	}
	if len(ipv4) == 0 {
		return []*gobgpapi.DefinedSet{set(name, ipv6)}
	}
	if len(ipv6) == 0 {
		return []*gobgpapi.DefinedSet{set(name, ipv4)}
	}
	return []*gobgpapi.DefinedSet{set(name+"-ipv4", ipv4), set(name+"-ipv6", ipv6)}
}

func bgpImportPolicyPrefixes(spec routerapi.BGPImportPolicySpec) []*gobgpapi.Prefix {
	var out []*gobgpapi.Prefix
	for _, value := range spec.AllowedPrefixes {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		minLen, maxLen := importPolicyLengthBounds(spec, prefix)
		out = append(out, &gobgpapi.Prefix{
			IpPrefix:      prefix.String(),
			MaskLengthMin: uint32(minLen),
			MaskLengthMax: uint32(maxLen),
		})
	}
	return out
}

func bgpPrefixMaxLength(prefix netip.Prefix) uint32 {
	if prefix.Addr().Is6() {
		return 128
	}
	return 32
}

func bgpPolicyStatementName(policyName, suffix string) string {
	return strings.TrimSpace(policyName) + "-" + suffix
}

func cleanCommunityPolicyValues(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if decoded, err := bgpstate.StandardCommunities([]string{value}); err == nil && len(decoded) == 1 {
			value = fmt.Sprintf("^%d:%d$", decoded[0]>>16, decoded[0]&0xffff)
		}
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func globalImportPolicyAssignment(policyName string, includePolicy bool) *gobgpapi.PolicyAssignment {
	assignment := &gobgpapi.PolicyAssignment{
		Name:          "global",
		Direction:     gobgpapi.PolicyDirection_POLICY_DIRECTION_IMPORT,
		DefaultAction: gobgpapi.RouteAction_ROUTE_ACTION_ACCEPT,
	}
	if includePolicy && strings.TrimSpace(policyName) != "" {
		assignment.Policies = []*gobgpapi.Policy{{Name: strings.TrimSpace(policyName)}}
	}
	return assignment
}

func peerHasImportPolicy(spec routerapi.BGPImportPolicySpec) bool {
	return len(stringutil.UniqueTrimmedSorted(spec.AllowedPrefixes)) > 0 ||
		len(stringutil.UniqueTrimmedSorted(spec.RequiredCommunities)) > 0 ||
		len(stringutil.UniqueTrimmedSorted(spec.ForbiddenCommunities)) > 0 ||
		strings.TrimSpace(spec.NextHopRewrite) != "" ||
		spec.LocalPreference != 0
}
