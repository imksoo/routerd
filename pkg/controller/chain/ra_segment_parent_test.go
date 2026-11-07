// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	ra "github.com/imksoo/routerd/pkg/routeradvertisement"
	state "github.com/imksoo/routerd/pkg/state"
)

type raPendingParentFault struct {
	*state.SQLiteStore
	afterPending func()
}

func (s *raPendingParentFault) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if err := s.SQLiteStore.SaveObjectStatus(version, kind, name, status); err != nil {
		return err
	}
	if kind == "Router" && name == raJournalName && s.afterPending != nil {
		if receipts, ok := status["interfaces"].([]raReceipt); ok && len(receipts) > 0 {
			callback := s.afterPending
			s.afterPending = nil
			callback()
		}
	}
	return nil
}

func TestRARequiresActualSegmentParentAndRecordedWithdrawal(t *testing.T) {
	parent, native, store, _ := segmentOwnerFixture(t)
	router := parent.Router
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "segment-ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "segment", Prefix: "2001:db8:7::/64"}})
	service := NewRouterAdvertisementService(t.Context(), false)
	service.segments = &parent
	service.scope = func(context.Context) (string, error) { return native.scope, nil }
	now := time.Now().UTC()
	service.clock = func() time.Time { return now }
	prepared, err := prepareRAPlans(router)
	if err != nil {
		t.Fatal(err)
	}
	service.plans, err = selectRAPlans(prepared, router)
	if err != nil {
		t.Fatal(err)
	}
	service.prepared = prepared
	service.store = store
	service.declared = router
	socket := &raCapturedSocket{}
	endpoint := ra.Endpoint{Conn: socket, Source: netip.MustParseAddr("fe80::1"), Index: 8, MTU: 1500}
	native.link.Exists = true
	native.link.Index = 8
	native.link.Config.IfName = "vxlan100"
	native.link.Token = "foreign"
	native.link.Kind = "vxlan"
	if err := service.transmit(t.Context(), "vxlan100", endpoint, false); err == nil || len(socket.packets) != 0 {
		t.Fatalf("foreign parent emitted RA: %v", err)
	}
	native.link.Exists = false
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := service.transmit(t.Context(), "vxlan100", endpoint, false); err != nil {
		t.Fatal(err)
	}
	journal, err := readRAJournal(store)
	if err != nil {
		t.Fatal(err)
	}
	token := journal["vxlan100"].ParentToken
	if token == "" || token != native.link.Token || len(socket.packets) != 1 {
		t.Fatalf("actual parent proof not recorded: %+v", journal)
	}
	native.link.Token = "foreign-replacement"
	if err := service.transmit(t.Context(), "vxlan100", endpoint, false); err == nil || len(socket.packets) != 1 {
		t.Fatalf("replacement parent emitted RA: %v packets=%d", err, len(socket.packets))
	}
	if status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", "segment-ra"); err != nil || status["phase"] != "Error" {
		t.Fatalf("native parent error not published: %+v %v", status, err)
	}
	service.plans = map[string]raInterfacePlan{}
	service.declared = &api.Router{}
	if err := service.transmit(t.Context(), "vxlan100", endpoint, true); err == nil || len(socket.packets) != 1 {
		t.Fatalf("withdrawal on foreign parent: %v packets=%d", err, len(socket.packets))
	}
	if journal, err := readRAJournal(store); err != nil || journal["vxlan100"].ParentToken != token {
		t.Fatalf("unknown withdrawal lost receipt: %+v %v", journal, err)
	}
	native.link.Token = token
	if err := service.transmit(t.Context(), "vxlan100", endpoint, true); err != nil {
		t.Fatal(err)
	}
	prefixes := raWirePrefixes(t, socket.packets[len(socket.packets)-1])
	if len(socket.packets) != 2 || len(prefixes) != 1 || prefixes["2001:db8:7::/64"] != (raWirePrefix{}) {
		t.Fatalf("recorded parent withdrawal not exact: %+v packets=%d", prefixes, len(socket.packets))
	}
	if journal, err := readRAJournal(store); err != nil || len(journal) != 0 {
		t.Fatalf("confirmed withdrawal not committed: %+v %v", journal, err)
	}
}

func TestRARechecksSegmentParentAfterPendingAdvertisement(t *testing.T) {
	parent, native, store, _ := segmentOwnerFixture(t)
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	router := parent.Router
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "segment-ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "segment", Prefix: "2001:db8:7::/64"}})
	service := NewRouterAdvertisementService(t.Context(), false)
	service.segments = &parent
	service.scope = func(context.Context) (string, error) { return native.scope, nil }
	prepared, err := prepareRAPlans(router)
	if err != nil {
		t.Fatal(err)
	}
	service.plans, err = selectRAPlans(prepared, router)
	if err != nil {
		t.Fatal(err)
	}
	service.prepared = prepared
	service.declared = router
	token := native.link.Token
	fault := &raPendingParentFault{SQLiteStore: store, afterPending: func() { native.link.Token = "foreign-replacement" }}
	service.store = fault
	socket := &raCapturedSocket{}
	endpoint := ra.Endpoint{Conn: socket, Source: netip.MustParseAddr("fe80::1"), Index: 8, MTU: 1500}
	if err := service.transmit(t.Context(), "vxlan100", endpoint, false); err == nil || len(socket.packets) != 0 {
		t.Fatalf("changed parent admitted actual wire send: %v packets=%d", err, len(socket.packets))
	}
	if journal, err := readRAJournal(store); err != nil || journal["vxlan100"].ParentToken != token {
		t.Fatalf("pending send receipt lost: %+v %v", journal, err)
	}
}

func TestRADelegatedLANReceiptMustMatchCurrentSegmentCookie(t *testing.T) {
	store, router, lan, _ := sqliteNativeLANFixture(t)
	parent, native, _, _ := segmentOwnerFixture(t)
	parent.Store = store
	router.Spec.Resources[0] = parent.Router.Spec.Resources[2]
	spec, _ := router.Spec.Resources[2].IPv6DelegatedAddressSpec()
	spec.Interface = "segment"
	router.Spec.Resources[2].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, parent.Router.Spec.Resources[0], parent.Router.Spec.Resources[1])
	parent.Router = router
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "segment", map[string]any{"phase": "Up"}); err != nil {
		t.Fatal(err)
	}
	lan.Segments = &parent
	lan.Command = nil
	installLANIOFixture(&lan)
	observe := lan.ObserveAddress
	lan.ObserveAddress = func(ctx context.Context, ifname, address string, staged bool) (delegatedAddressObservation, error) {
		out, err := observe(ctx, ifname, address, staged)
		out.ifIndex, out.scope = native.link.Index, native.scope
		return out, err
	}
	if err := lan.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "segment-ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "segment", PrefixFrom: api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan-address", Field: "address"}}})
	service := NewRouterAdvertisementService(t.Context(), false)
	service.segments = &parent
	service.store = store
	service.declared = router
	service.scope = func(context.Context) (string, error) { return native.scope, nil }
	service.clock = lan.Now
	prepared, err := prepareRAPlans(router)
	if err != nil {
		t.Fatal(err)
	}
	service.prepared = prepared
	service.plans, err = selectRAPlans(prepared, router)
	if err != nil {
		t.Fatal(err)
	}
	socket := &raCapturedSocket{}
	endpoint := ra.Endpoint{Conn: socket, Source: netip.MustParseAddr("fe80::1"), Index: native.link.Index, MTU: 1500}
	if err := service.transmit(t.Context(), "vxlan100", endpoint, false); err != nil {
		t.Fatal(err)
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := delegatedReceipt(status)
	if err != nil {
		t.Fatal(err)
	}
	receipt.ParentToken = "another-parent"
	if err := lan.saveDelegatedReceipt("lan-address", status, receipt); err != nil {
		t.Fatal(err)
	}
	if err := service.transmit(t.Context(), "vxlan100", endpoint, false); err == nil || len(socket.packets) != 1 {
		t.Fatalf("LAN receipt from another parent emitted RA: %v packets=%d", err, len(socket.packets))
	}
}
