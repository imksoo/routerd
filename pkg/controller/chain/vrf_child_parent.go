// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"

	"github.com/imksoo/routerd/pkg/api"
)

func (c VRFController) observeChildParent(ctx context.Context, resource api.Resource, name, token string) (nativeParentLink, error) {
	absent := nativeParentLink{Declared: true}
	active := resource.Kind != ""
	var desired vrfPlan
	var err error
	if active {
		desired, err = c.currentPlan(resource.Metadata.Name)
		if err != nil {
			return absent, err
		}
		name = desired.Name
	}
	records, err := c.readReceipts()
	if err != nil {
		return absent, err
	}
	var record vrfReceipt
	for _, candidate := range records {
		if candidate.Plan.Name == name && (active && candidate.Resource == resource.Metadata.Name || !active && candidate.Token == token) {
			record = candidate
			break
		}
	}
	link, err := c.native().Observe(ctx, name)
	if err != nil {
		return absent, err
	}
	absent.Scope = link.Scope
	if record.Resource == "" {
		if link.Exists {
			return absent, errors.New("VRF parent exists without its ownership receipt")
		}
		return absent, ctx.Err()
	}
	if record.Index < 1 || active && (record.Pending || record.Withdrawing || !sameVRFPlan(record.Plan, desired)) {
		return absent, errors.New("VRF parent is pending or differs from current intent")
	}
	if err := checkVRFIdentity(record, link); err != nil {
		return absent, err
	}
	if !link.Exists {
		return absent, ctx.Err()
	}
	if active {
		healthy, err := c.membersMatch(ctx, record)
		if err != nil {
			return absent, err
		}
		if !link.Up || !healthy {
			return absent, errors.New("VRF parent is not natively active with its acknowledged members")
		}
	}
	link, err = c.native().Observe(ctx, name)
	if err != nil {
		return absent, err
	}
	if err := checkVRFIdentity(record, link); err != nil {
		return absent, err
	}
	if !link.Exists {
		return absent, ctx.Err()
	}
	if active && !link.Up {
		return absent, errors.New("VRF parent stopped during observation")
	}
	records, err = c.readReceipts()
	if err != nil {
		return absent, err
	}
	found := false
	for _, current := range records {
		if sameVRFReceipt(current, record) {
			found = true
			break
		}
	}
	if !found {
		return absent, errors.New("VRF parent receipt changed while observing")
	}
	if err := c.checkScope(ctx, record); err != nil {
		return absent, err
	}
	if active {
		current, err := c.currentPlan(record.Resource)
		if err != nil {
			return absent, err
		}
		if !sameVRFPlan(current, desired) {
			return absent, errors.New("VRF parent intent changed after observation waits")
		}
	}
	return nativeParentLink{Declared: true, Exists: true, IfName: name, Index: record.Index, Scope: record.Scope, Token: "vrf:" + record.Token}, ctx.Err()
}
