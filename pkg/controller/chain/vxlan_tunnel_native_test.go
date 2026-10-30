// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	state "github.com/imksoo/routerd/pkg/state"
	"github.com/imksoo/routerd/pkg/vxlan"
)

func tunnelOwnerFixture(t *testing.T) (VXLANSegmentController, *segmentNativeFixture, *state.SQLiteStore) {
	t.Helper()
	c, native, store, _ := segmentOwnerFixture(t)
	c.StartedAt = time.Now().UTC()
	c.Router.Spec.Resources[2] = api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "segment"}, Spec: api.VXLANTunnelSpec{IfName: "vxlan100", VNI: 100, LocalAddress: "192.0.2.1", Peers: []string{"192.0.2.2"}, UnderlayInterface: "uplink", MTU: 1400, When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}}}}
	if _, err := store.Set("ha.role", "master", "test"); err != nil {
		t.Fatal(err)
	}
	return c, native, store
}

func TestVXLANTunnelUsesOneNativeOwnerAcrossRestartAndGateWithdrawal(t *testing.T) {
	c, native, store := tunnelOwnerFixture(t)
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	owner := oneSegmentOwner(t, c)
	if owner.Resource != api.NetAPIVersion+"/VXLANTunnel/segment" || owner.Identity.Pending || !owner.Plan.NoLearning || native.creates != 1 {
		t.Fatalf("incomplete native tunnel ownership: %+v", owner)
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if native.creates != 1 {
		t.Fatal("unchanged tunnel created twice")
	}
	if _, err := store.Set("ha.role", "backup", "demotion"); err != nil {
		t.Fatal(err)
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if native.link.Exists || native.removed != 1 {
		t.Fatalf("gate demotion did not withdraw native owner: %+v", native)
	}
	if owners, err := c.readOwners(); err != nil || len(owners) != 0 {
		t.Fatalf("completed withdrawal retained receipt: %+v %v", owners, err)
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "VXLANTunnel", "segment")
	if err != nil || status["phase"] != "Removed" {
		t.Fatalf("tunnel result published under wrong identity: %+v %v", status, err)
	}
}

func TestVXLANTunnelGateRevisionAfterAdmissionCannotActivate(t *testing.T) {
	c, native, store := tunnelOwnerFixture(t)
	c.Admit = func(context.Context, string, vxlan.SegmentConfig) (bool, error) {
		if native.creates > 0 {
			if _, err := store.Set("ha.role", "master", "new witness revision during slow admission"); err != nil {
				t.Fatal(err)
			}
		}
		return true, nil
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if native.activations != 0 || native.link.Exists || native.removed != 1 {
		t.Fatalf("changed witness activated forwarding: %+v", native)
	}
}

func TestVXLANTunnelPreBootGateCannotAcquire(t *testing.T) {
	c, native, _ := tunnelOwnerFixture(t)
	c.StartedAt = time.Now().UTC().Add(time.Second)
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if native.creates != 0 {
		t.Fatal("persisted pre-boot witness acquired tunnel")
	}
}

func TestVXLANTunnelForeignReplacementCannotBeWithdrawn(t *testing.T) {
	c, native, store := tunnelOwnerFixture(t)
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	native.link.Token = "foreign"
	if _, err := store.Set("ha.role", "backup", "demotion"); err != nil {
		t.Fatal(err)
	}
	if err := c.Reconcile(t.Context()); err == nil {
		t.Fatal("foreign replacement accepted")
	}
	if native.removed != 0 || !native.link.Exists || !oneSegmentOwner(t, c).Withdrawing {
		t.Fatal("foreign replacement deleted or recovery evidence lost")
	}
}

func TestVXLANTunnelUnknownObservationRetainsPendingOwner(t *testing.T) {
	c, native, store := tunnelOwnerFixture(t)
	native.afterCreate = func() {
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Reconcile(t.Context()); err == nil {
		t.Fatal("closed observation source accepted")
	}
	if native.activations != 0 || native.removed != 0 || !oneSegmentOwner(t, c).Identity.Pending {
		t.Fatal("unknown gate became activation, replay or lost pending ownership")
	}
}

type gateChangeNative struct {
	*segmentNativeFixture
	change func()
}

func (n *gateChangeNative) ObserveOwned(ctx context.Context, cfg vxlan.SegmentConfig, id vxlan.SegmentIdentity) (vxlan.SegmentLink, error) {
	link, err := n.segmentNativeFixture.ObserveOwned(ctx, cfg, id)
	if n.change != nil {
		n.change()
	}
	return link, err
}

func TestVXLANTunnelParentGateChangeDuringNativeObservation(t *testing.T) {
	c, native, store := tunnelOwnerFixture(t)
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	c.Native = &gateChangeNative{segmentNativeFixture: native, change: func() {
		if _, err := store.Set("ha.role", "backup", "changed while native read waited"); err != nil {
			t.Fatal(err)
		}
	}}
	link, err := c.ObserveOwnedInterface(t.Context(), api.NetAPIVersion+"/VXLANTunnel/segment")
	if err == nil && link.Exists {
		t.Fatal("parent authorized child after its gate changed during native observation")
	}
}

func TestVXLANTunnelChildrenUseCurrentGateObservations(t *testing.T) {
	c, native, store := tunnelOwnerFixture(t)
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	current := staticAddressSnapshot{Scope: native.scope, Index: native.link.Index}
	static := IPv4StaticAddressController{Router: c.Router, Store: store, Segments: &c}
	token, err := static.staticSegmentParent(t.Context(), "segment", current)
	if err != nil || token != native.link.Token {
		t.Fatalf("current native tunnel rejected as static parent: %q %v", token, err)
	}
	lan := LANAddressController{Router: c.Router, DeclaredRouter: c.Router, Store: store, Segments: &c}
	parent, err := lan.delegatedSegmentParent(t.Context(), "vxlan100")
	if err != nil || parent.Token != native.link.Token {
		t.Fatalf("current native tunnel rejected as delegated parent: %+v %v", parent, err)
	}
	// The default production constructor must retain the actual state source too.
	if gate, err := lan.segmentParentOwner().gateReader().gateState(c.Router.Spec.Resources[2]); err != nil || gate != vxlanGateEnabled {
		t.Fatalf("child lost state reader: %s %v", gate, err)
	}
	lan.Segments = nil
	if gate, err := lan.segmentParentOwner().gateReader().gateState(c.Router.Spec.Resources[2]); err != nil || gate != vxlanGateEnabled {
		t.Fatalf("default child lost state reader: %s %v", gate, err)
	}
	if _, err := store.Set("ha.role", "backup", "demotion"); err != nil {
		t.Fatal(err)
	}
	if _, err := static.staticSegmentParent(t.Context(), "segment", current); !errors.Is(err, errStaticParentNotReady) {
		t.Fatalf("disabled tunnel admitted static child: %v", err)
	}
}

func TestVXLANPlansSeparateKindsAndRejectSharedNativeName(t *testing.T) {
	c, _, _ := tunnelOwnerFixture(t)
	segment := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANSegment"}, Metadata: api.ObjectMeta{Name: "segment"}, Spec: api.VXLANSegmentSpec{IfName: "vx200", VNI: 200, LocalAddress: "192.0.2.1", Remotes: []string{"192.0.2.2"}, UnderlayInterface: "uplink"}}
	c.Router.Spec.Resources = append(c.Router.Spec.Resources, segment)
	plans, err := segmentPlans(c.Router, platform.CurrentOS())
	if err != nil || len(plans) != 2 {
		t.Fatalf("kind identities collided: %+v %v", plans, err)
	}
	spec, _ := segment.VXLANSegmentSpec()
	spec.IfName = "vxlan100"
	segment.Spec = spec
	c.Router.Spec.Resources[3] = segment
	if _, err := segmentPlans(c.Router, platform.CurrentOS()); err == nil {
		t.Fatal("two resources acquired the same native name")
	}
}

func TestVXLANTunnelReadOnlyParentRequiresFreshGateWithoutStartedAt(t *testing.T) {
	c, _, store := tunnelOwnerFixture(t)
	c.StartedAt = time.Time{}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	value, err := store.ReadVariable("ha.role")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if err := store.SaveObjectStatus("state.routerd.net/v1alpha1", "StateVariable", "ha.role", map[string]any{"value": map[string]any{"status": value.Status, "value": value.Value, "reason": value.Reason, "since": old, "updatedAt": old}}); err != nil {
		t.Fatal(err)
	}
	link, err := c.ObserveOwnedInterface(t.Context(), api.NetAPIVersion+"/VXLANTunnel/segment")
	if err == nil && link.Exists {
		t.Fatal("read-only observer authorized parent from hour-old gate")
	}
}

func TestVXLANTunnelOmittedDFUsesNativeFreeBSDDefault(t *testing.T) {
	c, _, _ := tunnelOwnerFixture(t)
	plans, err := segmentPlans(c.Router, platform.OSFreeBSD)
	if err != nil {
		t.Fatal(err)
	}
	plan, ok := plans[api.NetAPIVersion+"/VXLANTunnel/segment"]
	if !ok || plan.OuterDF != "" || !plan.NoLearning {
		t.Fatalf("omitted DF became unsupported explicit FreeBSD request: %+v", plan)
	}
	linux, err := segmentPlans(c.Router, platform.OSLinux)
	if err != nil || linux[api.NetAPIVersion+"/VXLANTunnel/segment"].OuterDF != "inherit" {
		t.Fatalf("Linux implicit inherit lost: %+v %v", linux, err)
	}
}
