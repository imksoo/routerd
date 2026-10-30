// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

var errTunnelNativeInputsChanged = errors.New("TunnelInterface source or intent changed")

func (c TunnelInterfaceController) currentTunnelDesired(name string) (tunnelDesired, bool, string, error) {
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion == api.HybridAPIVersion && resource.Kind == "TunnelInterface" && resource.Metadata.Name == name {
			spec, err := resource.TunnelInterfaceSpec()
			if err != nil {
				return tunnelDesired{}, false, "", err
			}
			return c.resolveTunnelDesired(name, spec)
		}
	}
	return tunnelDesired{}, true, "DeclarationRemoved", nil
}

func (c TunnelInterfaceController) checkTunnelNativeInputs(ctx context.Context, record tunnelNativeReceipt) error {
	desired, pending, _, err := c.currentTunnelDesired(record.Resource)
	if err != nil {
		return err
	}
	if pending || desired != record.Plan {
		return errTunnelNativeInputsChanged
	}
	return c.checkTunnelNativeReceipt(ctx, record)
}

func (c TunnelInterfaceController) requireTunnelNativeInputs(ctx context.Context, record tunnelNativeReceipt) error {
	err := c.checkTunnelNativeInputs(ctx, record)
	if errors.Is(err, errTunnelNativeInputsChanged) {
		if withdrawErr := c.withdrawTunnelNative(ctx, record); withdrawErr != nil {
			return withdrawErr
		}
	}
	return err
}

func (c TunnelInterfaceController) reconcileNativeTunnels(ctx context.Context) error {
	var failures []error
	desiredNames := map[string]bool{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "TunnelInterface" {
			continue
		}
		desiredNames[resource.Metadata.Name] = true
		desired, pending, reason, err := c.currentTunnelDesired(resource.Metadata.Name)
		if err == nil {
			err = c.reconcileTunnelNative(ctx, resource.Metadata.Name, desired, pending)
		}
		status := tunnelStatus(desired, false, map[string]any{"phase": "Up"})
		if !pending && err == nil {
			status["interfaceOwned"] = true
		}
		if pending || errors.Is(err, errTunnelNativeInputsChanged) {
			status["phase"], status["reason"], status["pendingSource"] = "Pending", "EndpointSourcePending", reason
		}
		if err != nil && !errors.Is(err, errTunnelNativeInputsChanged) {
			status["phase"], status["reason"], status["error"] = "Unknown", "NativeOwnershipUnconfirmed", err.Error()
			failures = append(failures, err)
		}
		if saveErr := c.Store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", resource.Metadata.Name, status); saveErr != nil {
			failures = append(failures, saveErr)
		}
	}
	records, err := c.tunnelNativeReceipts()
	if err != nil {
		return errors.Join(append(failures, err)...)
	}
	for _, record := range records {
		if !desiredNames[record.Resource] {
			if err := c.withdrawTunnelNative(ctx, record); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

func (c TunnelInterfaceController) reconcileTunnelNative(ctx context.Context, name string, desired tunnelDesired, pending bool) error {
	records, err := c.tunnelNativeReceipts()
	if err != nil {
		return err
	}
	var record tunnelNativeReceipt
	for _, current := range records {
		if current.Resource == name {
			record = current
		}
	}
	if pending {
		if record.Resource != "" {
			return c.withdrawTunnelNative(ctx, record)
		}
		return nil
	}
	if record.Resource != "" {
		forgotten, err := c.retireTunnelOldKernel(ctx, record)
		if err != nil {
			return err
		}
		if forgotten {
			record = tunnelNativeReceipt{}
		}
	}
	if record.Resource != "" && (record.Withdrawing || tunnelNativeKind(record.Plan.Mode) != tunnelNativeKind(desired.Mode) || record.PreviousAddress != "" && record.Plan.Address != desired.Address) {
		if err := c.withdrawTunnelNative(ctx, record); err != nil {
			return err
		}
		record = tunnelNativeReceipt{}
	}
	link, err := c.observeOwnedTunnel(ctx, name)
	if err != nil {
		return err
	}
	if record.Resource == "" && link.Exists {
		return fmt.Errorf("refuse unrecorded TunnelInterface %s", name)
	}
	if record.Resource != "" {
		if err := verifyTunnelNative(record, link); err != nil {
			return err
		}
		if !record.Pending && record.Plan == desired && tunnelNativeApplied(record, link) {
			listenerReady := true
			if listener, uses := fouListenerForDesired(desired); uses {
				present, matching, err := c.observeTunnelNativeListener(ctx, listener)
				if err != nil {
					return err
				}
				listenerReady = present && matching
			}
			if listenerReady {
				if err := c.requireTunnelNativeInputs(ctx, record); err != nil {
					return err
				}
				fresh, err := c.checkedOwnedTunnel(ctx, record)
				if err != nil {
					return err
				}
				if tunnelNativeApplied(record, fresh) {
					return c.requireTunnelNativeInputs(ctx, record)
				}
			}
		}
	}
	var previous *tunnelNativeReceipt
	if record.Resource == "" {
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return err
		}
		record = tunnelNativeReceipt{Resource: name, Scope: link.Scope, Token: hex.EncodeToString(token[:]), Pending: true, Plan: desired}
	} else {
		prior := record
		previous = &prior
	}
	// Retire an old listener before changing the tuple retained in the receipt.
	if listener, wanted := fouListenerForDesired(desired); record.ListenerOwned && (!wanted || listener != record.Listener) {
		prior := record
		record.Pending = true
		if err := c.saveTunnelNative(&prior, record, false); err != nil {
			return err
		}
		if err := c.retireTunnelNativeListener(ctx, &record); err != nil {
			return err
		}
		prior = record
		previous = &prior
	}
	if record.Plan.Address != desired.Address && record.PreviousAddress == "" {
		record.PreviousAddress = record.Plan.Address
	}
	record.Plan, record.Pending = desired, true
	if !link.Exists {
		record.Index = 0
	}
	if err := c.saveTunnelNative(previous, record, false); err != nil {
		return err
	}
	if err := c.requireTunnelNativeInputs(ctx, record); err != nil {
		return err
	}
	if !link.Exists {
		if listener, wanted := fouListenerForDesired(desired); wanted {
			present, matching, err := c.observeTunnelNativeListener(ctx, listener)
			if err != nil {
				return err
			}
			if present && (!matching || record.ListenerPending) {
				return errors.New("TunnelInterface listener is foreign or its create ACK is unknown")
			}
		}
		fresh, err := c.observeOwnedTunnel(ctx, name)
		if err != nil {
			return err
		}
		if fresh.Scope != record.Scope || fresh.Exists {
			return errors.New("TunnelInterface exclusive creation premise changed")
		}
		if err := c.requireTunnelNativeInputs(ctx, record); err != nil {
			return err
		}
		index := tunnelCreationIndex(record.Token)
		args := tunnelAddArgs(desired)
		args = append(args[:4], append([]string{"index", strconv.Itoa(index)}, args[4:]...)...)
		if _, err := c.run(ctx, "ip", args...); err != nil {
			return err
		}
		// This invocation has the successful exclusive create ACK. If current
		// intent was revoked while it waited, retire only that exact bare index.
		// A resumed pending receipt never enters this acknowledged branch.
		if err := c.checkTunnelNativeInputs(ctx, record); err != nil {
			if errors.Is(err, errTunnelNativeInputsChanged) {
				if cleanupErr := c.withdrawAcknowledgedTunnelCreate(ctx, record, index); cleanupErr != nil {
					return cleanupErr
				}
			}
			return err
		}
		install := c.SetAliasByIndex
		if install == nil {
			install = setTunnelNativeAlias
		}
		if err := install(ctx, index, name, tunnelNativeKind(desired.Mode), "routerd-tunnel:"+record.Token, record.Scope); err != nil {
			return err
		}
		link, err = c.checkedOwnedTunnel(ctx, record)
		if err != nil || !link.Exists {
			return errors.Join(err, errors.New("TunnelInterface create has no exact native readback"))
		}
		prior := record
		record.Index = link.Index
		if err := c.saveTunnelNative(&prior, record, false); err != nil {
			return err
		}
	}
	if record.Index == 0 {
		prior := record
		record.Index = link.Index
		if err := c.saveTunnelNative(&prior, record, false); err != nil {
			return err
		}
	}
	if err := c.ensureTunnelNativeListener(ctx, &record); err != nil {
		return err
	}
	if _, err := c.checkedOwnedTunnel(ctx, record); err != nil {
		return err
	}
	if err := c.requireTunnelNativeInputs(ctx, record); err != nil {
		return err
	}
	apply := c.ApplyNative
	if apply == nil {
		apply = applyTunnelNativePlan
	}
	if err := apply(ctx, record); err != nil {
		return err
	}
	link, err = c.checkedOwnedTunnel(ctx, record)
	if err != nil {
		return err
	}
	if !link.Exists || !tunnelLinkMatches(link.Observed, desired) || !link.Observed.Up || desired.MTU > 0 && link.Observed.MTU != desired.MTU || desired.Address != "" && !slices.Contains(link.Addresses, desired.Address) || record.PreviousAddress != "" && record.PreviousAddress != desired.Address && slices.Contains(link.Addresses, record.PreviousAddress) {
		return errors.New("TunnelInterface native readback did not confirm applied intent")
	}
	if err := c.requireTunnelNativeInputs(ctx, record); err != nil {
		return err
	}
	prior := record
	record.Pending, record.PreviousAddress = false, ""
	return c.saveTunnelNative(&prior, record, false)
}

func (c TunnelInterfaceController) withdrawAcknowledgedTunnelCreate(ctx context.Context, record tunnelNativeReceipt, index int) error {
	if err := c.checkTunnelNativeReceipt(ctx, record); err != nil {
		return err
	}
	prior := record
	record.Withdrawing = true
	if err := c.saveTunnelNative(&prior, record, false); err != nil {
		return err
	}
	remove := c.DeleteByIndex
	if remove == nil {
		remove = deleteTunnelNativeLink
	}
	if err := c.checkTunnelNativeReceipt(ctx, record); err != nil {
		return err
	}
	if err := remove(ctx, index, record.Plan.Name, tunnelNativeKind(record.Plan.Mode), "", record.Scope); err != nil {
		return err
	}
	link, err := c.observeOwnedTunnel(ctx, record.Plan.Name)
	if err != nil || link.Exists || link.Scope != record.Scope {
		return errors.Join(err, errors.New("TunnelInterface acknowledged create withdrawal is unconfirmed"))
	}
	if err := c.retireTunnelNativeListener(ctx, &record); err != nil {
		return err
	}
	return c.saveTunnelNative(&record, record, true)
}

func (c TunnelInterfaceController) withdrawTunnelNative(ctx context.Context, record tunnelNativeReceipt) error {
	forgotten, err := c.retireTunnelOldKernel(ctx, record)
	if err != nil || forgotten {
		return err
	}
	link, err := c.checkedOwnedTunnel(ctx, record)
	if err != nil {
		return err
	}
	prior := record
	record.Pending, record.Withdrawing = true, true
	if err := c.saveTunnelNative(&prior, record, false); err != nil {
		return err
	}
	link, err = c.checkedOwnedTunnel(ctx, record)
	if err != nil {
		return err
	}
	if link.Exists {
		remove := c.DeleteByIndex
		if remove == nil {
			remove = deleteTunnelNativeLink
		}
		if err := remove(ctx, link.Index, record.Plan.Name, tunnelNativeKind(record.Plan.Mode), "routerd-tunnel:"+record.Token, record.Scope); err != nil {
			return err
		}
	}
	link, err = c.checkedOwnedTunnel(ctx, record)
	if err != nil || link.Exists {
		return errors.Join(err, errors.New("TunnelInterface absence is not confirmed"))
	}
	if err := c.retireTunnelNativeListener(ctx, &record); err != nil {
		return err
	}
	if err := c.Store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", record.Resource, tunnelStatus(record.Plan, false, map[string]any{"phase": "Absent", "reason": "NativeOwnerWithdrawn"})); err != nil {
		return err
	}
	return c.saveTunnelNative(&record, record, true)
}

func (c TunnelInterfaceController) retireTunnelOldKernel(ctx context.Context, record tunnelNativeReceipt) (bool, error) {
	scope, err := c.tunnelKernelScope(ctx)
	if err != nil || scope == record.Scope {
		return false, err
	}
	oldParts, newParts := strings.Split(record.Scope, ":"), strings.Split(scope, ":")
	if len(oldParts) != 3 || len(newParts) != 3 || oldParts[1] == newParts[1] {
		return false, errors.New("TunnelInterface receipt belongs to another namespace")
	}
	return true, c.saveTunnelNative(&record, record, true)
}

func (c TunnelInterfaceController) observeTunnelNativeListener(ctx context.Context, listener fouListener) (bool, bool, error) {
	if c.NativeListeners != nil {
		return c.NativeListeners.Observe(ctx, listener)
	}
	return observeTunnelNativeFOU(ctx, listener)
}

func (c TunnelInterfaceController) ensureTunnelNativeListener(ctx context.Context, record *tunnelNativeReceipt) error {
	listener, wanted := fouListenerForDesired(record.Plan)
	if !wanted {
		return nil
	}
	present, matching, err := c.observeTunnelNativeListener(ctx, listener)
	if err != nil {
		return err
	}
	if err := c.requireTunnelNativeInputs(ctx, *record); err != nil {
		return err
	}
	if present {
		if !matching || record.ListenerPending {
			return errors.New("TunnelInterface listener is foreign or its create ACK is unknown")
		}
		return nil
	}
	prior := *record
	record.Listener, record.ListenerOwned, record.ListenerPending = listener, false, true
	if err := c.saveTunnelNative(&prior, *record, false); err != nil {
		return err
	}
	if err := c.requireTunnelNativeInputs(ctx, *record); err != nil {
		return err
	}
	var addErr error
	if c.NativeListeners != nil {
		addErr = c.NativeListeners.Add(ctx, listener)
	} else {
		addErr = addTunnelNativeFOU(ctx, listener)
	}
	if addErr != nil {
		return addErr
	}
	present, matching, err = c.observeTunnelNativeListener(ctx, listener)
	if err != nil || !present || !matching {
		return errors.Join(err, errors.New("TunnelInterface listener creation has no exact readback"))
	}
	if err := c.checkTunnelNativeReceipt(ctx, *record); err != nil {
		return err
	}
	prior = *record
	record.ListenerOwned, record.ListenerPending = true, false
	return c.saveTunnelNative(&prior, *record, false)
}

func (c TunnelInterfaceController) retireTunnelNativeListener(ctx context.Context, record *tunnelNativeReceipt) error {
	if !record.ListenerOwned && !record.ListenerPending {
		return nil
	}
	present, matching, err := c.observeTunnelNativeListener(ctx, record.Listener)
	if err != nil {
		return err
	}
	if err := c.checkTunnelNativeReceipt(ctx, *record); err != nil {
		return err
	}
	if present {
		if !matching || record.ListenerPending {
			return errors.New("TunnelInterface listener ownership is unconfirmed")
		}
		records, err := c.tunnelNativeReceipts()
		if err != nil {
			return err
		}
		for i, candidate := range records {
			listener, uses := fouListenerForDesired(candidate.Plan)
			if candidate.Resource == record.Resource || candidate.Pending || candidate.Withdrawing || !uses || listener != record.Listener {
				continue
			}
			if _, err := c.checkedOwnedTunnel(ctx, candidate); err != nil {
				return err
			}
			desired, pending, _, err := c.currentTunnelDesired(candidate.Resource)
			if err != nil {
				return err
			}
			if pending || desired != candidate.Plan {
				continue
			}
			current, err := c.tunnelNativeReceipts()
			if err != nil || !slices.Equal(current, records) {
				return errors.Join(err, errors.New("TunnelInterface listener transfer premise changed"))
			}
			records[i].Listener, records[i].ListenerOwned = record.Listener, true
			for j := range records {
				if records[j] == *record {
					records[j].Listener, records[j].ListenerOwned, records[j].ListenerPending = fouListener{}, false, false
					*record = records[j]
					return c.Store.SaveObjectStatus(api.RouterAPIVersion, "Router", tunnelNativeStatusName, map[string]any{"interfaces": records})
				}
			}
			return errors.New("TunnelInterface listener owner changed before transfer")
		}
		prior := *record
		record.ListenerPending = true
		if err := c.saveTunnelNative(&prior, *record, false); err != nil {
			return err
		}
		present, matching, err = c.observeTunnelNativeListener(ctx, record.Listener)
		if err != nil || present && !matching {
			return errors.Join(err, errors.New("TunnelInterface listener changed before withdrawal"))
		}
		if err := c.checkTunnelNativeReceipt(ctx, *record); err != nil {
			return err
		}
		if present {
			var deleteErr error
			if c.NativeListeners != nil {
				deleteErr = c.NativeListeners.Delete(ctx, record.Listener)
			} else {
				deleteErr = deleteTunnelNativeFOU(ctx, record.Listener)
			}
			if deleteErr != nil {
				return deleteErr
			}
		}
	}
	prior := *record
	record.Listener, record.ListenerOwned, record.ListenerPending = fouListener{}, false, false
	return c.saveTunnelNative(&prior, *record, false)
}
