// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/platform"
	ra "github.com/imksoo/routerd/pkg/routeradvertisement"
	state "github.com/imksoo/routerd/pkg/state"
	"github.com/mdlayher/ndp"
	"golang.org/x/net/ipv6"
)

type raCapturedSocket struct {
	packets     [][]byte
	failure     error
	control     *ipv6.ControlMessage
	destination netip.Addr
}

func (c *raCapturedSocket) ReadRaw([]byte) (int, *ipv6.ControlMessage, netip.Addr, error) {
	time.Sleep(5 * time.Millisecond)
	return 0, nil, netip.Addr{}, os.ErrDeadlineExceeded
}
func (c *raCapturedSocket) WriteTo(message ndp.Message, control *ipv6.ControlMessage, destination netip.Addr) error {
	if c.failure != nil {
		return c.failure
	}
	source, _ := netip.AddrFromSlice(control.Src)
	packet, err := ndp.MarshalMessageChecksum(message, source, destination)
	if err != nil {
		return err
	}
	c.packets = append(c.packets, packet)
	c.control = control
	c.destination = destination
	return nil
}
func (c *raCapturedSocket) SetReadDeadline(time.Time) error  { return nil }
func (c *raCapturedSocket) SetWriteDeadline(time.Time) error { return nil }
func (c *raCapturedSocket) Close() error                     { return nil }

type raWirePrefix struct{ preferred, valid uint32 }

func raWirePrefixes(t *testing.T, packet []byte) map[string]raWirePrefix {
	t.Helper()
	out := map[string]raWirePrefix{}
	if len(packet) < 16 || packet[0] != 134 || packet[1] != 0 {
		t.Fatalf("not an RA: %x", packet)
	}
	for offset := 16; offset < len(packet); {
		if len(packet)-offset < 2 || packet[offset+1] == 0 {
			t.Fatalf("invalid RA option %x", packet[offset:])
		}
		size := int(packet[offset+1]) * 8
		if size > len(packet)-offset {
			t.Fatalf("truncated RA option")
		}
		if packet[offset] == 3 {
			if size != 32 {
				t.Fatal("wrong PIO length")
			}
			var address [16]byte
			copy(address[:], packet[offset+16:offset+32])
			prefix := netip.PrefixFrom(netip.AddrFrom16(address), int(packet[offset+2]))
			out[prefix.String()] = raWirePrefix{preferred: binary.BigEndian.Uint32(packet[offset+8 : offset+12]), valid: binary.BigEndian.Uint32(packet[offset+4 : offset+8])}
		}
		offset += size
	}
	return out
}

func raServiceFixture(t *testing.T) (*RouterAdvertisementService, *state.SQLiteStore, *api.Router, ra.Endpoint, *raCapturedSocket, time.Time) {
	t.Helper()
	store, err := state.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	acquired := time.Now().UTC().Add(-10*time.Second - time.Second/4)
	lease, _, err := store.ConfigurePDLease("wan-pd", state.PDLeaseConfig{Client: api.IPv6PDClientRouterd, UplinkIfname: "wan0", ClientDUID: "01", IAID: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease.CurrentPrefix = "2001:db8:1200::/60"
	lease.LastReplyAt = acquired
	lease.SnapshotUpdatedAt = acquired
	lease.PreferredSeconds = 90
	lease.ValidSeconds = 120
	lease.Phase = daemonapi.ResourcePhaseBound
	lease.PreviousPrefixes = []state.PDPreviousPrefix{{Prefix: "2001:db8:1100::/60", ExpiresAt: acquired.Add(300 * time.Second)}}
	if err := store.SavePDLease("wan-pd", lease); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address", map[string]any{"phase": "Applied", "address": "2001:db8:1200:1::1/64"}); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ra-test-lan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-address"}, Spec: api.IPv6DelegatedAddressSpec{Interface: "lan", PrefixDelegation: "wan-pd", SubnetID: "1", AddressSuffix: "::1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "lan", PrefixFrom: api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan-address", Field: "address"}, MFlag: true, OFlag: false}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "independent-server"}, Spec: api.DHCPv6ServerSpec{Interface: "lan", Mode: "stateless"}},
	}}}
	service := NewRouterAdvertisementService(t.Context(), false)
	service.scope = func(context.Context) (string, error) { return "lan-test-kernel", nil }
	recordLANReceiptForTest(t, store, "lan-address", "ra-test-lan", "wan-pd", "2001:db8:1200:1::1/64", "2001:db8:1100:1::1/64")
	nativeStatus, _ := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	nativeReceipt, _ := delegatedReceipt(nativeStatus)
	for i := range nativeReceipt.Addresses {
		nativeReceipt.Addresses[i].PreferredUntil = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	}
	if err := (LANAddressController{Store: store}).saveDelegatedReceipt("lan-address", nativeStatus, nativeReceipt); err != nil {
		t.Fatal(err)
	}
	service.clock = func() time.Time { return acquired.Add(10*time.Second + time.Second/4) }
	prepared, err := prepareRAPlans(router)
	if err != nil {
		t.Fatal(err)
	}
	service.plans, err = selectRAPlans(prepared, router)
	if err != nil {
		t.Fatal(err)
	}
	service.store, service.declared = store, router
	socket := &raCapturedSocket{}
	endpoint := ra.Endpoint{Conn: socket, Source: netip.MustParseAddr("fe80::1"), Index: 7, MTU: 1500, HardwareAddress: net.HardwareAddr{2, 0, 0, 0, 0, 1}}
	return service, store, router, endpoint, socket, acquired
}

func TestRAActualWireSelectsCanonicalPrefixesFiniteLifetimesAndExactFlags(t *testing.T) {
	service, store, _, endpoint, socket, _ := raServiceFixture(t)
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	packet := socket.packets[0]
	if packet[5]&0xc0 != 0x80 || binary.BigEndian.Uint16(packet[6:8]) != 1800 {
		t.Fatalf("M/O/router lifetime not independent: %x", packet[:16])
	}
	prefixes := raWirePrefixes(t, packet)
	if len(prefixes) != 2 || prefixes["2001:db8:1200:1::/64"] != (raWirePrefix{79, 109}) || prefixes["2001:db8:1100:1::/64"] != (raWirePrefix{0, 289}) {
		t.Fatalf("actual selected PIOs=%+v", prefixes)
	}
	if socket.control.HopLimit != 255 || socket.control.IfIndex != 7 || socket.destination != netip.MustParseAddr("ff02::1") {
		t.Fatalf("wrong native transmission envelope: %+v %v", socket.control, socket.destination)
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", "lan-ra")
	if err != nil || status["phase"] != "Applied" || status["renderer"] != "routerd" {
		t.Fatalf("wire result not published: %+v %v", status, err)
	}
}

func TestRAReleaseWithdrawsExactReceiptAndRetainsFailedWithdrawal(t *testing.T) {
	service, store, _, endpoint, socket, _ := raServiceFixture(t)
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	lease, _, err := store.ReadPDLease("wan-pd")
	if err != nil {
		t.Fatal(err)
	}
	lease.CurrentPrefix = ""
	lease.PreviousPrefixes = nil
	lease.Phase = daemonapi.ResourcePhaseReleased
	if err := store.SavePDLease("wan-pd", lease); err != nil {
		t.Fatal(err)
	}
	socket.failure = errors.New("write failed")
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); !errors.Is(err, socket.failure) {
		t.Fatalf("withdraw error=%v", err)
	}
	journal, err := readRAJournal(store)
	if err != nil || len(journal["ra-test-lan"].Prefixes) != 2 {
		t.Fatalf("failed withdrawal lost receipt: %+v %v", journal, err)
	}
	socket.failure = nil
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	packet := socket.packets[len(socket.packets)-1]
	prefixes := raWirePrefixes(t, packet)
	if len(prefixes) != 2 {
		t.Fatalf("withdrawal prefixes=%+v", prefixes)
	}
	for _, prefix := range prefixes {
		if prefix.valid != 0 || prefix.preferred != 0 {
			t.Fatalf("released authority revived: %+v", prefixes)
		}
	}
	journal, err = readRAJournal(store)
	if err != nil || len(journal) != 0 {
		t.Fatalf("successful withdrawal retained authority receipt: %+v %v", journal, err)
	}
}

func TestRAStaticPrefixUsesIndependentPIOAndRouterLifetimes(t *testing.T) {
	service, _, router, endpoint, socket, _ := raServiceFixture(t)
	resource := router.Spec.Resources[3]
	resource.Spec = api.IPv6RouterAdvertisementSpec{Interface: "lan", Prefix: "2001:db8:2000::123/64", PreferredLifetime: "3600", ValidLifetime: "7200", RouterLifetime: "0", OFlag: true}
	router.Spec.Resources[3] = resource
	prepared, err := prepareRAPlans(router)
	if err != nil {
		t.Fatal(err)
	}
	service.plans, err = selectRAPlans(prepared, router)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	packet := socket.packets[0]
	prefixes := raWirePrefixes(t, packet)
	if len(prefixes) != 1 || prefixes["2001:db8:2000::/64"] != (raWirePrefix{3600, 7200}) || binary.BigEndian.Uint16(packet[6:8]) != 0 || packet[5]&0xc0 != 0x40 {
		t.Fatalf("static PIO/router flags=%+v header=%x", prefixes, packet[:16])
	}
}

func TestRAOlderValidCanonicalPrefixSurvivesCurrentExpiry(t *testing.T) {
	service, store, _, endpoint, socket, now := raServiceFixture(t)
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	lease, _, _ := store.ReadPDLease("wan-pd")
	epoch := now.Add(-10 * time.Second)
	lease.LastReplyAt = epoch
	lease.PreferredSeconds = 4
	lease.ValidSeconds = 5
	lease.PreviousPrefixes = []state.PDPreviousPrefix{{Prefix: "2001:db8:1100::/60", ExpiresAt: epoch.Add(100 * time.Second)}}
	if err := store.SavePDLease("wan-pd", lease); err != nil {
		t.Fatal(err)
	}
	service.clock = func() time.Time { return epoch.Add(11 * time.Second) }
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	pios := raWirePrefixes(t, socket.packets[len(socket.packets)-1])
	if pios["2001:db8:1100:1::/64"] != (raWirePrefix{preferred: 0, valid: 89}) || pios["2001:db8:1200:1::/64"] != (raWirePrefix{}) {
		t.Fatalf("current expiry withdrew/extended live previous authority: %+v", pios)
	}
	current, _, _ := store.ReadPDLease("wan-pd")
	if !current.LastReplyAt.Equal(epoch) || !current.SnapshotUpdatedAt.Equal(lease.SnapshotUpdatedAt) {
		t.Fatalf("RA fabricated new lease evidence: %+v", current)
	}
}

func TestRAHeldNativeRenewalKeepsConfirmedDeadline(t *testing.T) {
	service, store, _, endpoint, socket, _ := raServiceFixture(t)
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	status, _ := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	receipt, _ := delegatedReceipt(status)
	receipt.claim("2001:db8:1200:1::1/64", time.Unix(0, 0).UTC(), time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC))
	status["phase"], status["reason"] = "Pending", "AddressApplying"
	if err := (LANAddressController{Store: store}).saveDelegatedReceipt("lan-address", status, receipt); err != nil {
		t.Fatal(err)
	}
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	if len(socket.packets) != 2 {
		t.Fatal("held native lifetime refresh lost confirmed authority")
	}
	if pios := raWirePrefixes(t, socket.packets[1]); pios["2001:db8:1200:1::/64"] != (raWirePrefix{79, 109}) {
		t.Fatalf("pending update extended confirmed authority: %+v", pios)
	}
	receipt.confirm("2001:db8:1200:1::1/64")
	if err := (LANAddressController{Store: store}).saveDelegatedReceipt("lan-address", status, receipt); err != nil {
		t.Fatal(err)
	}
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	if len(socket.packets) != 3 {
		t.Fatal("confirmed native owner did not resume")
	}
}

func TestRAReplacementInterfaceDoesNotWithdrawOldLinkPrefixes(t *testing.T) {
	service, store, _, endpoint, socket, _ := raServiceFixture(t)
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	delete(service.plans, "ra-test-lan")
	endpoint.Index = 8
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, true); err != nil {
		t.Fatal(err)
	}
	if len(socket.packets) != 1 {
		t.Fatal("old-link journal authorized withdrawal on replacement")
	}
	journal, err := readRAJournal(store)
	if err != nil || len(journal) != 0 {
		t.Fatalf("old link receipt not retired: %+v %v", journal, err)
	}
}

type raSaveNoticeStore struct {
	*state.SQLiteStore
	saving chan struct{}
	once   sync.Once
}

func (s *raSaveNoticeStore) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if kind == "Router" && name == raJournalName {
		s.once.Do(func() { close(s.saving) })
	}
	return s.SQLiteStore.SaveObjectStatus(version, kind, name, status)
}

func TestRAStopHonorsDeadlineDuringActualSQLiteWriteLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := state.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	locker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Close()
	locker.SetMaxOpenConns(1)
	if _, err := locker.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			locker.Exec("ROLLBACK")
		}
	}()
	notice := &raSaveNoticeStore{SQLiteStore: store, saving: make(chan struct{})}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "static"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "ra-test-lan", Prefix: "2001:db8::/64"}}}}}
	socket := &raCapturedSocket{}
	service := NewRouterAdvertisementService(t.Context(), false)
	service.scope = func(context.Context) (string, error) { return "lan-test-kernel", nil }
	service.open = func(string) (ra.Endpoint, error) {
		return ra.Endpoint{Conn: socket, Source: netip.MustParseAddr("fe80::1"), Index: 7, MTU: 1500}, nil
	}
	done := make(chan error, 1)
	go func() { done <- service.Reconcile(t.Context(), router, router, notice) }()
	select {
	case <-notice.saving:
	case <-time.After(2 * time.Second):
		t.Fatal("actual pending SQLite write not reached")
	}
	stopCtx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := service.Stop(stopCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unjoined disk owner falsely acknowledged: %v", err)
	}
	if _, err := locker.Exec("ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	locked = false
	joinCtx, joinCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer joinCancel()
	if err := service.Stop(joinCtx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reconcile not joined")
	}
}

type raBlockedSocket struct {
	raCapturedSocket
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *raBlockedSocket) WriteTo(message ndp.Message, control *ipv6.ControlMessage, destination netip.Addr) error {
	s.once.Do(func() { close(s.entered); <-s.release })
	return s.raCapturedSocket.WriteTo(message, control, destination)
}
func TestRAStopDoesNotAcknowledgeBlockedNativeWrite(t *testing.T) {
	service, store, router, endpoint, _, _ := raServiceFixture(t)
	socket := &raBlockedSocket{entered: make(chan struct{}), release: make(chan struct{})}
	endpoint.Conn = socket
	service.open = func(string) (ra.Endpoint, error) { return endpoint, nil }
	done := make(chan error, 1)
	go func() { done <- service.Reconcile(t.Context(), router, router, store) }()
	select {
	case <-socket.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("native write not reached")
	}
	stopCtx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := service.Stop(stopCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked native owner falsely joined: %v", err)
	}
	close(socket.release)
	joinCtx, joinCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer joinCancel()
	if err := service.Stop(joinCtx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reconcile not joined")
	}
	if len(socket.packets) != 2 || binary.BigEndian.Uint16(socket.packets[1][6:8]) != 0 {
		t.Fatalf("final router withdrawal absent: packets=%d", len(socket.packets))
	}
}

func raOptionLifetimes(t *testing.T, packet []byte, kind byte) []uint32 {
	t.Helper()
	var values []uint32
	for offset := 16; offset < len(packet); {
		if offset+2 > len(packet) || packet[offset+1] == 0 {
			t.Fatal("bad option")
		}
		size := int(packet[offset+1]) * 8
		if offset+size > len(packet) {
			t.Fatal("truncated option")
		}
		if packet[offset] == kind {
			values = append(values, binary.BigEndian.Uint32(packet[offset+4:offset+8]))
		}
		offset += size
	}
	return values
}
func TestRADNSOptionsWithdrawExactPriorValues(t *testing.T) {
	service, store, _, endpoint, socket, _ := raServiceFixture(t)
	plan := service.plans["ra-test-lan"]
	plan.Resources[0].Spec.RDNSS = []string{"2001:db8::53"}
	plan.Resources[0].Spec.DNSSL = []string{"lan.example"}
	service.plans["ra-test-lan"] = plan
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	if got := raOptionLifetimes(t, socket.packets[0], 25); len(got) != 1 || got[0] != 109 {
		t.Fatalf("RDNSS exceeded finite prefix lifetime: %+v", got)
	}
	if got := raOptionLifetimes(t, socket.packets[0], 31); len(got) != 1 || got[0] != 109 {
		t.Fatalf("DNSSL exceeded finite prefix lifetime: %+v", got)
	}
	plan.Resources[0].Spec.RDNSS = nil
	plan.Resources[0].Spec.DNSSL = nil
	service.plans["ra-test-lan"] = plan
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []byte{25, 31} {
		if got := raOptionLifetimes(t, socket.packets[1], kind); len(got) != 1 || got[0] != 0 {
			t.Fatalf("removed DNS option %d not withdrawn: %+v", kind, got)
		}
	}
	journal, err := readRAJournal(store)
	if err != nil || len(journal["ra-test-lan"].DNSServers) != 0 || len(journal["ra-test-lan"].Domains) != 0 {
		t.Fatalf("old DNS withdrawal not committed: %+v %v", journal, err)
	}
}

func TestRAActualWireRetainsPathMTUAndPreference(t *testing.T) {
	for _, mtu := range []int{1454, 0} {
		t.Run(fmt.Sprint(mtu), func(t *testing.T) {
			service, _, router, endpoint, socket, _ := raServiceFixture(t)
			resource := router.Spec.Resources[3]
			resource.Spec = api.IPv6RouterAdvertisementSpec{Interface: "lan", Prefix: "2001:db8::/64", MTU: 1500, PRFPreference: "high"}
			router.Spec.Resources[3] = resource
			router.Spec.Resources = append(router.Spec.Resources,
				api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite"}, Spec: api.DSLiteTunnelSpec{TunnelName: "dslite0", MTU: mtu}},
				api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan-zone"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
				api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan-zone"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"ds-lite"}}})
			prepared, err := prepareRAPlans(router)
			if err != nil {
				t.Fatal(err)
			}
			service.plans, err = selectRAPlans(prepared, router)
			if err != nil {
				t.Fatal(err)
			}
			if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
				t.Fatal(err)
			}
			values := raOptionLifetimes(t, socket.packets[0], 5)
			want := uint32(mtu)
			if want == 0 {
				want = 1460
			}
			if len(values) != 1 || values[0] != want || socket.packets[0][5]&0x18 != 0x08 {
				t.Fatalf("MTU/preference lost: values=%v flags=%x", values, socket.packets[0][5])
			}
		})
	}
}

func TestRAZeroPIOLifetimeDoesNotOverrideExplicitRouterRole(t *testing.T) {
	service, store, router, endpoint, socket, _ := raServiceFixture(t)
	resource := router.Spec.Resources[3]
	resource.Spec = api.IPv6RouterAdvertisementSpec{Interface: "lan", Prefix: "2001:db8::/64", PreferredLifetime: "0", ValidLifetime: "0", RouterLifetime: "600"}
	router.Spec.Resources[3] = resource
	prepared, err := prepareRAPlans(router)
	if err != nil {
		t.Fatal(err)
	}
	service.plans, err = selectRAPlans(prepared, router)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(socket.packets[0][6:8]) != 600 {
		t.Fatal("PIO withdrawal incorrectly withdrew independently configured router role")
	}
	journal, err := readRAJournal(store)
	if err != nil || !journal["ra-test-lan"].Router {
		t.Fatalf("router-only wire effect lacks withdrawal receipt: %+v %v", journal, err)
	}
	delete(service.plans, "ra-test-lan")
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, true); err != nil {
		t.Fatal(err)
	}
	if len(socket.packets) != 2 || binary.BigEndian.Uint16(socket.packets[1][6:8]) != 0 {
		t.Fatal("router-only role not withdrawn")
	}
	journal, err = readRAJournal(store)
	if err != nil || len(journal) != 0 {
		t.Fatalf("router-only receipt retained after confirmed withdrawal: %+v %v", journal, err)
	}
}

func TestRAImplicitServerModesHaveIndependentFlagsAndNativeDeadlines(t *testing.T) {
	bin := t.TempDir()
	output := `[{"ifname":"ra-test-lan","addr_info":[{"local":"2001:db8:3000::1","prefixlen":64,"flags":["mngtmpaddr"],"preferred_life_time":40,"valid_life_time":90},{"local":"2001:db8:3000::2","prefixlen":64,"preferred_life_time":30,"valid_life_time":60},{"local":"2001:db8:4000::1","prefixlen":64,"temporary":true,"preferred_life_time":80,"valid_life_time":100}]}]`
	if platform.CurrentOS() == platform.OSFreeBSD {
		output = "ra-test-lan: flags=\n\tinet6 2001:db8:3000::1 prefixlen 64 pltime 40 vltime 90\n\tinet6 2001:db8:3000::2 prefixlen 64 pltime 30 vltime 60\n\tinet6 2001:db8:4000::1 prefixlen 64 pltime 80 vltime 100 temporary\n"
	}
	for _, name := range []string{"ip", "ifconfig"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\ncat <<'NATIVE'\n"+output+"\nNATIVE\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tc := range []struct {
		mode    string
		m, o, a bool
	}{{"stateful", true, true, false}, {"both", true, true, true}, {"stateless", false, true, true}, {"ra-only", false, false, true}} {
		t.Run(tc.mode, func(t *testing.T) {
			service, _, router, endpoint, socket, _ := raServiceFixture(t)
			server := router.Spec.Resources[4]
			server.Spec = api.DHCPv6ServerSpec{Interface: "lan", Mode: tc.mode, DefaultRoute: true, DNSSource: "none"}
			router.Spec.Resources = []api.Resource{router.Spec.Resources[0], server}
			prepared, err := prepareRAPlans(router)
			if err != nil {
				t.Fatal(err)
			}
			service.plans, err = selectRAPlans(prepared, router)
			if err != nil {
				t.Fatal(err)
			}
			if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
				t.Fatal(err)
			}
			packet := socket.packets[0]
			flags := packet[5]
			if (flags&0x80 != 0) != tc.m || (flags&0x40 != 0) != tc.o {
				t.Fatalf("%s flags=%x", tc.mode, flags)
			}
			prefixes := raWirePrefixes(t, packet)
			if len(prefixes) != 1 || prefixes["2001:db8:3000::/64"] != (raWirePrefix{40, 90}) {
				t.Fatalf("native prefix/lifetime selection=%+v", prefixes)
			}
			autonomous := false
			for offset := 16; offset < len(packet); offset += int(packet[offset+1]) * 8 {
				if packet[offset] == 3 {
					autonomous = packet[offset+3]&0x40 != 0
				}
			}
			if autonomous != tc.a {
				t.Fatalf("%s autonomous=%t", tc.mode, autonomous)
			}
		})
	}
}

// A newly installed delegated address is tentative while the kernel performs
// DAD. Configuration apply must remain pending, not fail merely because the RA
// controller runs before that asynchronous check finishes.
func TestRADelegatedDADWaitDefersAdvertisementAndResumes(t *testing.T) {
	service, store, _, endpoint, socket, _ := raServiceFixture(t)
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	if err != nil {
		t.Fatal(err)
	}
	status["phase"], status["reason"] = "Pending", "AddressNotReady"
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address", status); err != nil {
		t.Fatal(err)
	}
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatalf("ordinary DAD wait failed configuration apply: %v", err)
	}
	packet := socket.packets[len(socket.packets)-1]
	if len(raWirePrefixes(t, packet)) != 0 || binary.BigEndian.Uint16(packet[6:8]) != 0 {
		t.Fatalf("tentative address advertised a prefix/default route: %x", packet)
	}
	raStatus, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", "lan-ra")
	if err != nil || raStatus["phase"] != "Pending" {
		t.Fatalf("DAD wait should publish Pending: %+v, %v", raStatus, err)
	}
	status["phase"] = "Applied"
	delete(status, "reason")
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address", status); err != nil {
		t.Fatal(err)
	}
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	packet = socket.packets[len(socket.packets)-1]
	if raWirePrefixes(t, packet)["2001:db8:1200:1::/64"].valid == 0 || binary.BigEndian.Uint16(packet[6:8]) == 0 {
		t.Fatal("DAD completion did not resume prefix/default-router advertisement")
	}
}

func TestRADelegatedFailedDADAndUnknownOwnershipRemainErrors(t *testing.T) {
	for _, reason := range []string{"AddressDADFailed", "NativeOwnershipUnknown"} {
		t.Run(reason, func(t *testing.T) {
			service, store, _, endpoint, socket, _ := raServiceFixture(t)
			status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
			if err != nil {
				t.Fatal(err)
			}
			status["phase"], status["reason"] = "Pending", reason
			if err := store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address", status); err != nil {
				t.Fatal(err)
			}
			if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err == nil || len(socket.packets) != 0 {
				t.Fatalf("unsafe advertisement for %s: packets=%d err=%v", reason, len(socket.packets), err)
			}
		})
	}
}
