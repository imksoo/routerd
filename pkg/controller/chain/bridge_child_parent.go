// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"slices"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

// Active children require the current acknowledged plan. Recorded cleanup uses
// the same concrete receipt and native identity even after declaration removal
// or while the parent is withdrawing; it cannot acquire a new child effect.
func (c BridgeController) observeChildParent(ctx context.Context, resource api.Resource, ifname, token string) (nativeParentLink, error) {
	absent := nativeParentLink{Declared: true}
	if c.currentOS() != platform.OSLinux {
		return absent, errors.New("Bridge native child ownership is unavailable on this OS")
	}
	active := resource.Kind != ""
	var desired bridgePlan
	var err error
	if active {
		desired, err = c.childBridgePlan(resource)
		if err != nil {
			return absent, err
		}
		ifname = desired.IfName
	}
	absent.Scope, err = c.bridgeScope(ctx)
	if err != nil {
		return absent, err
	}
	owners, err := c.readBridgeOwners()
	if err != nil {
		return absent, err
	}
	var record bridgeOwner
	for _, owner := range owners {
		if owner.Plan.IfName != ifname || active && owner.Resource != resource.Metadata.Name || !active && owner.Token != token {
			continue
		}
		if record.Resource != "" {
			return absent, errors.New("multiple Bridge receipts claim the same native parent")
		}
		record = owner
	}
	if record.Resource == "" {
		iface, err := c.bridgeInterface(ifname)
		if err != nil {
			return absent, err
		}
		if iface != nil {
			return absent, errors.New("Bridge parent is present without its native ownership receipt")
		}
		currentScope, err := c.bridgeScope(ctx)
		if err != nil {
			return absent, err
		}
		if currentScope != absent.Scope {
			return absent, errors.New("Bridge parent scope changed while observing absence")
		}
		return absent, ctx.Err()
	}
	if err := c.checkBridgeScope(ctx, record); err != nil {
		return absent, err
	}
	absent.Scope = record.Scope
	if record.IfIndex < 1 || active && (!record.Activated || record.Withdrawing || record.Plan.digest() != desired.digest()) {
		return absent, errors.New("Bridge parent is pending or differs from current intent")
	}
	iface, err := c.observedOwnedBridge(record)
	if err != nil {
		return absent, err
	}
	if err := c.checkBridgeScope(ctx, record); err != nil {
		return absent, err
	}
	if iface == nil {
		return absent, ctx.Err()
	}
	if active {
		healthy, err := c.bridgeHealthy(record, *iface)
		if err != nil {
			return absent, err
		}
		if !healthy {
			return absent, errors.New("Bridge parent native readback differs from applied intent")
		}
	}
	// Health and artifact observations can wait. Recheck the actual receipt,
	// scope and native identity rather than publishing the earlier snapshot.
	if err := c.checkBridgeScope(ctx, record); err != nil {
		return absent, err
	}
	iface, err = c.observedOwnedBridge(record)
	if err != nil {
		return absent, err
	}
	if err := c.checkBridgeScope(ctx, record); err != nil {
		return absent, err
	}
	if iface == nil {
		return absent, ctx.Err()
	}
	if active {
		current, err := c.childBridgePlan(resource)
		if err != nil {
			return absent, err
		}
		if current.digest() != desired.digest() {
			return absent, errors.New("Bridge parent intent changed after native observation")
		}
	}
	owners, err = c.readBridgeOwners()
	if err != nil {
		return absent, err
	}
	found := false
	for _, owner := range owners {
		if owner.Resource == record.Resource && owner.Token == record.Token && owner.Scope == record.Scope && owner.IfIndex == record.IfIndex && owner.Activated == record.Activated && owner.Withdrawing == record.Withdrawing && owner.Plan.digest() == record.Plan.digest() && slices.Equal(owner.Ports, record.Ports) {
			found = true
			break
		}
	}
	if !found {
		return absent, errors.New("Bridge parent receipt changed while observing")
	}
	return nativeParentLink{Declared: true, Exists: true, IfName: ifname, Scope: record.Scope, Index: iface.Index, Token: "bridge:" + record.Token}, ctx.Err()
}

func (c BridgeController) childBridgePlan(resource api.Resource) (bridgePlan, error) {
	if c.Router != nil {
		for _, current := range c.Router.Spec.Resources {
			if current.ID() == resource.ID() {
				return c.bridgePlan(current)
			}
		}
	}
	return bridgePlan{}, errors.New("Bridge parent declaration is no longer current")
}
