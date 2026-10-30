// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

func (c DSLiteTunnelController) nativeTunnelName(name string, spec api.DSLiteTunnelSpec) string {
	name = firstNonEmpty(spec.TunnelName, spec.Interface, name)
	if c.currentOS() == platform.OSFreeBSD {
		name = freeBSDDSLiteRuntimeIfName(name)
	}
	return name
}

// Resolve current intent without running the controller or persisting anything.
func (c DSLiteTunnelController) childParentPlan(ctx context.Context, resource api.Resource) (dsliteTunnelPlan, error) {
	spec, err := resource.DSLiteTunnelSpec()
	if err != nil {
		return dsliteTunnelPlan{}, err
	}
	if !api.BoolDefault(spec.Enabled, true) {
		return dsliteTunnelPlan{}, errors.New("DS-Lite parent is disabled")
	}
	if resourcequery.ResourceWhenPresent(spec.When) {
		state, ok := c.Store.(resourcequery.StateStore)
		if !ok {
			return dsliteTunnelPlan{}, errors.New("DS-Lite parent condition reader unavailable")
		}
		eligible, err := resourcequery.ResourceWhenMatches(spec.When, state)
		if err != nil {
			return dsliteTunnelPlan{}, err
		}
		if !eligible {
			return dsliteTunnelPlan{}, errors.New("DS-Lite parent condition is not true")
		}
	}
	source, err := resourcequery.ReadValue(c.Store, spec.AFTRFrom)
	if err != nil {
		return dsliteTunnelPlan{}, err
	}
	_, remote, err := c.resolveRemote(ctx, spec, source)
	if err != nil {
		return dsliteTunnelPlan{}, err
	}
	local, _, err := c.localAddress(ctx, spec)
	if err != nil {
		return dsliteTunnelPlan{}, err
	}
	inner, pending, err := dsliteInnerLocalIPv4(c.Router, c.Store, spec)
	if err != nil {
		return dsliteTunnelPlan{}, err
	}
	if local == "" || pending != "" {
		return dsliteTunnelPlan{}, errors.New("DS-Lite parent source is not ready")
	}
	plan := c.nativeTunnelPlan(resource.Metadata.Name, spec, local, remote, inner)
	if err := c.checkTunnelInputs(ctx, resource.Metadata.Name, spec, plan); err != nil {
		return dsliteTunnelPlan{}, err
	}
	return plan, nil
}

func (c DSLiteTunnelController) nativeTunnelPlan(name string, spec api.DSLiteTunnelSpec, local, remote, inner string) dsliteTunnelPlan {
	name = c.nativeTunnelName(name, spec)
	underlay := interfaceName(c.Router, spec.Interface)
	if underlay == name {
		underlay = ""
	}
	mtu := spec.MTU
	if mtu == 0 {
		mtu = 1460
	}
	return dsliteTunnelPlan{Name: name, Underlay: underlay, Local: local, Remote: remote, InnerLocal: inner, EncapLimit: firstNonEmpty(spec.EncapsulationLimit, "none"), MTU: mtu, DefaultRoute: spec.DefaultRoute, RouteMetric: spec.RouteMetric}
}

// A recorded token permits only cleanup on its exact native parent, even after
// declaration removal. Active children also require current applied intent.
func (c DSLiteTunnelController) observeChildParent(ctx context.Context, resource api.Resource, ifname, token string) (nativeParentLink, error) {
	var absent nativeParentLink
	if c.Store == nil {
		return absent, errors.New("DS-Lite parent journal unavailable")
	}
	active := resource.Kind != ""
	var desired dsliteTunnelPlan
	var err error
	if active {
		desired, err = c.childParentPlan(ctx, resource)
		if err != nil {
			return absent, err
		}
		ifname = desired.Name
	}
	scope, err := c.tunnelScope(ctx)
	if err != nil {
		return absent, err
	}
	absent.Scope = scope
	records, err := c.nativeTunnelReceipts()
	if err != nil {
		return absent, err
	}
	var record dsliteTunnelReceipt
	for _, candidate := range records {
		if candidate.Plan.Name == ifname && (active && candidate.Resource == resource.Metadata.Name || !active && candidate.Token == token) {
			record = candidate
			break
		}
	}
	if record.Resource != "" && (record.Pending || record.IfIndex < 1 || active && record.Plan != desired) {
		return absent, errors.New("DS-Lite parent is pending or differs from current intent")
	}
	observed, err := c.observeNativeTunnel(ctx, ifname)
	if err != nil {
		return absent, err
	}
	if record.Resource == "" {
		if observed.Exists {
			return absent, errors.New("DS-Lite parent is present without its native ownership receipt")
		}
		return absent, ctx.Err()
	}
	if err := c.verifyNativeTunnel(ctx, record, observed); err != nil {
		return absent, err
	}
	if !observed.Exists {
		return absent, nil
	}
	if active {
		if !dsliteAppliedParentLink(record.OS, desired, observed) || observed.InnerLocal != desired.InnerLocal {
			return absent, errors.New("DS-Lite parent native readback differs from applied intent")
		}
		current, err := c.childParentPlan(ctx, resource)
		if err != nil {
			return absent, err
		}
		if current != desired {
			return absent, errors.New("DS-Lite parent intent changed while observing")
		}
	}
	// Required journal reads may wait. A parent beginning withdrawal during the
	// native snapshot cannot authorize a new child or its deletion.
	current, err := c.nativeTunnelReceipts()
	if err != nil {
		return absent, err
	}
	found := false
	for _, candidate := range current {
		if candidate == record {
			found = true
			break
		}
	}
	if !found {
		return absent, errors.New("DS-Lite parent receipt changed while observing")
	}
	// The inner-address, scope and journal reads can wait. Observe the actual
	// native cookie after those waits, without a later scope lookup on a cached link.
	scope, err = c.tunnelScope(ctx)
	if err != nil {
		return absent, err
	}
	observed, err = c.observeNativeTunnelLink(ctx, ifname)
	if err != nil {
		return absent, err
	}
	if err := checkNativeTunnelIdentity(c.currentOS(), scope, record, observed); err != nil {
		return absent, err
	}
	if !observed.Exists {
		return absent, nil
	}
	if active && !dsliteAppliedParentLink(record.OS, desired, observed) {
		return absent, errors.New("DS-Lite parent changed after observation waits")
	}
	if active {
		current, err := c.childParentPlan(ctx, resource)
		if err != nil {
			return absent, err
		}
		if current != desired {
			return absent, errors.New("DS-Lite parent source changed after native observation")
		}
	}
	return nativeParentLink{Exists: true, IfName: ifname, Index: observed.Index, Scope: scope, Token: "dslite:" + record.Token}, ctx.Err()
}

func dsliteAppliedParentLink(osName platform.OS, plan dsliteTunnelPlan, observed dsliteTunnelObservation) bool {
	return observed.Up && observed.MTU == plan.MTU && observed.Local == plan.Local && observed.Remote == plan.Remote && (osName != platform.OSLinux || observed.Proto == "ipip6" && observed.Underlay == plan.Underlay && observed.EncapLimit == plan.EncapLimit)
}
