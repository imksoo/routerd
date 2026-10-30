// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/resourcequery"
	state "github.com/imksoo/routerd/pkg/state"
)

type delegatedNativeIntent struct {
	Operation      string    `json:"operation"`
	PreferredUntil time.Time `json:"preferredUntil"`
	ValidUntil     time.Time `json:"validUntil"`
}
type delegatedNativeAddress struct {
	Address        string                 `json:"address"`
	Held           bool                   `json:"held"`
	Pending        *delegatedNativeIntent `json:"pending"`
	PreferredUntil time.Time              `json:"preferredUntil"`
	ValidUntil     time.Time              `json:"validUntil"`
}

type delegatedNativeReceipt struct {
	IfName           string                   `json:"ifname"`
	IfIndex          int                      `json:"ifIndex"`
	Scope            string                   `json:"scope"`
	ParentToken      string                   `json:"parentToken"`
	PrefixDelegation string                   `json:"prefixDelegation"`
	Addresses        []delegatedNativeAddress `json:"addresses"`
}

func delegatedReceipt(status map[string]any) (*delegatedNativeReceipt, error) {
	raw, present := status["nativeReceipt"]
	if !present {
		return nil, nil
	}
	var receipt delegatedNativeReceipt
	switch value := raw.(type) {
	case delegatedNativeReceipt:
		receipt = value
		receipt.Addresses = append([]delegatedNativeAddress(nil), value.Addresses...)
	case map[string]any:
		if len(value) != 6 {
			return nil, fmt.Errorf("invalid delegated address receipt shape")
		}
		var ok bool
		receipt.IfName, ok = value["ifname"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid delegated receipt interface")
		}
		receipt.ParentToken, ok = value["parentToken"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid delegated receipt parent token")
		}
		receipt.Scope, ok = value["scope"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid delegated receipt scope")
		}
		receipt.PrefixDelegation, ok = value["prefixDelegation"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid delegated receipt source")
		}
		index, ok := value["ifIndex"].(float64)
		if !ok || index < 1 || index > 2147483647 || index != float64(int(index)) {
			return nil, fmt.Errorf("invalid delegated receipt interface index")
		}
		receipt.IfIndex = int(index)
		addresses, ok := value["addresses"].([]any)
		if !ok {
			return nil, fmt.Errorf("invalid delegated receipt addresses")
		}
		for _, raw := range addresses {
			entry, ok := raw.(map[string]any)
			if !ok || len(entry) != 5 {
				return nil, fmt.Errorf("invalid delegated address receipt")
			}
			address, ok := entry["address"].(string)
			if !ok {
				return nil, fmt.Errorf("invalid delegated receipt address")
			}
			held, ok := entry["held"].(bool)
			if !ok {
				return nil, fmt.Errorf("invalid delegated held state")
			}
			preferred, err := receiptDeadline(entry["preferredUntil"])
			if err != nil {
				return nil, err
			}
			valid, err := receiptDeadline(entry["validUntil"])
			if err != nil {
				return nil, err
			}
			owned := delegatedNativeAddress{Address: address, Held: held, PreferredUntil: preferred, ValidUntil: valid}
			if rawPending := entry["pending"]; rawPending != nil {
				pending, ok := rawPending.(map[string]any)
				if !ok || len(pending) != 3 {
					return nil, fmt.Errorf("invalid delegated pending operation")
				}
				operation, ok := pending["operation"].(string)
				if !ok {
					return nil, fmt.Errorf("invalid delegated pending operation")
				}
				preferred, err := receiptDeadline(pending["preferredUntil"])
				if err != nil {
					return nil, err
				}
				valid, err := receiptDeadline(pending["validUntil"])
				if err != nil {
					return nil, err
				}
				owned.Pending = &delegatedNativeIntent{Operation: operation, PreferredUntil: preferred, ValidUntil: valid}
			}
			receipt.Addresses = append(receipt.Addresses, owned)
		}
	default:
		return nil, fmt.Errorf("invalid delegated native receipt")
	}
	if receipt.IfName == "" || strings.ContainsAny(receipt.IfName, "/\x00\n\r") || receipt.Scope == "" || receipt.IfIndex < 1 || receipt.PrefixDelegation == "" {
		return nil, fmt.Errorf("invalid delegated receipt native identity")
	}
	seen := map[string]bool{}
	for _, entry := range receipt.Addresses {
		prefix, err := netip.ParsePrefix(entry.Address)
		if err != nil || !prefix.Addr().Is6() || prefix.Addr().Is4In6() || prefix.String() != entry.Address || seen[entry.Address] || (entry.Held && (entry.PreferredUntil.IsZero() || entry.ValidUntil.IsZero())) {
			return nil, fmt.Errorf("invalid delegated receipt address %q", entry.Address)
		}
		if !entry.Held && entry.Pending == nil {
			return nil, fmt.Errorf("delegated receipt has no known or pending operation")
		}
		if entry.Pending != nil {
			if entry.Pending.Operation != "apply" && entry.Pending.Operation != "withdraw" {
				return nil, fmt.Errorf("invalid delegated pending operation")
			}
			if entry.Pending.Operation == "apply" && (entry.Pending.PreferredUntil.IsZero() || entry.Pending.ValidUntil.IsZero()) {
				return nil, fmt.Errorf("invalid delegated apply deadline")
			}
		}
		seen[entry.Address] = true
	}
	return &receipt, nil
}

func (r *delegatedNativeReceipt) sameInterface(observed delegatedAddressObservation) bool {
	return r != nil && r.IfIndex == observed.ifIndex && r.Scope == observed.scope && r.ParentToken == observed.parentToken
}

func (r *delegatedNativeReceipt) has(address string) bool {
	if r != nil {
		for _, entry := range r.Addresses {
			if entry.Address == address {
				return true
			}
		}
	}
	return false
}
func receiptDeadline(raw any) (time.Time, error) {
	value, ok := raw.(string)
	if !ok {
		return time.Time{}, fmt.Errorf("invalid delegated receipt deadline")
	}
	return time.Parse(time.RFC3339Nano, value)
}
func (r *delegatedNativeReceipt) entry(address string) *delegatedNativeAddress {
	if r != nil {
		for i := range r.Addresses {
			if r.Addresses[i].Address == address {
				return &r.Addresses[i]
			}
		}
	}
	return nil
}
func (r *delegatedNativeReceipt) ownedAt(address string, now time.Time) bool {
	entry := r.entry(address)
	return entry != nil && entry.Held && now.Before(entry.ValidUntil) && (entry.Pending == nil || entry.Pending.Operation == "apply")
}
func (r *delegatedNativeReceipt) claim(address string, preferred, valid time.Time) {
	entry := r.entry(address)
	if entry == nil {
		r.Addresses = append(r.Addresses, delegatedNativeAddress{Address: address})
		entry = &r.Addresses[len(r.Addresses)-1]
	}
	entry.Pending = &delegatedNativeIntent{Operation: "apply", PreferredUntil: preferred, ValidUntil: valid}
}
func (r *delegatedNativeReceipt) confirm(address string) {
	entry := r.entry(address)
	if entry != nil && entry.Pending != nil && entry.Pending.Operation == "apply" {
		entry.Held = true
		entry.PreferredUntil, entry.ValidUntil = entry.Pending.PreferredUntil, entry.Pending.ValidUntil
		entry.Pending = nil
	}
}
func (r *delegatedNativeReceipt) matches(address string, preferred, valid time.Time) bool {
	entry := r.entry(address)
	return entry != nil && entry.Held && entry.Pending == nil && entry.PreferredUntil.Equal(preferred) && entry.ValidUntil.Equal(valid)
}
func (r *delegatedNativeReceipt) beginWithdrawal(address string) {
	if entry := r.entry(address); entry != nil {
		entry.Pending = &delegatedNativeIntent{Operation: "withdraw"}
	}
}

func nativePDDeadline(acquired time.Time, seconds uint32) time.Time {
	if seconds == infinitePDLifetime {
		return time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	}
	return acquired.Add(time.Duration(seconds) * time.Second).UTC()
}
func nativePDAddressDeadlines(lease state.PDLease, deprecated bool) (time.Time, time.Time) {
	preferred := nativePDDeadline(lease.LastReplyAt, lease.PreferredSeconds)
	if deprecated || lease.PreferredSeconds == 0 {
		preferred = time.Unix(0, 0).UTC()
	}
	return preferred, nativePDDeadline(lease.LastReplyAt, lease.ValidSeconds)
}
func (r *delegatedNativeReceipt) forget(address string) {
	var kept []delegatedNativeAddress
	for _, entry := range r.Addresses {
		if entry.Address != address {
			kept = append(kept, entry)
		}
	}
	r.Addresses = kept
}

func (c LANAddressController) saveDelegatedReceipt(name string, status map[string]any, receipt *delegatedNativeReceipt) error {
	next := copyStatusMap(status)
	delete(next, "nativeReceipt")
	if receipt != nil && len(receipt.Addresses) > 0 {
		owned := *receipt
		owned.Addresses = append([]delegatedNativeAddress(nil), receipt.Addresses...)
		next["nativeReceipt"] = owned
	}
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", name, next)
}

func (c LANAddressController) withdrawDelegatedReceiptAddress(ctx context.Context, name string, status map[string]any, receipt *delegatedNativeReceipt, address string) error {
	if receipt == nil || !receipt.has(address) || c.DryRun {
		return nil
	}
	observed, err := c.observeRecordedDelegatedAddress(ctx, receipt, address)
	if err != nil {
		return err
	}
	if !receipt.sameInterface(observed) || !observed.present {
		receipt.forget(address)
		return c.saveDelegatedReceipt(name, status, receipt)
	}
	if !receipt.ownedAt(address, c.now()) {
		return c.fenceDelegatedAddress(name, status, receipt, address)
	}
	receipt.beginWithdrawal(address)
	pending := copyStatusMap(status)
	pending["phase"], pending["reason"] = "Pending", "AddressWithdrawing"
	if err := c.saveDelegatedReceipt(name, pending, receipt); err != nil {
		return err
	}
	// Durable ownership does not authorize an identically named replacement.
	observed, err = c.observeRecordedDelegatedAddress(ctx, receipt, address)
	if err != nil {
		return err
	}
	if !receipt.sameInterface(observed) || !observed.present {
		receipt.forget(address)
		return c.saveDelegatedReceipt(name, status, receipt)
	}
	command := c.Command
	if command == nil {
		command = runCommandContext
	}
	cmd, args := ipv6StaticAddressDeleteCommand(c.currentOS(), receipt.IfName, address)
	writeErr := command(ctx, cmd, args...)
	after, readErr := c.observeRecordedDelegatedAddress(ctx, receipt, address)
	if readErr != nil {
		return errors.Join(writeErr, readErr)
	}
	if receipt.sameInterface(after) && after.present {
		return errors.Join(writeErr, fmt.Errorf("delegated address %s remains after withdrawal", address))
	}
	receipt.forget(address)
	return c.saveDelegatedReceipt(name, status, receipt)
}

// Reconcile enumerates receipts as well as declarations, so removing the final
// PD/LAN resource cannot orphan an acquired address after a restart.
func (c LANAddressController) Reconcile(ctx context.Context) error {
	lister, ok := c.Store.(state.ObjectStatusLister)
	if !ok {
		return fmt.Errorf("delegated address receipt enumeration unavailable")
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return err
	}
	names := map[string]bool{}
	declared := c.DeclaredRouter
	if declared == nil {
		declared = c.Router
	}
	for _, name := range declaredPrefixDelegationNames(c.Router, declared) {
		names[name] = true
	}
	for _, object := range statuses {
		if object.APIVersion != api.NetAPIVersion || object.Kind != "IPv6DelegatedAddress" {
			continue
		}
		receipt, err := delegatedReceipt(object.Status)
		if err != nil {
			return err
		}
		if receipt != nil {
			names[receipt.PrefixDelegation] = true
		}
	}
	for name := range names {
		if err := c.reconcile(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

func (c LANAddressController) cleanupDelegatedReceipts(ctx context.Context, pdName string) error {
	lister, ok := c.Store.(state.ObjectStatusLister)
	if !ok {
		return fmt.Errorf("delegated address receipt enumeration unavailable")
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return err
	}
	type nativeKey struct {
		IfName         string
		Index          int
		Scope, Address string
	}
	owners := map[nativeKey]string{}
	for _, object := range statuses {
		if object.APIVersion != api.NetAPIVersion || object.Kind != "IPv6DelegatedAddress" {
			continue
		}
		receipt, err := delegatedReceipt(object.Status)
		if err != nil {
			return err
		}
		if receipt == nil {
			continue
		}
		for _, entry := range receipt.Addresses {
			key := nativeKey{receipt.IfName, receipt.IfIndex, receipt.Scope, entry.Address}
			if prior := owners[key]; prior != "" && prior != object.Name {
				return fmt.Errorf("delegated native address has conflicting owners %s and %s", prior, object.Name)
			}
			owners[key] = object.Name
		}
	}
	declared := c.DeclaredRouter
	if declared == nil {
		declared = c.Router
	}
	resources := map[string]api.Resource{}
	if declared != nil {
		for _, resource := range declared.Spec.Resources {
			if resource.Kind == "IPv6DelegatedAddress" {
				resources[resource.Metadata.Name] = resource
			}
		}
	}
	lease, _, err := livePDLease(c.Store, pdName, c.now())
	if err != nil {
		return err
	}

	for _, object := range statuses {
		if object.APIVersion != api.NetAPIVersion || object.Kind != "IPv6DelegatedAddress" {
			continue
		}
		receipt, err := delegatedReceipt(object.Status)
		if err != nil {
			return err
		}
		if receipt == nil || receipt.PrefixDelegation != pdName {
			continue
		}
		if !c.DryRun && len(receipt.Addresses) > 0 {
			observed, err := c.observeRecordedDelegatedAddress(ctx, receipt, receipt.Addresses[0].Address)
			if err != nil {
				return err
			}
			if !receipt.sameInterface(observed) {
				receipt.Addresses = nil
				if err := c.saveDelegatedReceipt(object.Name, object.Status, nil); err != nil {
					return err
				}
			}
		}
		resource, exists := resources[object.Name]
		wanted := map[string]bool{}
		reason := "ResourceRemoved"
		if exists {
			spec, err := resource.IPv6DelegatedAddressSpec()
			if err != nil {
				return err
			}
			stateStore, _ := c.Store.(resourcequery.StateStore)
			eligible, err := resourcequery.ResourceWhenEligible(spec.When, stateStore)
			if err != nil {
				return err
			}
			ifname := interfaceIfName(declared, spec.Interface)
			if ifname == "" {
				ifname = spec.Interface
			}
			reason = "WhenFalse"
			if eligible && spec.PrefixDelegation == pdName && ifname == receipt.IfName {
				reason = "PDLeaseStale"
				prefixes := []string{}
				if lease.Phase == "Bound" {
					if lease.HasFreshTransactionEvidence(c.now()) {
						prefixes = append(prefixes, lease.CurrentPrefix)
					}
					for _, prefix := range lease.ValidPreviousPrefixes(c.now()) {
						prefixes = append(prefixes, prefix.Prefix)
					}
				}
				for _, prefix := range prefixes {
					address, err := DeriveIPv6Address(prefix, spec.SubnetID, spec.AddressSuffix)
					if err != nil {
						return err
					}
					address, err = delegatedAddressWithPrefixLength(address, spec.PrefixLength)
					if err != nil {
						return err
					}
					wanted[address] = true
				}
			}
		}
		owned := append([]delegatedNativeAddress(nil), receipt.Addresses...)
		for _, entry := range owned {
			if !wanted[entry.Address] {
				if err := c.withdrawDelegatedReceiptAddress(ctx, object.Name, object.Status, receipt, entry.Address); err != nil {
					return err
				}
			}
		}
		if c.DryRun {
			continue
		}
		if !exists && len(receipt.Addresses) == 0 {
			deleter, ok := c.Store.(state.ObjectDeleteStore)
			if !ok {
				return fmt.Errorf("delegated status deletion unavailable")
			}
			if err := deleter.DeleteObject(api.NetAPIVersion, "IPv6DelegatedAddress", object.Name); err != nil {
				return err
			}
			continue
		}
		if len(receipt.Addresses) == 0 {
			status := copyStatusMap(object.Status)
			status["phase"], status["reason"] = "Pending", reason
			delete(status, "address")
			delete(status, "previousAddresses")
			delete(status, "staged")
			if err := c.saveDelegatedReceipt(object.Name, status, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c LANAddressController) fenceDelegatedAddress(name string, status map[string]any, receipt *delegatedNativeReceipt, address string) error {
	cause := fmt.Errorf("native delegated ownership unknown for %s on %s; inspect and remove the address before retrying", address, receipt.IfName)
	next := copyStatusMap(status)
	next["phase"], next["reason"], next["message"] = "Pending", "NativeOwnershipUnknown", cause.Error()
	return errors.Join(cause, c.saveDelegatedReceipt(name, next, receipt))
}

func (c LANAddressController) rejectForeignDelegatedAddress(name string, status map[string]any, receipt *delegatedNativeReceipt, address, ifname string) error {
	cause := fmt.Errorf("foreign IPv6 address %s on %s has no native ownership receipt", address, ifname)
	next := copyStatusMap(status)
	next["phase"], next["reason"], next["message"] = "Pending", "AddressAlreadyExists", cause.Error()
	return errors.Join(cause, c.saveDelegatedReceipt(name, next, receipt))
}
