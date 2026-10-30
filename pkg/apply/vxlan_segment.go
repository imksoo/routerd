// SPDX-License-Identifier: BSD-3-Clause

package apply

import (
	"context"
	"strconv"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/vxlan"
)

// VXLANSegmentObserver is the existing native owner's read-only boundary.
// Display status and same-name native presence cannot establish ownership.
type VXLANSegmentObserver interface {
	ObserveOwnedInterface(context.Context, string) (vxlan.SegmentLink, error)
}

func (e *Engine) observeVXLANSegment(resource api.Resource, includePlan bool, result *ResourceResult) {
	spec, err := resource.VXLANSegmentSpec()
	if err != nil {
		result.Phase = "Blocked"
		result.Warnings = append(result.Warnings, err.Error())
		return
	}
	ifname := defaultString(spec.IfName, resource.Metadata.Name)
	result.Observed["ifname"], result.Observed["present"] = ifname, "unknown"
	result.Observed["vni"] = strconv.Itoa(spec.VNI)
	if e.Segments == nil {
		result.Phase = "Blocked"
		result.Warnings = append(result.Warnings, "VXLAN native ownership observation is unavailable")
		return
	}
	ctx := e.operationContext
	if ctx == nil {
		ctx = context.Background() // Explicit observation collaborators own their lifetime.
	}
	link, err := e.Segments.ObserveOwnedInterface(ctx, resource.Metadata.Name)
	if err != nil {
		result.Phase = "Blocked"
		result.Warnings = append(result.Warnings, err.Error())
		return
	}
	result.Observed["present"] = strconv.FormatBool(link.Exists)
	if !link.Exists {
		result.Phase = "Drifted"
		if includePlan {
			result.Plan = append(result.Plan, "acquire and activate native VXLAN segment "+ifname)
		}
		return
	}
	result.Observed["nativeOwned"] = "true"
	result.Observed["ifindex"] = strconv.Itoa(link.Index)
	result.Observed["scope"] = link.Scope
	if includePlan {
		result.Plan = append(result.Plan, "keep acknowledged native VXLAN segment "+ifname)
	}
}
