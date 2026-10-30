// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type staticAddressReceipt struct {
	IfName      string `json:"ifname"`
	IfIndex     int    `json:"ifIndex"`
	Scope       string `json:"scope"`
	Address     string `json:"address"`
	Pending     bool   `json:"pending"`
	ParentToken string `json:"parentToken,omitempty"`
}

type staticAddressSnapshot struct {
	Index     int
	Scope     string
	Addresses []netip.Prefix
}

var errStaticParentNotReady = errors.New("native parent is not acquired")

func (c IPv4StaticAddressController) parentReader() nativeParentReader {
	router := c.ParentRouter
	if router == nil {
		router = c.WhenRouter
	}
	if router == nil {
		router = c.DeclaredRouter
	}
	if router == nil {
		router = c.Router
	}
	return nativeParentReader{Router: router, Store: c.Store, Segments: c.Segments, DSLite: c.DSLite}
}

// The declaration's native name alone cannot authorize a child effect.
func (c IPv4StaticAddressController) staticNativeParent(ctx context.Context, reference string, observed staticAddressSnapshot) (string, error) {
	reader := c.parentReader()
	link, err := reader.observeDeclared(ctx, interfaceIfName(reader.Router, reference))
	if err != nil {
		return "", err
	}
	if !link.Declared {
		return "", nil
	}
	if !link.Exists {
		return "", errStaticParentNotReady
	}
	if link.Index != observed.Index || link.Scope != observed.Scope || link.IfName != interfaceIfName(reader.Router, reference) {
		return "", errors.New("static address native parent identity changed")
	}
	return link.Token, nil
}

func (s staticAddressSnapshot) has(address string) bool {
	for _, prefix := range s.Addresses {
		if prefix.String() == address {
			return true
		}
	}
	return false
}

func (c IPv4StaticAddressController) staticAddresses(ctx context.Context, ifname string) (staticAddressSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return staticAddressSnapshot{}, err
	}
	if c.ObserveAddresses != nil {
		return c.ObserveAddresses(ctx, ifname)
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return staticAddressSnapshot{}, err
	}
	for _, iface := range interfaces {
		if iface.Name != ifname {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			return staticAddressSnapshot{}, err
		}
		scope, err := platform.KernelNetworkScope(ctx)
		if err != nil {
			return staticAddressSnapshot{}, err
		}
		snapshot := staticAddressSnapshot{Index: iface.Index, Scope: scope}
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err != nil {
				return staticAddressSnapshot{}, fmt.Errorf("invalid native address on %s: %w", ifname, err)
			}
			snapshot.Addresses = append(snapshot.Addresses, prefix)
		}
		return snapshot, ctx.Err()
	}
	return staticAddressSnapshot{}, ctx.Err()
}

// The only dynamic representation is the existing object-status boundary.
// Neither a phase nor a declared address is a substitute for this receipt.
func staticReceipt(status map[string]any) (*staticAddressReceipt, error) {
	value, present := status["nativeReceipt"]
	if !present {
		return nil, nil
	}
	var receipt staticAddressReceipt
	switch value := value.(type) {
	case staticAddressReceipt:
		receipt = value
	case map[string]any:
		if len(value) != 5 && len(value) != 6 {
			return nil, errors.New("invalid IPv4StaticAddress native receipt shape")
		}
		var ok bool
		if parent, exists := value["parentToken"]; exists {
			receipt.ParentToken, ok = parent.(string)
			if !ok || receipt.ParentToken == "" {
				return nil, errors.New("invalid static address parent token")
			}
		} else if len(value) != 5 {
			return nil, errors.New("invalid static address receipt field")
		}
		receipt.IfName, ok = value["ifname"].(string)
		if !ok {
			return nil, errors.New("invalid static address receipt interface")
		}
		receipt.Address, ok = value["address"].(string)
		if !ok {
			return nil, errors.New("invalid static address receipt address")
		}
		receipt.Pending, ok = value["pending"].(bool)
		if !ok {
			return nil, errors.New("invalid static address receipt pending state")
		}
		receipt.Scope, ok = value["scope"].(string)
		if !ok {
			return nil, errors.New("invalid static address receipt kernel scope")
		}
		index, ok := value["ifIndex"].(float64)
		if !ok || index < 1 || index > 2147483647 || index != float64(int(index)) {
			return nil, errors.New("invalid static address receipt interface index")
		}
		receipt.IfIndex = int(index)
	default:
		return nil, errors.New("invalid IPv4StaticAddress native receipt")
	}
	prefix, err := netip.ParsePrefix(receipt.Address)
	if err != nil || !prefix.Addr().Is4() || prefix.String() != receipt.Address || receipt.IfName == "" || receipt.IfIndex < 1 || receipt.Scope == "" {
		return nil, errors.New("invalid IPv4StaticAddress native identity")
	}
	if receipt.ParentToken != "" {
		if !validNativeParentToken(receipt.ParentToken) {
			return nil, errors.New("invalid static address parent token")
		}
	}
	return &receipt, nil
}

func (c IPv4StaticAddressController) saveStaticAddress(name string, status map[string]any, receipt *staticAddressReceipt) error {
	if receipt != nil {
		status["nativeReceipt"] = *receipt
	}
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", name, status)
}

func (c IPv4StaticAddressController) withdrawStaticAddress(ctx context.Context, name string, receipt *staticAddressReceipt) error {
	if receipt == nil || c.DryRun {
		return nil
	}
	observed, err := c.staticAddresses(ctx, receipt.IfName)
	if err != nil {
		return err
	}
	// An interface that disappeared or was replaced no longer carries our
	// acquired identity. Its replacement's addresses must survive.
	if observed.Scope != receipt.Scope || observed.Index != receipt.IfIndex || !observed.has(receipt.Address) {
		return nil
	}
	if receipt.Pending {
		return errors.New("static address operation outcome is unknown; inspect the native address and confirm absence before retrying")
	}
	if err := c.checkStaticWithdrawalParent(ctx, receipt, observed); err != nil {
		return err
	}
	// A matching address does not identify its creator. After a lost delete
	// acknowledgement, a foreign writer may have installed the same tuple.
	// Persist the ambiguity before withdrawal and never replay it on presence.
	status, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", name)
	if err != nil {
		return err
	}
	status = copyStatusMap(status)
	previous := copyStatusMap(status)
	status["phase"], status["reason"] = "Pending", "WithdrawingAddress"
	receipt.Pending = true
	if err := c.saveStaticAddress(name, status, receipt); err != nil {
		return err
	}
	abortBeforeDelete := func(cause error) error {
		// No native deletion was issued. Preserve the prior acquisition and
		// report the failed attempt without manufacturing a lost-reply fence.
		receipt.Pending = false
		previous["phase"], previous["reason"], previous["error"] = "Error", "WithdrawalNotStarted", cause.Error()
		return errors.Join(cause, c.saveStaticAddress(name, previous, receipt))
	}
	observed, err = c.staticAddresses(ctx, receipt.IfName)
	if err != nil {
		return abortBeforeDelete(err)
	}
	if observed.Scope != receipt.Scope || observed.Index != receipt.IfIndex || !observed.has(receipt.Address) {
		return nil
	}
	if err := c.checkStaticWithdrawalParent(ctx, receipt, observed); err != nil {
		return abortBeforeDelete(err)
	}
	command := c.Command
	if command == nil {
		command = runCommandContext
	}
	cmd, args := ipv4StaticAddressDeleteCommand(platform.CurrentOS(), receipt.IfName, receipt.Address)
	if err := command(ctx, cmd, args...); err != nil {
		return err
	}
	observed, err = c.staticAddresses(ctx, receipt.IfName)
	if err != nil {
		return err
	}
	if observed.Scope == receipt.Scope && observed.Index == receipt.IfIndex && observed.has(receipt.Address) {
		return errors.New("static address remains after withdrawal")
	}
	return c.staticAddressEvent(ctx, name, receipt.IfName, receipt.Address, "removed")
}

func (c IPv4StaticAddressController) checkStaticWithdrawalParent(ctx context.Context, receipt *staticAddressReceipt, observed staticAddressSnapshot) error {
	if receipt.ParentToken == "" {
		token, err := c.staticNativeParent(ctx, receipt.IfName, observed)
		if err != nil {
			return err
		}
		if token != "" {
			return errors.New("static address receipt lacks its native parent token")
		}
		return nil
	}
	reader := c.parentReader()
	link, err := reader.observeRecorded(ctx, receipt.IfName, receipt.ParentToken)
	if err != nil {
		return err
	}
	if !link.Exists || link.Scope != receipt.Scope || link.Index != observed.Index || link.Token != receipt.ParentToken || link.IfName != receipt.IfName {
		return errors.New("static address cleanup parent identity is no longer proven")
	}
	return nil
}

func (c IPv4StaticAddressController) staticAddressEvent(ctx context.Context, name, ifname, address, action string) error {
	if c.Bus == nil {
		return nil
	}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.lan.ipv4_address."+action, daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress", Name: name}
	event.Attributes = map[string]string{"ifname": ifname, "address": address, "dryRun": fmt.Sprint(c.DryRun)}
	return c.Bus.Publish(ctx, event)
}

func (c IPv4StaticAddressController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return errors.New("static address reconciliation requires current Router and Store")
	}
	// Enumeration must work before acquisition, so removal can recover an
	// interrupted invocation without reconstructing intent from declarations.
	lister, ok := c.Store.(interface {
		ListObjectStatuses() ([]routerstate.ObjectStatus, error)
	})
	if !ok {
		return errors.New("static address receipt enumeration is unavailable")
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return err
	}
	processed := map[string]bool{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "IPv4StaticAddress" {
			continue
		}
		processed[resource.Metadata.Name] = true
		if err := c.reconcileStaticAddress(ctx, resource); err != nil {
			return c.staticAddressFailure(resource.Metadata.Name, err)
		}
	}
	for _, object := range statuses {
		if object.APIVersion != api.NetAPIVersion || object.Kind != "IPv4StaticAddress" || processed[object.Name] {
			continue
		}
		receipt, err := staticReceipt(object.Status)
		if err != nil {
			return err
		}
		if err := c.withdrawStaticAddress(ctx, object.Name, receipt); err != nil {
			return c.staticAddressFailure(object.Name, err)
		}
		if c.DryRun {
			continue
		}
		declared := c.DeclaredRouter
		if c.WhenRouter != nil {
			declared = c.WhenRouter
		}
		if staticAddressDeclared(declared, object.Name) {
			status := copyStatusMap(object.Status)
			delete(status, "nativeReceipt")
			status["phase"], status["reason"] = "Pending", "WhenFalse"
			if receipt != nil {
				status["staticAddressRemoved"] = true
			}
			if err := c.saveStaticAddress(object.Name, status, nil); err != nil {
				return err
			}
		} else {
			deleter, ok := c.Store.(routerstate.ObjectDeleteStore)
			if !ok {
				return errors.New("static address status deletion is unavailable")
			}
			if err := deleter.DeleteObject(api.NetAPIVersion, "IPv4StaticAddress", object.Name); err != nil {
				return err
			}
		}
	}
	return c.reconcileMobilityStaticAddresses(ctx)
}

func (c IPv4StaticAddressController) staticAddressFailure(name string, cause error) error {
	// Read again: a failed effect may already have persisted a pending fence.
	// A display update must never replace it with the pre-attempt receipt.
	status, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", name)
	if err != nil {
		return errors.Join(cause, err)
	}
	status = copyStatusMap(status)
	status["phase"], status["error"] = "Error", cause.Error()
	return fmt.Errorf("IPv4StaticAddress/%s: %w", name, errors.Join(cause, c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", name, status)))
}

func staticAddressDeclared(router *api.Router, name string) bool {
	if router != nil {
		for _, resource := range router.Spec.Resources {
			if resource.Kind == "IPv4StaticAddress" && resource.Metadata.Name == name {
				return true
			}
		}
	}
	return false
}

func (c IPv4StaticAddressController) staticAddressEligible(spec api.IPv4StaticAddressSpec) (bool, error) {
	if !resourcequery.ResourceWhenPresent(spec.When) {
		return true, nil
	}
	state, ok := c.Store.(resourcequery.StateStore)
	if !ok {
		return false, errors.New("static address condition reading is unavailable")
	}
	return resourcequery.ResourceWhenMatches(spec.When, state)
}

func (c IPv4StaticAddressController) reconcileStaticAddress(ctx context.Context, resource api.Resource) error {
	spec, err := resource.IPv4StaticAddressSpec()
	if err != nil {
		return err
	}
	if spec.Exclusive {
		return errors.New("IPv4StaticAddress exclusive takeover is not implemented")
	}
	previous, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", resource.Metadata.Name)
	if err != nil {
		return err
	}
	receipt, err := staticReceipt(previous)
	if err != nil {
		return err
	}
	status := map[string]any{"phase": "Applied", "interface": spec.Interface, "address": spec.Address, "dryRun": c.DryRun}
	eligible, err := c.staticAddressEligible(spec)
	if err != nil {
		return err
	}
	ifname := interfaceIfName(c.Router, spec.Interface)
	if !eligible || ifname == "" {
		if err := c.withdrawStaticAddress(ctx, resource.Metadata.Name, receipt); err != nil {
			return err
		}
		status["phase"], status["reason"] = "Pending", "WhenFalse"
		if eligible {
			status["reason"] = "InterfaceMissing"
		}
		if c.DryRun {
			return c.saveStaticAddress(resource.Metadata.Name, status, receipt)
		}
		return c.saveStaticAddress(resource.Metadata.Name, status, nil)
	}
	status["ifname"] = ifname
	if c.DryRun {
		status["phase"] = "Rendered"
		return c.saveStaticAddress(resource.Metadata.Name, status, receipt)
	}
	observed, err := c.staticAddresses(ctx, ifname)
	if err != nil {
		return err
	}
	if observed.Index != 0 && observed.Scope == "" {
		return errors.New("static address observation has no kernel identity")
	}
	if receipt != nil && (receipt.IfName != ifname || receipt.Address != spec.Address || receipt.IfIndex != observed.Index || receipt.Scope != observed.Scope) {
		if err := c.withdrawStaticAddress(ctx, resource.Metadata.Name, receipt); err != nil {
			return err
		}
		if err := c.saveStaticAddress(resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "ReplacingAddress"}, nil); err != nil {
			return err
		}
		receipt = nil
	}
	if observed.Index == 0 {
		status["phase"], status["reason"] = "Pending", "InterfaceNotPresent"
		return c.saveStaticAddress(resource.Metadata.Name, status, nil)
	}
	parentToken, err := c.staticNativeParent(ctx, spec.Interface, observed)
	if errors.Is(err, errStaticParentNotReady) {
		status["phase"], status["reason"] = "Pending", "ParentNotReady"
		return c.saveStaticAddress(resource.Metadata.Name, status, receipt)
	}
	if err != nil {
		return err
	}
	if receipt != nil && receipt.ParentToken != parentToken {
		return errors.New("static address receipt does not prove its current virtual parent")
	}
	if receipt == nil && observed.has(spec.Address) {
		return errors.New("declared address already exists without a native ownership receipt")
	}
	if receipt != nil && receipt.Pending && observed.has(spec.Address) {
		return errors.New("static address operation outcome is unknown; matching presence does not prove ownership")
	}
	changed := receipt == nil || receipt.Pending || !observed.has(spec.Address)
	if !observed.has(spec.Address) {
		receipt = &staticAddressReceipt{IfName: ifname, IfIndex: observed.Index, Scope: observed.Scope, Address: spec.Address, Pending: true, ParentToken: parentToken}
		status["phase"] = "Pending"
		if err := c.saveStaticAddress(resource.Metadata.Name, status, receipt); err != nil {
			return err
		}
		abortBeforeCommand := func(cause error) error {
			status["phase"], status["reason"], status["error"] = "Error", "AcquisitionNotStarted", cause.Error()
			delete(status, "nativeReceipt")
			return errors.Join(cause, c.saveStaticAddress(resource.Metadata.Name, status, nil))
		}
		// The durable write can wait for another SQLite writer. Neither a
		// role transition nor expiry during that wait authorizes a new effect.
		eligible, err := c.staticAddressEligible(spec)
		if err != nil {
			return abortBeforeCommand(err)
		}
		if !eligible {
			status["phase"], status["reason"] = "Pending", "WhenFalse"
			delete(status, "nativeReceipt")
			return c.saveStaticAddress(resource.Metadata.Name, status, nil)
		}
		current, err := c.staticAddresses(ctx, ifname)
		if err != nil {
			return abortBeforeCommand(err)
		}
		if current.Scope != receipt.Scope || current.Index != receipt.IfIndex || current.has(spec.Address) {
			return abortBeforeCommand(errors.New("native interface or address changed while recording acquisition intent"))
		}
		currentParent, err := c.staticNativeParent(ctx, spec.Interface, current)
		if err != nil {
			return abortBeforeCommand(err)
		}
		if currentParent != parentToken {
			return abortBeforeCommand(errors.New("native parent changed while recording address intent"))
		}
		command := c.Command
		if command == nil {
			command = runCommandContext
		}
		cmd, args := ipv4StaticAddressApplyCommand(platform.CurrentOS(), ifname, spec.Address)
		if platform.CurrentOS() == platform.OSLinux {
			// Acquire without overwriting an address installed after our read.
			args[2] = "add"
		}
		if err := command(ctx, cmd, args...); err != nil {
			return err
		}
		observed, err = c.staticAddresses(ctx, ifname)
		if err != nil {
			return err
		}
		if observed.Scope != receipt.Scope || observed.Index != receipt.IfIndex || !observed.has(spec.Address) {
			return errors.New("static address acquisition was not confirmed on the same interface")
		}
		currentParent, err = c.staticNativeParent(ctx, spec.Interface, observed)
		if err != nil {
			return err
		}
		if currentParent != parentToken {
			return errors.New("native parent changed during address acquisition")
		}
		// Only this invocation has both the successful exclusive add and its
		// readback. A restarted invocation sees the durable pending record.
		receipt.Pending = false
	}
	eligible, err = c.staticAddressEligible(spec)
	if err != nil {
		return err
	}
	if !eligible {
		if err := c.withdrawStaticAddress(ctx, resource.Metadata.Name, receipt); err != nil {
			return err
		}
		status["phase"], status["reason"] = "Pending", "WhenFalse"
		delete(status, "nativeReceipt")
		return c.saveStaticAddress(resource.Metadata.Name, status, nil)
	}
	receipt.Pending = false
	status["phase"] = "Applied"
	if err := c.saveStaticAddress(resource.Metadata.Name, status, receipt); err != nil {
		return err
	}
	if changed {
		return c.staticAddressEvent(ctx, resource.Metadata.Name, ifname, strings.TrimSpace(spec.Address), "applied")
	}
	return nil
}
