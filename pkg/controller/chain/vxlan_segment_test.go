// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"github.com/imksoo/routerd/pkg/vxlan"
)

const segmentTestScope = "linux:11111111-1111-1111-1111-111111111111:7"

// Only one kernel link is needed to exercise the actual receipt owner. Native
// platform parsing and identity/effect guards are tested in pkg/vxlan.
type segmentNativeFixture struct {
	link                                      vxlan.SegmentLink
	scope                                     string
	creates, configured, removed, activations int
	readErr, configureErr, removeErr          error
	beforeCreate                              func(vxlan.SegmentIdentity)
	afterCreate, afterConfigure               func()
}

func (n *segmentNativeFixture) KernelScope(context.Context) (string, error) {
	return n.scope, n.readErr
}

func (n *segmentNativeFixture) Resolve(_ context.Context, cfg vxlan.SegmentConfig, token string) (vxlan.SegmentIdentity, error) {
	identity := vxlan.SegmentIdentity{Scope: n.scope, Token: token, CreationName: cfg.IfName, UnderlayIndex: 2}
	if cfg.Bridge != "" {
		identity.BridgeIndex = 3
	}
	return identity, nil
}
func (n *segmentNativeFixture) Observe(context.Context, string) (vxlan.SegmentLink, error) {
	link := n.link
	link.Scope = n.scope
	return link, n.readErr
}
func (n *segmentNativeFixture) ObserveOwned(ctx context.Context, cfg vxlan.SegmentConfig, _ vxlan.SegmentIdentity) (vxlan.SegmentLink, error) {
	return n.Observe(ctx, cfg.IfName)
}
func (n *segmentNativeFixture) Create(_ context.Context, cfg vxlan.SegmentConfig, owner vxlan.SegmentIdentity) error {
	if n.beforeCreate != nil {
		n.beforeCreate(owner)
	}
	n.creates++
	n.link = vxlan.SegmentLink{Exists: true, Scope: n.scope, Index: n.creates + 7, Token: owner.Token, Kind: "vxlan", Config: cfg, LocalPort: cfg.Port, UnderlayIndex: owner.UnderlayIndex}
	if n.afterCreate != nil {
		n.afterCreate()
	}
	return nil
}
func (n *segmentNativeFixture) Configure(ctx context.Context, cfg vxlan.SegmentConfig, owner vxlan.SegmentIdentity, admit func(context.Context) error) error {
	n.configured++
	if err := admit(ctx); err != nil {
		return err
	}
	n.link.Up, n.link.Running, n.link.BridgeIndex = true, true, owner.BridgeIndex
	n.link.Flood = append([]string(nil), cfg.Remotes...)
	n.activations++
	if n.afterConfigure != nil {
		n.afterConfigure()
	}
	return n.configureErr
}
func (n *segmentNativeFixture) Remove(context.Context, vxlan.SegmentConfig, vxlan.SegmentIdentity) error {
	n.removed++
	if n.removeErr != nil {
		return n.removeErr
	}
	n.link = vxlan.SegmentLink{Scope: n.scope}
	return nil
}

func segmentOwnerFixture(t *testing.T) (VXLANSegmentController, *segmentNativeFixture, *routerstate.SQLiteStore, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "uplink"}, Spec: api.InterfaceSpec{IfName: "underlay0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "bridge"}, Spec: api.BridgeSpec{IfName: "bridge0", Members: []string{"segment"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANSegment"}, Metadata: api.ObjectMeta{Name: "segment"}, Spec: api.VXLANSegmentSpec{IfName: "vxlan100", VNI: 100, LocalAddress: "192.0.2.1", Remotes: []string{"192.0.2.2", "192.0.2.3"}, UnderlayInterface: "uplink", MTU: 1400}},
	}}}
	native := &segmentNativeFixture{scope: segmentTestScope}
	controller := VXLANSegmentController{Router: router, Store: store, Native: native, StateDir: dir, scope: segmentTestScope, Admit: func(context.Context, string, vxlan.SegmentConfig) (bool, error) { return true, nil }}
	return controller, native, store, path
}

func oneSegmentOwner(t *testing.T, controller VXLANSegmentController) vxlanSegmentOwner {
	t.Helper()
	owners, err := controller.readOwners()
	if err != nil || len(owners) != 1 {
		t.Fatalf("expected one durable owner: %+v %v", owners, err)
	}
	return owners[0]
}

func TestVXLANSegmentPendingBeforeCreateAndRestartWithdrawal(t *testing.T) {
	c, native, store, path := segmentOwnerFixture(t)
	native.beforeCreate = func(identity vxlan.SegmentIdentity) {
		owner := oneSegmentOwner(t, c)
		if !owner.Identity.Pending || owner.Identity.Index != 0 || owner.Identity.Token != identity.Token || owner.Identity.CreationName != identity.CreationName {
			t.Fatalf("native creation preceded durable acquisition intent: %+v", owner)
		}
	}
	failure := errors.New("native success, reply failed")
	native.configureErr = failure
	if err := c.Reconcile(t.Context()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	owner := oneSegmentOwner(t, c)
	if !owner.Identity.Pending || owner.Identity.Index != native.link.Index || !native.link.Up || len(native.link.Flood) != 2 || native.link.BridgeIndex != 3 {
		t.Fatalf("partial native result lost ownership: %+v %+v", owner, native.link)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	c.Store, c.Router = reopened, &api.Router{}
	native.removeErr = failure
	if err := c.Reconcile(t.Context()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if !oneSegmentOwner(t, c).Withdrawing || !native.link.Exists {
		t.Fatal("failed cleanup forgot a possible native effect")
	}
	native.removeErr = nil
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if owners, err := c.readOwners(); err != nil || len(owners) != 0 || native.link.Exists {
		t.Fatalf("restart did not complete exact pending withdrawal: %+v %+v %v", owners, native.link, err)
	}
}

func TestVXLANSegmentForeignReplacementPreservesReceipt(t *testing.T) {
	c, native, _, _ := segmentOwnerFixture(t)
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	native.link.Index++
	native.link.Token = "foreign"
	c.Router = &api.Router{}
	if err := c.Reconcile(t.Context()); err == nil {
		t.Fatal("foreign replacement accepted as owned absence")
	}
	if native.removed != 0 || !oneSegmentOwner(t, c).Withdrawing {
		t.Fatalf("unsafe cleanup or lost recovery: calls=%d", native.removed)
	}
}

func TestVXLANSegmentNamespacesSharingFilesystemKeepIndependentOwners(t *testing.T) {
	c, native, _, _ := segmentOwnerFixture(t)
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	original := oneSegmentOwner(t, c)
	secondNative := &segmentNativeFixture{scope: "linux:11111111-1111-1111-1111-111111111111:8"}
	second := c
	second.Native, second.scope = secondNative, secondNative.scope
	if err := second.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	other := oneSegmentOwner(t, second)
	if other.Identity.Token == original.Identity.Token || second.receiptDir() == c.receiptDir() {
		t.Fatal("independent native realms shared an acquisition")
	}
	second.Router = &api.Router{}
	if err := second.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if secondNative.removed != 1 || native.removed != 0 || oneSegmentOwner(t, c).Withdrawing || oneSegmentOwner(t, c).Identity.Token != original.Identity.Token {
		t.Fatal("withdrawal reached another namespace's receipt/effect")
	}
	native.scope = secondNative.scope
	c.Router = &api.Router{}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if native.removed != 0 || oneSegmentOwner(t, c).Withdrawing {
		t.Fatal("entering a new native scope claimed an old realm's receipt")
	}
}

func TestVXLANSegmentCurrentAdmissionClosesChangedIntentButPreservesReadError(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "known withdrawn", true: "unknown read"}[unknown], func(t *testing.T) {
			c, native, _, _ := segmentOwnerFixture(t)
			live := true
			failure := errors.New("current source unreadable")
			c.Admit = func(context.Context, string, vxlan.SegmentConfig) (bool, error) {
				if !live && unknown {
					return false, failure
				}
				return live, nil
			}
			native.afterCreate = func() { live = false }
			err := c.Reconcile(t.Context())
			if unknown {
				if !errors.Is(err, failure) || native.removed != 0 || !oneSegmentOwner(t, c).Identity.Pending {
					t.Fatalf("unknown source became withdrawal/success: removed=%d err=%v", native.removed, err)
				}
			} else if err != nil || native.removed != 1 || native.link.Exists {
				t.Fatalf("known changed intent retained its capture: %+v %v", native.link, err)
			}
			if native.activations != 0 {
				t.Fatal("native forwarding activated after its source changed")
			}
		})
	}
}

func TestVXLANSegmentStatusFailureRetainsActualAppliedOwner(t *testing.T) {
	c, native, store, path := segmentOwnerFixture(t)
	native.afterConfigure = func() {
		native.afterConfigure = nil
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	}
	if err := c.Reconcile(t.Context()); err == nil {
		t.Fatal("closed status publisher reported successful apply")
	}
	owner := oneSegmentOwner(t, c)
	if owner.Identity.Pending || !native.link.Up {
		t.Fatal("public status failure discarded the actual native result")
	}
	reopened, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	c.Store = reopened
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if native.creates != 1 || native.activations != 1 || oneSegmentOwner(t, c).Identity.Token != owner.Identity.Token {
		t.Fatal("status retry reacquired an already owned native interface")
	}
}

func TestVXLANSegmentPendingUnboundAcquisitionCanWithdraw(t *testing.T) {
	c, native, _, _ := segmentOwnerFixture(t)
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	owner := oneSegmentOwner(t, c)
	owner.Identity.Pending, owner.Identity.Index = true, 0
	if err := c.saveOwner(owner); err != nil {
		t.Fatal(err)
	}
	c.Router = &api.Router{}
	if err := c.Reconcile(t.Context()); err != nil || native.link.Exists || native.removed != 1 {
		t.Fatalf("create-success/bind-save crash could not withdraw: %+v %v", native.link, err)
	}
}

func TestVXLANSegmentActualRuntimeOwnsDynamicExpiryAndBridgePort(t *testing.T) {
	c, native, store, _ := segmentOwnerFixture(t)
	base := *c.Router
	base.Spec.Resources = append([]api.Resource(nil), c.Router.Spec.Resources[:2]...)
	base.TypeMeta = api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}
	base.Metadata = api.ObjectMeta{Name: "runtime"}
	// Bridge itself stays declared without claiming the dynamically owned port.
	spec, _ := base.Spec.Resources[1].BridgeSpec()
	spec.Members = []string{"uplink"}
	base.Spec.Resources[1].Spec = spec
	dynamic := c.Router.Spec.Resources[2]
	segment, _ := dynamic.VXLANSegmentSpec()
	segment.Bridge = "bridge"
	dynamic.Spec = segment
	now := time.Now().UTC()
	part := dynamicconfig.NewPart("segments", "segments", nil, 1, now, now.Add(time.Second))
	part.Spec.Resources = []api.Resource{dynamic}
	record, err := codec.Encode(part)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		t.Fatal(err)
	}
	eventBus := bus.New()
	runner := &Runner{Router: &base, Store: store, Bus: eventBus}
	controller := runner.vxlanSegmentController(eventedStore{Store: store, Bus: eventBus}, c)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- (framework.Runner{Bus: eventBus}).Run(ctx, controller) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("native segment runtime failed to join")
		}
	}()
	phases := []string{"Healthy", "Removed"}
	for _, phase := range phases {
		deadline := time.After(2 * time.Second)
		for {
			status, err := store.ReadObjectStatus(api.NetAPIVersion, "VXLANSegment", "segment")
			if err != nil {
				t.Fatal(err)
			}
			if status["phase"] == phase {
				break
			}
			select {
			case <-deadline:
				t.Fatalf("dynamic Segment never reached %s: %+v", phase, status)
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
	cancel()
	err = <-done
	done <- err
	if native.creates != 1 || native.activations != 1 || native.removed != 1 || native.link.Exists {
		t.Fatalf("expiry did not withdraw the same owner: %+v", native)
	}
	owners, err := c.readOwners()
	if err != nil || len(owners) != 0 {
		t.Fatalf("withdrawal left owner: %+v %v", owners, err)
	}
}

func TestVXLANSegmentParentProofUsesActualJournalAndNativeCookie(t *testing.T) {
	c, native, _, _ := segmentOwnerFixture(t)
	link, err := c.ObserveOwnedInterface(t.Context(), api.NetAPIVersion+"/VXLANSegment/segment")
	if err != nil || link.Exists {
		t.Fatalf("unacquired parent admitted: %+v %v", link, err)
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	link, err = c.ObserveOwnedInterface(t.Context(), api.NetAPIVersion+"/VXLANSegment/segment")
	if err != nil || !link.Exists || link.Index != native.link.Index || link.Scope != native.scope {
		t.Fatalf("applied native parent rejected: %+v %v", link, err)
	}
	native.link.Token = "foreign"
	if _, err := c.ObserveOwnedInterface(t.Context(), api.NetAPIVersion+"/VXLANSegment/segment"); err == nil {
		t.Fatal("foreign replacement admitted by file/status alone")
	}
	owner := oneSegmentOwner(t, c)
	owner.Identity.Pending = true
	if err := c.saveOwner(owner); err != nil {
		t.Fatal(err)
	}
	native.link.Token = owner.Identity.Token
	if _, err := c.ObserveOwnedInterface(t.Context(), api.NetAPIVersion+"/VXLANSegment/segment"); err == nil {
		t.Fatal("unfinished parent acquisition admitted")
	}
}

func TestVXLANSegmentRecordedParentSurvivesDeclarationRemoval(t *testing.T) {
	parent, native, _, _ := segmentOwnerFixture(t)
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	owner := oneSegmentOwner(t, parent)
	parent.Router = &api.Router{}
	native.link.Up, native.link.Running = false, false
	link, err := parent.ObserveRecordedInterface(t.Context(), owner.Plan.IfName, owner.Identity.Token)
	if err != nil || !link.Exists || link.Token != owner.Identity.Token {
		t.Fatalf("declared removal lost exact cleanup proof: %+v %v", link, err)
	}
	native.link.Token = "foreign"
	if _, err := parent.ObserveRecordedInterface(t.Context(), owner.Plan.IfName, owner.Identity.Token); err == nil {
		t.Fatal("foreign replacement passed recorded-parent proof")
	}
	if err := parent.removeOwner(owner.Resource); err != nil {
		t.Fatal(err)
	}
	if _, err := parent.ObserveRecordedInterface(t.Context(), owner.Plan.IfName, owner.Identity.Token); err == nil {
		t.Fatal("missing parent journal became cleanup authority")
	}
	native.link = vxlan.SegmentLink{Scope: native.scope}
	link, err = parent.ObserveRecordedInterface(t.Context(), owner.Plan.IfName, owner.Identity.Token)
	if err != nil || link.Exists {
		t.Fatalf("confirmed absent parent could not retire: %+v %v", link, err)
	}
}

func TestVXLANSegmentOwnsBridgeMembershipExactlyOnce(t *testing.T) {
	c, _, _, _ := segmentOwnerFixture(t)
	bridge := c.Router.Spec.Resources[1]
	plan, err := (BridgeController{Router: c.Router}).bridgePlan(bridge)
	if err != nil || len(plan.Members) != 0 {
		t.Fatalf("Bridge claims Segment native port: %+v %v", plan, err)
	}
	plans, err := segmentPlans(c.Router)
	if err != nil || plans[api.NetAPIVersion+"/VXLANSegment/segment"].Bridge != "bridge0" {
		t.Fatalf("Segment lost declared Bridge.Members association: %+v %v", plans, err)
	}
}

func TestVXLANSegmentActualRuntimeReadFailureCannotActivateOrWithdrawPending(t *testing.T) {
	c, native, store, _ := segmentOwnerFixture(t)
	eventBus := bus.New()
	runner := &Runner{Router: c.Router, Store: store, Bus: eventBus}
	controller := runner.vxlanSegmentController(eventedStore{Store: store, Bus: eventBus}, c)
	native.afterCreate = func() {
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
	_, err := controller.PeriodicReconcile(t.Context())
	if err == nil || native.creates != 1 || native.activations != 0 || native.removed != 0 || !native.link.Exists || !oneSegmentOwner(t, c).Identity.Pending {
		t.Fatalf("unreadable live source became activation/withdrawal/success: native=%+v err=%v", native, err)
	}
}

func TestVXLANSegmentActualRuntimeWithdrawsWhenUnderlayIsSuppressed(t *testing.T) {
	c, native, store, _ := segmentOwnerFixture(t)
	iface, _ := c.Router.Spec.Resources[0].InterfaceSpec()
	iface.When = api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}}
	c.Router.Spec.Resources[0].Spec = iface
	if _, err := store.Set("ha.role", "master", "test"); err != nil {
		t.Fatal(err)
	}
	eventBus := bus.New()
	runner := &Runner{Router: c.Router, Store: store, Bus: eventBus}
	controller := runner.vxlanSegmentController(eventedStore{Store: store, Bus: eventBus}, c)
	if _, err := controller.PeriodicReconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if native.creates != 1 || !native.link.Up {
		t.Fatal("eligible Segment did not activate")
	}
	if _, err := store.Set("ha.role", "backup", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.PeriodicReconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if native.removed != 1 || native.link.Exists {
		t.Fatal("known underlay suppression left owned capture active")
	}
	owners, err := c.readOwners()
	if err != nil || len(owners) != 0 {
		t.Fatalf("known suppression left a journal: %+v %v", owners, err)
	}
}
