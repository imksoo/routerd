// SPDX-License-Identifier: BSD-3-Clause

package bgpdaemon

import (
	gobgpapi "github.com/osrg/gobgp/v4/api"
	"reflect"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"
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
	Name       string
	Conditions *gobgpapi.Conditions
	Actions    *gobgpapi.Actions
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
		conditions, actions := statement.GetConditions(), statement.GetActions()
		// GoBGP may allocate an empty top-level message when there is no
		// condition/action. Preserve every populated native field, including
		// fields routerd does not currently generate.
		if proto.Equal(conditions, &gobgpapi.Conditions{}) {
			conditions = nil
		}
		if proto.Equal(actions, &gobgpapi.Actions{}) {
			actions = nil
		}
		out.Statements = append(out.Statements, canonicalStatement{
			Name:       strings.TrimSpace(statement.GetName()),
			Conditions: conditions,
			Actions:    actions,
		})
	}
	return out
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
	x, y := canonicalizePolicy(a), canonicalizePolicy(b)
	if x.Name != y.Name || len(x.Statements) != len(y.Statements) {
		return false
	}
	for i, statement := range x.Statements {
		other := y.Statements[i]
		if statement.Name != other.Name || !proto.Equal(statement.Conditions, other.Conditions) || !proto.Equal(statement.Actions, other.Actions) {
			return false
		}
	}
	return true
}
func EqualDefinedSet(a, b *gobgpapi.DefinedSet) bool {
	return reflect.DeepEqual(canonicalizeDefinedSet(a), canonicalizeDefinedSet(b))
}
func EqualPolicyAssignment(a, b *gobgpapi.PolicyAssignment) bool {
	return reflect.DeepEqual(canonicalizePolicyAssignment(a), canonicalizePolicyAssignment(b))
}
