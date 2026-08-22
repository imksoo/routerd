// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"fmt"
	"strings"
)

// LookupEventGroup resolves a local EventGroup by its group name. EventGroup
// identity is shared by validation, mobility planning, diagnostics, and
// graceful stop; keep the resource lookup in one strict API boundary.
func LookupEventGroup(router *Router, groupRef string) (EventGroupSpec, bool, error) {
	groupRef = strings.TrimSpace(groupRef)
	if groupRef == "" {
		return EventGroupSpec{}, false, fmt.Errorf("groupRef is required")
	}
	if router == nil {
		return EventGroupSpec{}, false, nil
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != FederationAPIVersion || resource.Kind != "EventGroup" || strings.TrimSpace(resource.Metadata.Name) != groupRef {
			continue
		}
		spec, err := resource.EventGroupSpec()
		if err != nil {
			return EventGroupSpec{}, true, err
		}
		return spec, true, nil
	}
	return EventGroupSpec{}, false, nil
}

// EventGroupSelfNode returns the exact local node identity for a group. It
// intentionally has no site or suffix fallback: a MobilityPool must name the
// same SAMNodeSet node that owns the local controller.
func EventGroupSelfNode(router *Router, groupRef string) (string, error) {
	spec, found, err := LookupEventGroup(router, groupRef)
	if err != nil {
		return "", err
	}
	groupRef = strings.TrimSpace(groupRef)
	if !found {
		return "", fmt.Errorf("EventGroup/%s not found", groupRef)
	}
	node := strings.TrimSpace(spec.NodeName)
	if node == "" {
		return "", fmt.Errorf("EventGroup/%s spec.nodeName is required", groupRef)
	}
	return node, nil
}

func lookupMobilityResource(router *Router, ref, expectedKind, field string) (Resource, bool, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != expectedKind || strings.TrimSpace(name) == "" {
		return Resource{}, false, fmt.Errorf("%s resource must reference %s/<name>", field, expectedKind)
	}
	if router == nil {
		return Resource{}, false, nil
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == MobilityAPIVersion && resource.Kind == expectedKind && resource.Metadata.Name == strings.TrimSpace(name) {
			return resource, true, nil
		}
	}
	return Resource{}, false, nil
}

func lookupMobilitySpec[T any](router *Router, ref, expectedKind, field string, decode func(Resource) (T, error)) (T, bool, error) {
	var zero T
	resource, found, err := lookupMobilityResource(router, ref, expectedKind, field)
	if err != nil || !found {
		return zero, found, err
	}
	spec, err := decode(resource)
	if err != nil {
		return zero, true, fmt.Errorf("%s spec: %w", ref, err)
	}
	return spec, true, nil
}

// LookupSAMRRSet resolves a SAMRRSet reference and decodes its spec.
func LookupSAMRRSet(router *Router, ref, field string) (SAMRRSetSpec, bool, error) {
	return lookupMobilitySpec(router, ref, "SAMRRSet", field, Resource.SAMRRSetSpec)
}

// LookupSAMPeerGroup resolves a runtime SAMPeerGroup reference and decodes its
// typed node topology.
func LookupSAMPeerGroup(router *Router, ref, field string) (SAMPeerGroupSpec, bool, error) {
	return lookupMobilitySpec(router, ref, "SAMPeerGroup", field, Resource.SAMPeerGroupSpec)
}

// LookupSAMEnrollmentPolicy resolves a SAMEnrollmentPolicy reference and decodes its spec.
func LookupSAMEnrollmentPolicy(router *Router, ref, field string) (SAMEnrollmentPolicySpec, bool, error) {
	return lookupMobilitySpec(router, ref, "SAMEnrollmentPolicy", field, Resource.SAMEnrollmentPolicySpec)
}

// LookupSAMNodeSet resolves a SAMNodeSet reference and decodes its spec.
func LookupSAMNodeSet(router *Router, ref, field string) (SAMNodeSetSpec, bool, error) {
	return lookupMobilitySpec(router, ref, "SAMNodeSet", field, Resource.SAMNodeSetSpec)
}
