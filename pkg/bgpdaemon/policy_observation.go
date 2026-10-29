// SPDX-License-Identifier: BSD-3-Clause

package bgpdaemon

import (
	gobgpapi "github.com/osrg/gobgp/v4/api"
	"reflect"
	"sort"
	"strings"
)

type canonicalDefinedSet struct {
	Name     string
	Type     int32
	Prefixes []canonicalPrefix
	List     []string
}

type canonicalPrefix struct {
	Prefix string
	Min    uint32
	Max    uint32
}

type canonicalPolicy struct {
	Name       string
	Statements []canonicalStatement
}

type canonicalStatement struct {
	Name             string
	PrefixSetName    string
	PrefixSetType    int32
	NeighborSetName  string
	NeighborSetType  int32
	CommunitySetName string
	CommunitySetType int32
	RouteType        int32
	RouteAction      int32
	NextHop          string
	LocalPreference  *uint32
}

type canonicalPolicyAssignment struct {
	Name          string
	Direction     int32
	DefaultAction int32
	Policies      []string
}

func canonicalizeDefinedSet(set *gobgpapi.DefinedSet) canonicalDefinedSet {
	if set == nil {
		return canonicalDefinedSet{}
	}
	out := canonicalDefinedSet{Name: strings.TrimSpace(set.GetName()), Type: int32(set.GetDefinedType())}
	for _, prefix := range set.GetPrefixes() {
		out.Prefixes = append(out.Prefixes, canonicalPrefix{
			Prefix: strings.TrimSpace(prefix.GetIpPrefix()),
			Min:    prefix.GetMaskLengthMin(),
			Max:    prefix.GetMaskLengthMax(),
		})
	}
	out.List = append(out.List, set.GetList()...)
	sort.Strings(out.List)
	sort.Slice(out.Prefixes, func(i, j int) bool {
		if out.Prefixes[i].Prefix != out.Prefixes[j].Prefix {
			return out.Prefixes[i].Prefix < out.Prefixes[j].Prefix
		}
		if out.Prefixes[i].Min != out.Prefixes[j].Min {
			return out.Prefixes[i].Min < out.Prefixes[j].Min
		}
		return out.Prefixes[i].Max < out.Prefixes[j].Max
	})
	return out
}

func canonicalizePolicy(policy *gobgpapi.Policy) canonicalPolicy {
	if policy == nil {
		return canonicalPolicy{}
	}
	out := canonicalPolicy{Name: strings.TrimSpace(policy.GetName())}
	for _, statement := range policy.GetStatements() {
		prefixSet := statement.GetConditions().GetPrefixSet()
		neighborSet := statement.GetConditions().GetNeighborSet()
		communitySet := statement.GetConditions().GetCommunitySet()
		out.Statements = append(out.Statements, canonicalStatement{
			Name:             strings.TrimSpace(statement.GetName()),
			PrefixSetName:    strings.TrimSpace(prefixSet.GetName()),
			PrefixSetType:    int32(prefixSet.GetType()),
			NeighborSetName:  strings.TrimSpace(neighborSet.GetName()),
			NeighborSetType:  int32(neighborSet.GetType()),
			CommunitySetName: strings.TrimSpace(communitySet.GetName()),
			CommunitySetType: int32(communitySet.GetType()),
			RouteType:        int32(statement.GetConditions().GetRouteType()),
			RouteAction:      int32(statement.GetActions().GetRouteAction()),
			NextHop:          canonicalNextHopAction(statement.GetActions().GetNexthop()),
			LocalPreference:  canonicalLocalPreferenceAction(statement.GetActions().GetLocalPref()),
		})
	}
	return out
}

func canonicalNextHopAction(action *gobgpapi.NexthopAction) string {
	switch {
	case action == nil:
		return ""
	case action.GetPeerAddress():
		return "peer-address"
	case action.GetUnchanged():
		return "unchanged"
	default:
		return ""
	}
}

func canonicalLocalPreferenceAction(action *gobgpapi.LocalPrefAction) *uint32 {
	if action == nil {
		return nil
	}
	value := action.GetValue()
	return &value
}

func canonicalizePolicyAssignment(assignment *gobgpapi.PolicyAssignment) canonicalPolicyAssignment {
	if assignment == nil {
		return canonicalPolicyAssignment{}
	}
	out := canonicalPolicyAssignment{
		Name:          strings.TrimSpace(assignment.GetName()),
		Direction:     int32(assignment.GetDirection()),
		DefaultAction: int32(assignment.GetDefaultAction()),
	}
	for _, policy := range assignment.GetPolicies() {
		if name := strings.TrimSpace(policy.GetName()); name != "" {
			out.Policies = append(out.Policies, name)
		}
	}
	return out
}

func EqualPolicy(a, b *gobgpapi.Policy) bool {
	return reflect.DeepEqual(canonicalizePolicy(a), canonicalizePolicy(b))
}
func EqualDefinedSet(a, b *gobgpapi.DefinedSet) bool {
	return reflect.DeepEqual(canonicalizeDefinedSet(a), canonicalizeDefinedSet(b))
}
func EqualPolicyAssignment(a, b *gobgpapi.PolicyAssignment) bool {
	return reflect.DeepEqual(canonicalizePolicyAssignment(a), canonicalizePolicyAssignment(b))
}
