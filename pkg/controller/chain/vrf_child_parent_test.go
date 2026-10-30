// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"slices"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestVRFForeignParentCannotAcquireStaticAddress(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VRF"}, Metadata: api.ObjectMeta{Name: "parent"}, Spec: api.VRFSpec{IfName: "vrf-foreign", RouteTable: 101}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "child"}, Spec: api.IPv4StaticAddressSpec{Interface: "parent", Address: "192.0.2.1/24"}},
	}}}
	present, writes := false, 0
	native := &vrfNativeFixture{scope: "vrf-fixture-kernel", links: map[string]vrfLink{"vrf-foreign": {Exists: true, Name: "vrf-foreign", Kind: "vrf", Index: 31, Table: 101, Up: true}}}
	parent := VRFController{Router: router, Store: store, OS: platform.OSLinux, Native: native}
	child := IPv4StaticAddressController{Router: router, Store: store, VRFs: &parent,
		ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
			observed := staticAddressSnapshot{Index: 31, Scope: "vrf-fixture-kernel"}
			if present {
				observed.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
			}
			return observed, nil
		},
		Command: func(context.Context, string, ...string) error { writes++; present = true; return nil },
	}
	err = child.Reconcile(t.Context())
	if writes != 0 || present {
		t.Fatalf("unacquired VRF parent admitted a child address: writes=%d err=%v", writes, err)
	}
}

// Replace only the concrete native I/O boundary. Receipts and all decisions use
// the production controller and an actual SQLite store.
type vrfNativeFixture struct {
	scope                  string
	links                  map[string]vrfLink
	ops                    []string
	failCookie, failDelete bool
	afterObserve           func(string)
}

func (n *vrfNativeFixture) Scope(ctx context.Context) (string, error) { return n.scope, ctx.Err() }
func (n *vrfNativeFixture) Observe(ctx context.Context, name string) (vrfLink, error) {
	scope := n.scope
	link := n.links[name]
	link.Name, link.Scope = name, scope
	if n.afterObserve != nil {
		n.afterObserve(name)
	}
	if n.scope != scope {
		return vrfLink{}, errors.New("fixture native scope changed during observation")
	}
	return link, ctx.Err()
}
func (n *vrfNativeFixture) exact(expected vrfLink, token string) (vrfLink, error) {
	link := n.links[expected.Name]
	if n.scope != expected.Scope || !link.Exists || link.Index != expected.Index || link.Kind != "vrf" || link.Table != expected.Table || link.Token != token {
		return vrfLink{}, errors.New("fixture native identity changed")
	}
	return link, nil
}
func (n *vrfNativeFixture) Create(ctx context.Context, plan vrfPlan, index int, scope string) error {
	if n.scope != scope || n.links[plan.Name].Exists || index < 1 {
		return errors.New("fixture exclusive create refused")
	}
	for _, link := range n.links {
		if link.Exists && link.Index == index {
			return errors.New("fixture exclusive index collision")
		}
	}
	n.ops = append(n.ops, "create")
	n.links[plan.Name] = vrfLink{Exists: true, Name: plan.Name, Kind: "vrf", Index: index, Table: plan.Table}
	return ctx.Err()
}
func (n *vrfNativeFixture) Cookie(ctx context.Context, expected vrfLink, token string) error {
	n.ops = append(n.ops, "cookie")
	link, err := n.exact(expected, "")
	if err != nil {
		return err
	}
	if n.failCookie {
		return errors.New("fixture cookie installation failed after create ACK")
	}
	link.Token = token
	n.links[expected.Name] = link
	return ctx.Err()
}
func (n *vrfNativeFixture) Up(ctx context.Context, expected vrfLink, token string) error {
	link, err := n.exact(expected, token)
	if err != nil {
		return err
	}
	n.ops = append(n.ops, "up")
	link.Up = true
	n.links[expected.Name] = link
	return ctx.Err()
}
func (n *vrfNativeFixture) Attach(ctx context.Context, port vrfPort, expected vrfLink, token string) error {
	if _, err := n.exact(expected, token); err != nil {
		return err
	}
	link := n.links[port.Name]
	if !link.Exists || link.Index != port.Index || link.Master != 0 && link.Master != expected.Index {
		return errors.New("fixture member identity changed")
	}
	n.ops = append(n.ops, "attach")
	link.Master = expected.Index
	n.links[port.Name] = link
	return ctx.Err()
}
func (n *vrfNativeFixture) Delete(ctx context.Context, expected vrfLink, token string) error {
	if _, err := n.exact(expected, token); err != nil {
		return err
	}
	n.ops = append(n.ops, "delete")
	if n.failDelete {
		return errors.New("fixture delete failed")
	}
	delete(n.links, expected.Name)
	for name, link := range n.links {
		if link.Master == expected.Index {
			link.Master = 0
			n.links[name] = link
		}
	}
	return ctx.Err()
}
func vrfTestOwner(t *testing.T) (VRFController, *vrfNativeFixture) {
	t.Helper()
	requireLinuxRuntimeFixture(t)
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "vrf-port"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VRF"}, Metadata: api.ObjectMeta{Name: "tenant"}, Spec: api.VRFSpec{IfName: "vrf101", RouteTable: 101, Members: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "address"}, Spec: api.IPv4StaticAddressSpec{Interface: "tenant", Address: "192.0.2.1/24"}},
	}}}
	native := &vrfNativeFixture{scope: "vrf-fixture-kernel", links: map[string]vrfLink{"vrf-port": {Exists: true, Name: "vrf-port", Kind: "veth", Index: 11, Up: true}}}
	return VRFController{Router: router, Store: store, OS: platform.OSLinux, Native: native}, native
}
func vrfOneReceipt(t *testing.T, owner VRFController) vrfReceipt {
	t.Helper()
	records, err := owner.readReceipts()
	if err != nil || len(records) != 1 {
		t.Fatalf("VRF receipt count=%d err=%v", len(records), err)
	}
	return records[0]
}
func vrfStaticChild(owner *VRFController, native *vrfNativeFixture, present *bool, writes *int) IPv4StaticAddressController {
	return IPv4StaticAddressController{Router: owner.Router, Store: owner.Store, VRFs: owner,
		ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
			observed := staticAddressSnapshot{Index: native.links["vrf101"].Index, Scope: native.scope}
			if *present {
				observed.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
			}
			return observed, nil
		},
		Command: func(_ context.Context, _ string, args ...string) error {
			*writes++
			*present = !slices.Contains(args, "del")
			return nil
		},
	}
}

func TestVRFParentReceiptOwnsChildAndWithdrawal(t *testing.T) {
	owner, native := vrfTestOwner(t)
	if err := owner.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	record := vrfOneReceipt(t, owner)
	if record.Pending || record.Withdrawing || record.Index < 1 || !native.links["vrf101"].Up || native.links["vrf-port"].Master != record.Index {
		t.Fatalf("VRF is not natively acknowledged: %+v", record)
	}
	before := len(native.ops)
	reopened := VRFController{Router: owner.Router, Store: owner.Store, OS: platform.OSLinux, Native: native}
	if err := reopened.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.ops) != before || !sameVRFReceipt(vrfOneReceipt(t, reopened), record) {
		t.Fatal("acknowledged VRF replayed native effects or replaced its receipt")
	}
	present, writes := false, 0
	child := vrfStaticChild(&reopened, native, &present, &writes)
	if err := child.Reconcile(t.Context()); err != nil || !present || writes != 1 {
		t.Fatalf("native child acquisition: writes=%d err=%v", writes, err)
	}
	status, err := owner.Store.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
	if err != nil {
		t.Fatal(err)
	}
	address, err := staticReceipt(status)
	if err != nil || address == nil || address.Pending || address.ParentToken != "vrf:"+record.Token || address.IfIndex != record.Index || address.Scope != record.Scope {
		t.Fatalf("child lacks exact VRF ownership: %+v err=%v", address, err)
	}
	// Recorded cleanup remains valid after the parent declaration disappears.
	reopened.Router, child.Router = &api.Router{}, &api.Router{}
	if err := child.Reconcile(t.Context()); err != nil || present || writes != 2 {
		t.Fatalf("recorded child cleanup: writes=%d err=%v", writes, err)
	}
	if err := reopened.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	records, err := reopened.readReceipts()
	if err != nil || len(records) != 0 || native.links["vrf101"].Exists || native.links["vrf-port"].Master != 0 {
		t.Fatalf("withdrawal did not retire native ownership: records=%v err=%v", records, err)
	}
}

func TestVRFUnconfirmedCreationNeverReplaysCookieOrForgetsReceipt(t *testing.T) {
	owner, native := vrfTestOwner(t)
	native.failCookie = true
	if err := owner.Reconcile(t.Context()); err == nil {
		t.Fatal("cookie installation failure was hidden")
	}
	record := vrfOneReceipt(t, owner)
	if record.Index != 0 || !record.Pending || !native.links["vrf101"].Exists || native.links["vrf101"].Token != "" {
		t.Fatal("fixture did not leave an unconfirmed native create")
	}
	native.failCookie = false
	before := len(native.ops)
	if err := owner.Reconcile(t.Context()); err == nil {
		t.Fatal("unconfirmed create was adopted after restart")
	}
	owner.Router = &api.Router{}
	if err := owner.Reconcile(t.Context()); err == nil {
		t.Fatal("unconfirmed native create was forgotten after declaration removal")
	}
	if len(native.ops) != before || !sameVRFReceipt(record, vrfOneReceipt(t, owner)) || !native.links["vrf101"].Exists {
		t.Fatal("unconfirmed create replayed a native effect or lost its receipt")
	}
}

func TestVRFFailedWithdrawalBlocksChildrenAndRetriesExactOwner(t *testing.T) {
	owner, native := vrfTestOwner(t)
	if err := owner.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	record := vrfOneReceipt(t, owner)
	router := owner.Router
	owner.Router = &api.Router{}
	native.failDelete = true
	if err := owner.Reconcile(t.Context()); err == nil {
		t.Fatal("failed deletion was hidden")
	}
	if current := vrfOneReceipt(t, owner); !current.Withdrawing || current.Token != record.Token {
		t.Fatal("failed withdrawal lost its ownership")
	}
	owner.Router = router
	reader := nativeParentReader{Router: router, Store: owner.Store, VRFs: &owner}
	if link, err := reader.observeDeclared(t.Context(), "vrf101"); err == nil || link.Exists {
		t.Fatal("withdrawing owner admitted a child")
	}
	native.failDelete = false
	owner.Router = &api.Router{}
	if err := owner.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if native.links["vrf101"].Exists {
		t.Fatal("retry failed to remove its exact owned interface")
	}
}

func TestVRFParentProofRejectsChangesAfterNativeWait(t *testing.T) {
	for _, change := range []string{"intent", "receipt", "scope"} {
		t.Run(change, func(t *testing.T) {
			owner, native := vrfTestOwner(t)
			if err := owner.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			reads := 0
			native.afterObserve = func(name string) {
				if name != "vrf101" {
					return
				}
				reads++
				if reads != 2 {
					return
				}
				switch change {
				case "intent":
					spec, _ := owner.Router.Spec.Resources[1].VRFSpec()
					spec.RouteTable = 102
					owner.Router.Spec.Resources[1].Spec = spec
				case "receipt":
					before := vrfOneReceipt(t, owner)
					record := before
					record.Pending = true
					if err := owner.saveReceipt(t.Context(), before, record); err != nil {
						t.Fatal(err)
					}
				case "scope":
					native.scope = "another-kernel"
				}
			}
			reader := nativeParentReader{Router: owner.Router, Store: owner.Store, VRFs: &owner}
			if link, err := reader.observeDeclared(t.Context(), "vrf101"); err == nil || link.Exists {
				t.Fatalf("changed %s admitted child proof: %+v err=%v", change, link, err)
			}
		})
	}
}
