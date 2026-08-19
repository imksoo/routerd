// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
)

type mobilityMembersFromStatus struct {
	Resource    string `json:"resource"`
	Optional    bool   `json:"optional,omitempty"`
	Phase       string `json:"phase"`
	MemberCount int    `json:"memberCount,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type mobilityMembersResolution struct {
	Members             []api.ResolvedMobilityPoolMember
	MembersFrom         []mobilityMembersFromStatus
	PendingSources      []string
	ResolvedMemberCount int
}

// resolvedMobilityPool retains source-resolution status in the controller
// shell while Pool is the only identity/topology input to planning.
type resolvedMobilityPool struct {
	Pool     NormalizedMobilityPool
	Resolved mobilityMembersResolution
}

func resolveNormalizedMobilityPool(router *api.Router, spec api.MobilityPoolSpec) (resolvedMobilityPool, error) {
	selfNode, err := api.EventGroupSelfNode(router, spec.GroupRef)
	if err != nil {
		return resolvedMobilityPool{}, err
	}
	resolved, err := (mobilityMemberResolver{Router: router}).resolve(spec)
	if err != nil {
		return resolvedMobilityPool{}, err
	}
	result := resolvedMobilityPool{
		Pool:     NormalizedMobilityPool{Spec: spec, SelfNode: selfNode},
		Resolved: resolved,
	}
	if len(resolved.PendingSources) > 0 {
		return result, nil
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(result.Pool.Spec.Prefix))
	if err != nil {
		return resolvedMobilityPool{}, fmt.Errorf("parse MobilityPool prefix: %w", err)
	}
	result.Pool.Prefix = prefix.Masked()
	if err := validateResolvedMobilityMembers(resolved.Members); err != nil {
		return resolvedMobilityPool{}, err
	}
	if err := mobilityconfig.RequireResolvedMobilityPoolSelfMember(resolved.Members, selfNode); err != nil {
		return resolvedMobilityPool{}, err
	}
	normalizedMembers, err := mobilityconfig.NormalizeResolvedMobilityPoolMembers(spec, resolved.Members, selfNode)
	if err != nil {
		return resolvedMobilityPool{}, err
	}
	result.Pool.Members = plannerMembers(normalizedMembers)
	self, ok := lookupMemberByNodeRef(result.Pool.Members, selfNode)
	if !ok {
		return resolvedMobilityPool{}, fmt.Errorf("self node %q is not a member of MobilityPool/%s", selfNode, strings.TrimSpace(result.Pool.Spec.Prefix))
	}
	result.Pool.Self = self
	result.Pool.SelfNode = result.Pool.Self.NodeRef
	return result, nil
}

type mobilityMemberResolver struct {
	Router *api.Router
}

func (r mobilityMemberResolver) resolve(spec api.MobilityPoolSpec) (mobilityMembersResolution, error) {
	resolved, err := mobilityconfig.ResolveMobilityPoolMembers(r.Router, spec)
	statuses := make([]mobilityMembersFromStatus, 0, len(resolved.Sources))
	pending := []string{}
	for _, source := range resolved.Sources {
		status := mobilityMembersFromStatus{
			Resource:    source.Resource,
			Optional:    source.Optional,
			Phase:       "Resolved",
			MemberCount: source.MemberCount,
		}
		if source.Err != nil {
			status.Phase = "Invalid"
			status.Reason = source.Err.Error()
			statuses = append(statuses, status)
			return mobilityMembersResolution{Members: resolved.Members, MembersFrom: statuses, PendingSources: pending}, source.Err
		}
		if !source.Found {
			status.Phase = "Missing"
			status.Reason = "SAMNodeSet not found"
			statuses = append(statuses, status)
			if !source.Optional {
				pending = append(pending, source.Resource)
			}
			continue
		}
		statuses = append(statuses, status)
	}
	if err != nil {
		return mobilityMembersResolution{Members: resolved.Members, MembersFrom: statuses, PendingSources: pending}, err
	}
	sort.Strings(pending)
	return mobilityMembersResolution{
		Members:             resolved.Members,
		MembersFrom:         statuses,
		PendingSources:      pending,
		ResolvedMemberCount: len(resolved.Members),
	}, nil
}

func validateResolvedMobilityMembers(members []api.ResolvedMobilityPoolMember) error {
	seen := map[string]bool{}
	for _, member := range members {
		nodeRef := strings.TrimSpace(member.NodeRef)
		if nodeRef == "" {
			return fmt.Errorf("resolved MobilityPool member nodeRef is required")
		}
		if seen[nodeRef] {
			return fmt.Errorf("resolved MobilityPool member nodeRef %q is duplicated", nodeRef)
		}
		seen[nodeRef] = true
		if strings.TrimSpace(member.Site) == "" {
			return fmt.Errorf("resolved MobilityPool member %q site is required", nodeRef)
		}
		switch strings.TrimSpace(member.Role) {
		case "onprem", "cloud":
		default:
			return fmt.Errorf("resolved MobilityPool member %q role must be onprem or cloud", nodeRef)
		}
	}
	return nil
}
