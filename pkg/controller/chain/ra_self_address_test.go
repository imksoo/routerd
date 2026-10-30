// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	state "github.com/imksoo/routerd/pkg/state"
)

func configureRASelfPolicy(t *testing.T, service *RouterAdvertisementService, router *api.Router, candidates ...api.SelfAddressPolicyCandidate) {
	t.Helper()
	server := router.Spec.Resources[4]
	server.Spec = api.DHCPv6ServerSpec{DelegatedAddress: "lan-address", Mode: "ra-only", DefaultRoute: true, SelfAddressPolicy: "dns-self"}
	extra := append([]api.Resource(nil), router.Spec.Resources[5:]...)
	router.Spec.Resources = append([]api.Resource{router.Spec.Resources[0], router.Spec.Resources[1], router.Spec.Resources[2], server},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "SelfAddressPolicy"}, Metadata: api.ObjectMeta{Name: "dns-self"}, Spec: api.SelfAddressPolicySpec{AddressFamily: "ipv6", Candidates: candidates}})
	router.Spec.Resources = append(router.Spec.Resources, extra...)
	refreshRASelfPlans(t, service, router)
}

func refreshRASelfPlans(t *testing.T, service *RouterAdvertisementService, router *api.Router) {
	t.Helper()
	plans, err := prepareRAPlans(router)
	if err != nil {
		t.Fatal(err)
	}
	service.plans, err = selectRAPlans(plans, router)
	if err != nil {
		t.Fatal(err)
	}
}

func raWireDNS(t *testing.T, packet []byte) map[string]uint32 {
	t.Helper()
	servers := map[string]uint32{}
	for offset := 16; offset < len(packet); {
		if offset+2 > len(packet) || packet[offset+1] == 0 {
			t.Fatal("invalid RA option")
		}
		size := int(packet[offset+1]) * 8
		if offset+size > len(packet) {
			t.Fatal("truncated RA option")
		}
		if packet[offset] == 25 {
			for start := offset + 8; start+16 <= offset+size; start += 16 {
				address, _ := netip.AddrFromSlice(packet[start : start+16])
				servers[address.String()] = binary.BigEndian.Uint32(packet[offset+4 : offset+8])
			}
		}
		offset += size
	}
	return servers
}

func TestRASelfPolicyCustomStaticAddressAppearsOnActualOwnerWire(t *testing.T) {
	service, store, router, endpoint, socket, _ := raServiceFixture(t)
	configureRASelfPolicy(t, service, router, api.SelfAddressPolicyCandidate{Source: "static", Address: "fe80::53"})
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	if got := raWireDNS(t, socket.packets[0]); len(got) != 1 || got["fe80::53"] != 109 {
		t.Fatalf("configured self DNS was not advertised: %v", got)
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "DHCPv6Server", "independent-server")
	if err != nil || status["phase"] != "Applied" {
		t.Fatalf("self DNS result not published: %+v %v", status, err)
	}
	servers, ok := status["rdnss"].([]any)
	if !ok || len(servers) != 1 || servers[0] != "fe80::53" {
		t.Fatalf("self DNS status differs: %+v", status)
	}
}

func TestRASelfPolicyDelegatedDeadlineAndFallbackWithdrawOldDNS(t *testing.T) {
	service, store, router, endpoint, socket, _ := raServiceFixture(t)
	addRASelfDelegatedAddress(t, service, store, router, 17*time.Second)
	configureRASelfPolicy(t, service, router,
		api.SelfAddressPolicyCandidate{Source: "delegatedAddress", DelegatedAddress: "dns-address"},
		api.SelfAddressPolicyCandidate{Source: "static", Address: "2001:db8::53"})
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	if got := raWireDNS(t, socket.packets[0]); len(got) != 1 || got["2001:db8:1200:1::53"] != 17 {
		t.Fatalf("custom delegated DNS did not retain own shorter deadline: %v", got)
	}
	if got := raWirePrefixes(t, socket.packets[0]); got["2001:db8:1200:1::/64"].valid != 109 {
		t.Fatalf("DNS deadline incorrectly shortened unrelated PIO: %v", got)
	}
	failure := errors.New("self DNS source unavailable")
	service.store = raSelfSourceErrorStore{SQLiteStore: store, failure: failure}
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); !errors.Is(err, failure) || len(socket.packets) != 1 {
		t.Fatalf("failed required self read selected a fallback/effect: %v", err)
	}
	service.store = store
	later := service.clock().Add(18 * time.Second)
	service.clock = func() time.Time { return later }
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	if got := raWireDNS(t, socket.packets[1]); got["2001:db8:1200:1::53"] != 0 || got["2001:db8::53"] != 91 {
		t.Fatalf("expired DNS did not withdraw and select ordered fallback: %v", got)
	}
	journal, err := readRAJournal(store)
	if err != nil || len(journal["ra-test-lan"].DNSServers) != 1 || journal["ra-test-lan"].DNSServers[0] != "2001:db8::53" {
		t.Fatalf("DNS withdrawal receipt differs: %+v %v", journal, err)
	}
}

type raSelfChangeAfterJournalStore struct {
	*state.SQLiteStore
	change func()
}

func (s *raSelfChangeAfterJournalStore) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if err := s.SQLiteStore.SaveObjectStatus(version, kind, name, status); err != nil {
		return err
	}
	if kind == "Router" && name == raJournalName && s.change != nil {
		change := s.change
		s.change = nil
		change()
	}
	return nil
}

func TestRASelfPolicyRechecksNativeReceiptAfterPendingJournalWrite(t *testing.T) {
	service, store, router, endpoint, socket, _ := raServiceFixture(t)
	addRASelfDelegatedAddress(t, service, store, router, time.Minute)
	configureRASelfPolicy(t, service, router,
		api.SelfAddressPolicyCandidate{Source: "delegatedAddress", DelegatedAddress: "dns-address"},
		api.SelfAddressPolicyCandidate{Source: "static", Address: "2001:db8::99"})
	changed := false
	service.store = &raSelfChangeAfterJournalStore{SQLiteStore: store, change: func() {
		changed = true
		status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "dns-address")
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := delegatedReceipt(status)
		if err != nil {
			t.Fatal(err)
		}
		receipt.Addresses[0].ValidUntil = service.clock()
		if err := (LANAddressController{Store: store}).saveDelegatedReceipt("dns-address", status, receipt); err != nil {
			t.Fatal(err)
		}
	}}
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	if got := raWireDNS(t, socket.packets[0]); !changed || len(got) != 1 || got["2001:db8::99"] != 109 {
		t.Fatalf("stale DNS survived the actual pending write boundary: %v, changed=%t", got, changed)
	}
}

func TestRASelfPolicyUnavailableCandidateWithdrawsDNSAndReportsPending(t *testing.T) {
	service, store, router, endpoint, socket, _ := raServiceFixture(t)
	addRASelfDelegatedAddress(t, service, store, router, time.Second)
	configureRASelfPolicy(t, service, router, api.SelfAddressPolicyCandidate{Source: "delegatedAddress", DelegatedAddress: "dns-address"})
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	later := service.clock().Add(2 * time.Second)
	service.clock = func() time.Time { return later }
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	if got := raWireDNS(t, socket.packets[1]); len(got) != 1 || got["2001:db8:1200:1::53"] != 0 {
		t.Fatalf("expired selected address was not withdrawn: %v", got)
	}
	if got := raWirePrefixes(t, socket.packets[1]); got["2001:db8:1200:1::/64"].valid != 107 {
		t.Fatalf("missing DNS suppressed a separately usable prefix: %v", got)
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "DHCPv6Server", "independent-server")
	if err != nil || status["phase"] != "Pending" || status["reason"] != "SelfDNSNotReady" {
		t.Fatalf("unavailable configured DNS was reported ready: %+v %v", status, err)
	}
}

func addRASelfDelegatedAddress(t *testing.T, service *RouterAdvertisementService, store *state.SQLiteStore, router *api.Router, valid time.Duration) {
	t.Helper()
	spec, err := router.Spec.Resources[2].IPv6DelegatedAddressSpec()
	if err != nil {
		t.Fatal(err)
	}
	spec.AddressSuffix = "::53"
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "dns-address"}, Spec: spec})
	recordLANReceiptForTest(t, store, "dns-address", "ra-test-lan", "wan-pd", "2001:db8:1200:1::53/64")
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "dns-address")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := delegatedReceipt(status)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Addresses[0].ValidUntil = service.clock().Add(valid)
	if err := (LANAddressController{Store: store}).saveDelegatedReceipt("dns-address", status, receipt); err != nil {
		t.Fatal(err)
	}
}

func TestRASelfNativeAddressObservationKeepsHostAddressAndLifetime(t *testing.T) {
	input := []byte(`[{"ifname":"lan0","addr_info":[{"local":"2001:db8::53","prefixlen":128,"preferred_life_time":15,"valid_life_time":23},{"local":"2001:db8::54","prefixlen":64,"preferred_life_time":"forever","valid_life_time":"forever"},{"local":"2001:db8::55","prefixlen":64,"temporary":true,"preferred_life_time":15,"valid_life_time":30}]}]`)
	addresses, err := parseNativeRAAddresses(input, "lan0", platform.OSLinux)
	if err != nil || len(addresses) != 2 || addresses[0].Address.String() != "2001:db8::53/128" || addresses[0].Valid != 23 || addresses[1].Valid != math.MaxUint32 {
		t.Fatalf("native address/lifetime lost: %+v %v", addresses, err)
	}
	prefixes := nativeRAPrefixValues(addresses)
	if len(prefixes) != 1 || prefixes[0].Prefix.String() != "2001:db8::/64" {
		t.Fatalf("host /128 became a PIO: %+v", prefixes)
	}
	bsd, err := parseNativeRAAddresses([]byte("em0: flags=\n\tinet6 2001:db8::53 prefixlen 128 pltime infty vltime infty\n"), "em0", platform.OSFreeBSD)
	if err != nil || len(bsd) != 1 || bsd[0].Valid != math.MaxUint32 {
		t.Fatalf("explicit infinite native lifetime lost: %+v %v", bsd, err)
	}
	if _, err := parseNativeRAAddresses([]byte("em0: flags=\n\tinet6 2001:db8::53 prefixlen 128\n"), "em0", platform.OSFreeBSD); err == nil {
		t.Fatal("missing FreeBSD lifetime was guessed to be infinite")
	}
}

func TestRASelfPolicyInterfaceAddressUsesLiveCommandAndConfiguredOrder(t *testing.T) {
	service, _, router, endpoint, socket, _ := raServiceFixture(t)
	dir := t.TempDir()
	response := filepath.Join(dir, "addresses.json")
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(response, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(`[{"ifname":"ra-test-lan","addr_info":[{"local":"2001:db8::53","prefixlen":128,"preferred_life_time":15,"valid_life_time":23}]}]`)
	if err := os.WriteFile(filepath.Join(dir, "ip"), []byte("#!/bin/sh\ncat '"+response+"'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	configureRASelfPolicy(t, service, router,
		api.SelfAddressPolicyCandidate{Source: "interfaceAddress", Interface: "lan", MatchSuffix: "::53"},
		api.SelfAddressPolicyCandidate{Source: "static", Address: "2001:db8::99"})
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	if got := raWireDNS(t, socket.packets[0]); len(got) != 1 || got["2001:db8::53"] != 23 {
		t.Fatalf("live interface self DNS lifetime lost: %v", got)
	}
	write(`[{"ifname":"ra-test-lan","addr_info":[]}]`)
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	if got := raWireDNS(t, socket.packets[1]); got["2001:db8::53"] != 0 || got["2001:db8::99"] != 109 {
		t.Fatalf("native disappearance did not select fallback and withdraw old DNS: %v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "ip"), []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err == nil || len(socket.packets) != 2 {
		t.Fatalf("failed native read selected a fallback/effect: %v", err)
	}
}

type raSelfSourceErrorStore struct {
	*state.SQLiteStore
	failure error
}

func (s raSelfSourceErrorStore) ReadObjectStatus(version, kind, name string) (map[string]any, error) {
	if kind == "IPv6DelegatedAddress" && name == "dns-address" {
		return nil, s.failure
	}
	return s.SQLiteStore.ReadObjectStatus(version, kind, name)
}

func TestRASelfPolicyCrossInterfaceRequiresCurrentNativeReceiptIdentity(t *testing.T) {
	service, store, router, _, _, _ := raServiceFixture(t)
	addRASelfDelegatedAddress(t, service, store, router, time.Minute)
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "dns-address")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := delegatedReceipt(status)
	if err != nil {
		t.Fatal(err)
	}
	receipt.IfName, receipt.IfIndex = "other-lan", 8
	if err := (LANAddressController{Store: store}).saveDelegatedReceipt("dns-address", status, receipt); err != nil {
		t.Fatal(err)
	}
	spec, err := router.Spec.Resources[len(router.Spec.Resources)-1].IPv6DelegatedAddressSpec()
	if err != nil {
		t.Fatal(err)
	}
	spec.Interface = "other-lan"
	candidate := raSelfDNSCandidate{Spec: api.SelfAddressPolicyCandidate{Source: "delegatedAddress", DelegatedAddress: "dns-address"}, IfName: "other-lan", Delegated: &spec}
	plan := raInterfacePlan{Interface: "ra-test-lan", NativeIndex: 7, NativeScope: receipt.Scope}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ip"), []byte("#!/bin/sh\nprintf '%s\\n' '[{\"ifname\":\"other-lan\",\"addr_info\":[{\"local\":\"2001:db8:1200:1::53\",\"prefixlen\":64,\"preferred_life_time\":15,\"valid_life_time\":23}]}]'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tc := range []struct {
		name     string
		observed delegatedAddressObservation
		wantErr  bool
		wantDNS  bool
	}{
		{"matching", delegatedAddressObservation{ifIndex: 8, scope: receipt.Scope, present: true, usable: true}, false, true},
		{"replaced-index", delegatedAddressObservation{ifIndex: 9, scope: receipt.Scope, present: true, usable: true}, true, false},
		{"old-kernel", delegatedAddressObservation{ifIndex: 8, scope: "other-kernel", present: true, usable: true}, true, false},
		{"missing", delegatedAddressObservation{ifIndex: 8, scope: receipt.Scope}, false, false},
		{"not-usable", delegatedAddressObservation{ifIndex: 8, scope: receipt.Scope, present: true}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			owner := LANAddressController{Router: router, Store: store, ObserveAddress: func(_ context.Context, ifname, address string, staged bool) (delegatedAddressObservation, error) {
				called = true
				if ifname != receipt.IfName || address != "2001:db8:1200:1::53/64" || staged {
					t.Fatalf("wrong native selection: %s %s %t", ifname, address, staged)
				}
				return tc.observed, nil
			}}
			selected, err := currentDelegatedRASelfDNS(t.Context(), owner, plan, candidate, service.clock())
			if !called || (err != nil) != tc.wantErr || selected.Address.IsValid() != tc.wantDNS {
				t.Fatalf("selected=%+v err=%v native-read=%t", selected, err, called)
			}
			if tc.wantDNS && selected.Valid != 23 {
				t.Fatalf("native finite deadline was lost: %+v", selected)
			}
		})
	}
}

func TestRASelfPolicyDoesNotAdvertiseUnresolvedDelegatedDAD(t *testing.T) {
	service, store, router, endpoint, socket, _ := raServiceFixture(t)
	addRASelfDelegatedAddress(t, service, store, router, time.Minute)
	configureRASelfPolicy(t, service, router, api.SelfAddressPolicyCandidate{Source: "delegatedAddress", DelegatedAddress: "dns-address"})
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "dns-address")
	if err != nil {
		t.Fatal(err)
	}
	status["phase"], status["reason"] = "Pending", "AddressDADFailed"
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "dns-address", status); err != nil {
		t.Fatal(err)
	}
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err == nil || len(socket.packets) != 0 {
		t.Fatalf("unresolved DAD advertised DNS: %v packets=%d", err, len(socket.packets))
	}
}
