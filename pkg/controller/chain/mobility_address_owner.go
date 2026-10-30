// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"errors"
	"fmt"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/platform"
)

var errMobilityAddressIntentChanged = errors.New("mobility address intent changed")

func (c IPv4StaticAddressController) currentMobilityAddressPlan() (dynamicconfig.MobilityDataplanePlan, error) {
	return currentMobilityNativePlan(c.MobilitySource, c.MobilityDataplane)
}
func mobilityAddressIntent(a mobilityAppliedIPv4Address) dynamicconfig.MobilityIPv4AddressIntent {
	return dynamicconfig.MobilityIPv4AddressIntent{ID: a.ID, PoolRef: a.PoolRef, PoolPrefix: a.PoolPrefix, Purpose: dynamicconfig.MobilityIPv4AddressPurpose(a.Purpose), Interface: a.Interface, Address: a.Address}
}
func mobilityAddressReceipt(a mobilityAppliedIPv4Address) staticAddressReceipt {
	return staticAddressReceipt{IfName: a.Interface, IfIndex: a.IfIndex, Scope: a.Scope, Address: a.Address, ParentToken: a.ParentToken, Pending: a.Operation != ""}
}
func (c IPv4StaticAddressController) mobilityAddressSnapshot(ctx context.Context, ifname string) (staticAddressSnapshot, error) {
	observed, err := c.staticAddresses(ctx, ifname)
	if err != nil {
		return observed, err
	}
	if observed.Scope == "" {
		observed.Scope, err = platform.KernelNetworkScope(ctx)
	}
	if err != nil {
		return observed, err
	}
	if observed.Scope == "" || observed.Index < 0 {
		return observed, errors.New("mobility address native observation has no kernel identity")
	}
	return observed, ctx.Err()
}
func (c IPv4StaticAddressController) currentMobilityAddressPremise(ctx context.Context, intent dynamicconfig.MobilityIPv4AddressIntent) (mobilityAppliedIPv4Address, error) {
	var zero mobilityAppliedIPv4Address
	current, err := c.currentMobilityAddressPlan()
	if err != nil {
		return zero, err
	}
	found := false
	for _, a := range current.StaticAddresses {
		if a == intent {
			found = true
			break
		}
	}
	if !found {
		return zero, errMobilityAddressIntentChanged
	}
	if mobilityStaticAddressDesiredByUser(c.Router, intent.Interface, intent.Address) {
		return zero, errors.New("mobility address is also desired by a user resource")
	}
	observed, err := c.mobilityAddressSnapshot(ctx, intent.Interface)
	if err != nil {
		return zero, err
	}
	if observed.Index <= 0 {
		return zero, fmt.Errorf("mobility address interface %s is unavailable", intent.Interface)
	}
	parent, err := c.staticSegmentParent(ctx, intent.Interface, observed)
	if err != nil {
		return zero, err
	}
	latest, err := c.currentMobilityAddressPlan()
	if err != nil {
		return zero, err
	}
	found = false
	for _, a := range latest.StaticAddresses {
		if a == intent {
			found = true
			break
		}
	}
	if !found {
		return zero, errMobilityAddressIntentChanged
	}
	return mobilityAppliedIPv4Address{ID: intent.ID, PoolRef: intent.PoolRef, PoolPrefix: intent.PoolPrefix, Purpose: string(intent.Purpose), Interface: intent.Interface, Address: intent.Address, Scope: observed.Scope, IfIndex: observed.Index, ParentToken: parent}, ctx.Err()
}
func (c IPv4StaticAddressController) verifyMobilityAddressIdentity(ctx context.Context, a mobilityAppliedIPv4Address) (staticAddressSnapshot, error) {
	observed, err := c.mobilityAddressSnapshot(ctx, a.Interface)
	if err != nil {
		return observed, err
	}
	if observed.Scope != a.Scope || observed.Index != a.IfIndex {
		return observed, errors.New("mobility address native world or interface identity changed")
	}
	receipt := mobilityAddressReceipt(a)
	receipt.Pending = false
	if a.ParentToken != "" {
		if err := c.checkStaticWithdrawalParent(ctx, &receipt, observed); err != nil {
			return observed, err
		}
	}
	return observed, ctx.Err()
}

func (c IPv4StaticAddressController) applyMobilityStaticAddress(ctx context.Context, intent dynamicconfig.MobilityIPv4AddressIntent) (mobilityAppliedIPv4Address, error) {
	if c.DryRun {
		return mobilityAppliedIPv4Address{ID: intent.ID, PoolRef: intent.PoolRef, PoolPrefix: intent.PoolPrefix, Purpose: string(intent.Purpose), Interface: intent.Interface, Address: intent.Address}, nil
	}
	wanted, err := c.currentMobilityAddressPremise(ctx, intent)
	if err != nil {
		return mobilityAppliedIPv4Address{}, err
	}
	previous, err := c.appliedMobilityStaticAddresses()
	if err != nil {
		return mobilityAppliedIPv4Address{}, err
	}
	owned := false
	for _, old := range previous {
		if old.ID != wanted.ID {
			continue
		}
		if old == wanted {
			owned = true
			continue
		}
		if err := c.removeMobilityStaticAddress(ctx, old); err != nil {
			return mobilityAppliedIPv4Address{}, err
		}
	}
	observed, err := c.verifyMobilityAddressIdentity(ctx, wanted)
	if err != nil {
		return mobilityAppliedIPv4Address{}, err
	}
	if observed.has(wanted.Address) {
		if !owned {
			return mobilityAppliedIPv4Address{}, fmt.Errorf("foreign address occupies %s on %s", wanted.Address, wanted.Interface)
		}
		fresh, err := c.currentMobilityAddressPremise(ctx, intent)
		if err != nil || fresh != wanted {
			return mobilityAppliedIPv4Address{}, errors.Join(err, errMobilityAddressIntentChanged)
		}
		return wanted, nil
	}
	pending := wanted
	pending.Operation = "add"
	if err := c.stageMobilityAddress(pending); err != nil {
		return mobilityAppliedIPv4Address{}, err
	}
	abort := func(cause error) (mobilityAppliedIPv4Address, error) {
		return mobilityAppliedIPv4Address{}, errors.Join(cause, c.confirmMobilityAddress(pending, false, false))
	}
	fresh, err := c.currentMobilityAddressPremise(ctx, intent)
	if err != nil {
		return abort(err)
	}
	if fresh != wanted {
		return abort(errMobilityAddressIntentChanged)
	}
	observed, err = c.verifyMobilityAddressIdentity(ctx, wanted)
	if err != nil {
		return abort(err)
	}
	if observed.has(wanted.Address) {
		return abort(errors.New("address appeared before exclusive mobility acquisition"))
	}
	// Read current authority after both persistence and the native observation.
	fresh, err = c.currentMobilityAddressPremise(ctx, intent)
	if err != nil {
		return abort(err)
	}
	if fresh != wanted {
		return abort(errMobilityAddressIntentChanged)
	}
	command := c.Command
	if command == nil {
		command = runCommandContext
	}
	name, args := ipv4StaticAddressApplyCommand(platform.CurrentOS(), wanted.Interface, wanted.Address)
	if platform.CurrentOS() == platform.OSLinux {
		args[2] = "add"
	}
	if err := command(ctx, name, args...); err != nil {
		return mobilityAppliedIPv4Address{}, err
	}
	observed, err = c.verifyMobilityAddressIdentity(ctx, wanted)
	if err != nil {
		return mobilityAppliedIPv4Address{}, err
	}
	if !observed.has(wanted.Address) {
		return mobilityAppliedIPv4Address{}, errors.New("mobility address acquisition lacks exact native readback")
	}
	if err := c.confirmMobilityAddress(pending, true, false); err != nil {
		return mobilityAppliedIPv4Address{}, err
	}
	fresh, err = c.currentMobilityAddressPremise(ctx, intent)
	if err != nil || fresh != wanted {
		cause := errors.Join(err, errMobilityAddressIntentChanged)
		if errors.Is(err, errMobilityAddressIntentChanged) {
			cause = errors.Join(cause, c.removeMobilityStaticAddress(ctx, wanted))
		}
		return mobilityAppliedIPv4Address{}, cause
	}
	return wanted, nil
}

func (c IPv4StaticAddressController) removeMobilityStaticAddress(ctx context.Context, a mobilityAppliedIPv4Address) error {
	if c.DryRun {
		return nil
	}
	previous, err := c.appliedMobilityStaticAddresses()
	if err != nil {
		return err
	}
	known := false
	for _, old := range previous {
		if old == a {
			known = true
			break
		}
	}
	if !known {
		return nil
	}
	observed, err := c.verifyMobilityAddressIdentity(ctx, a)
	if err != nil {
		return err
	}
	if !observed.has(a.Address) {
		return c.confirmMobilityAddress(a, false, true)
	}
	pending := a
	pending.Operation = "delete"
	if err := c.stageMobilityAddress(pending); err != nil {
		return err
	}
	abort := func(cause error) error { return errors.Join(cause, c.confirmMobilityAddress(pending, false, false)) }
	observed, err = c.verifyMobilityAddressIdentity(ctx, a)
	if err != nil {
		return abort(err)
	}
	if !observed.has(a.Address) {
		return c.confirmMobilityAddress(pending, false, true)
	}
	current, err := c.currentMobilityAddressPlan()
	if err != nil {
		return abort(err)
	}
	for _, intent := range current.StaticAddresses {
		if mobilityStaticAddressMatchesIntent(a, intent) {
			return abort(errors.New("mobility address became active before withdrawal"))
		}
	}
	if mobilityStaticAddressDesiredByUser(c.Router, a.Interface, a.Address) {
		return abort(errors.New("mobility address is desired by a user resource"))
	}
	command := c.Command
	if command == nil {
		command = runCommandContext
	}
	name, args := ipv4StaticAddressDeleteCommand(platform.CurrentOS(), a.Interface, a.Address)
	if err := command(ctx, name, args...); err != nil {
		return err
	}
	observed, err = c.verifyMobilityAddressIdentity(ctx, a)
	if err != nil {
		return err
	}
	if observed.has(a.Address) {
		return errors.New("mobility address remains after native withdrawal")
	}
	return c.confirmMobilityAddress(pending, false, true)
}

// The only acquisition acknowledgement is this invocation's successful native
// command plus exact readback. An equal old applied row cannot retire pending.
func (c IPv4StaticAddressController) confirmMobilityAddress(a mobilityAppliedIPv4Address, acquired, withdrawn bool) error {
	status, err := readMobilityNativeStatus(c.Store)
	if err != nil {
		return err
	}
	pending, err := pendingMobilityAddresses(status)
	if err != nil {
		return err
	}
	applied, err := c.appliedMobilityStaticAddresses()
	if err != nil {
		return err
	}
	known := a
	known.Operation = ""
	next := map[string]mobilityAppliedIPv4Address{}
	for _, old := range applied {
		if !withdrawn || old != known {
			next[old.ID] = old
		}
	}
	if acquired {
		next[known.ID] = known
	}
	remaining := make([]mobilityAppliedIPv4Address, 0, len(pending))
	for _, old := range pending {
		if old != a {
			remaining = append(remaining, old)
		}
	}
	merger, ok := c.Store.(objectStatusMerger)
	if !ok {
		return errors.New("mobility address ownership merger unavailable")
	}
	return merger.MergeObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName, map[string]any{"appliedMobilityStaticAddresses": sortedMobilityAppliedStaticAddresses(next), "pendingMobilityStaticAddresses": remaining})
}
func (c IPv4StaticAddressController) publishMobilityAddressWithdrawals() error {
	if c.DryRun {
		return nil
	}
	status, err := readMobilityNativeStatus(c.Store)
	if err != nil {
		return err
	}
	pending, err := pendingMobilityAddresses(status)
	if err != nil {
		return err
	}
	applied, err := c.appliedMobilityStaticAddresses()
	if err != nil {
		return err
	}
	plan, err := c.currentMobilityAddressPlan()
	if err != nil {
		return err
	}
	if len(pending) == 0 && len(applied) == 0 && len(plan.Withdrawals) == 0 && !statusValuePresent(status, dynamicconfig.MobilityAddressWithdrawalAcks) {
		return nil
	}
	acks := matchingMobilityWithdrawalAcks(plan.Withdrawals, func(request dynamicconfig.MobilityWithdrawalRequest) bool {
		for _, a := range append(append([]mobilityAppliedIPv4Address(nil), applied...), pending...) {
			if a.PoolRef == request.PoolRef && dynamicconfig.MobilityEffectContainsAddress(a.Address, request.Address) {
				return false
			}
		}
		return true
	})
	merger, ok := c.Store.(objectStatusMerger)
	if !ok {
		return errors.New("mobility address ownership merger unavailable")
	}
	return merger.MergeObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName, map[string]any{dynamicconfig.MobilityAddressWithdrawalAcks: acks})
}
