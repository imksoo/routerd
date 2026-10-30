// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func pdFixtureClock(t *testing.T, store routerstate.PDLeaseReader) func() time.Time {
	t.Helper()
	lease, found, err := store.ReadPDLease("wan-pd")
	if err != nil || !found || lease.LastReplyAt.IsZero() {
		t.Fatalf("missing PD fixture epoch: %+v %v", lease, err)
	}
	return func() time.Time { return lease.LastReplyAt }
}

func finiteLANFixture(now time.Time) (*api.Router, mapStore, LANAddressController) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-address"}, Spec: api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", SubnetID: "1", AddressSuffix: "::1"}},
	}}}
	store := mapStore{api.NetAPIVersion + "/Interface/lan": {"phase": "Up", "ifname": "lan0"}}
	_ = store.SavePDLease("wan-pd", routerstate.PDLease{CurrentPrefix: "2001:db8:1200::/60", LastPrefix: "2001:db8:1200::/60", LastReplyAt: now, PreferredSeconds: 90, ValidSeconds: 120, Phase: daemonapi.ResourcePhaseBound})
	controller := LANAddressController{Router: router, DeclaredRouter: router, Store: store, OperatingSystem: platform.OSLinux, Now: func() time.Time { return now }, ObserveAddress: lanProbeFixture(func(context.Context, string, string) bool { return false }, nil, nil)}
	return router, store, controller
}

func TestLANCurrentAddressUsesFiniteRemainingPDLifetimeAndRenews(t *testing.T) {
	acquired := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	now := acquired.Add(10*time.Second + 250*time.Millisecond)
	_, store, controller := finiteLANFixture(acquired)
	controller.Now = func() time.Time { return now }
	var commands []string
	controller.Command = func(_ context.Context, name string, args ...string) error {
		commands = append(commands, name+" "+strings.Join(args, " "))
		return nil
	}
	installLANIOFixture(&controller)
	if err := controller.reconcile(context.Background(), "wan-pd"); err != nil {
		t.Fatal(err)
	}
	want := "ip -6 addr add 2001:db8:1200:1::1/64 dev lan0 preferred_lft 79 valid_lft 109"
	if !reflect.DeepEqual(commands, []string{want}) {
		t.Fatalf("finite PD address command: %+v", commands)
	}
	// The address is unchanged, but a Reply renews its native deadline.
	lease, _, _ := store.ReadPDLease("wan-pd")
	lease.LastReplyAt = acquired.Add(30 * time.Second)
	lease.PreferredSeconds, lease.ValidSeconds = 180, 240
	_ = store.SavePDLease("wan-pd", lease)
	now = acquired.Add(35 * time.Second)
	commands = nil
	if err := controller.reconcile(context.Background(), "wan-pd"); err != nil {
		t.Fatal(err)
	}
	want = "ip -6 addr replace 2001:db8:1200:1::1/64 dev lan0 preferred_lft 175 valid_lft 235"
	if !reflect.DeepEqual(commands, []string{want}) {
		t.Fatalf("same-address renewal did not refresh exact deadline: %+v", commands)
	}
	commands = nil
	now = now.Add(time.Second)
	if err := controller.reconcile(context.Background(), "wan-pd"); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 0 {
		t.Fatalf("unchanged authority reset native deadline: %+v", commands)
	}
}

func TestLANCurrentLifetimeRechecksAfterObservationBeforeWrite(t *testing.T) {
	acquired := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	now := acquired
	_, _, controller := finiteLANFixture(acquired)
	controller.Now = func() time.Time { return now }
	controller.ObserveAddress = lanProbeFixture(func(context.Context, string, string) bool { now = acquired.Add(121 * time.Second); return false }, nil, nil)
	writes := 0
	controller.Command = func(context.Context, string, ...string) error { writes++; return nil }
	if err := controller.reconcile(context.Background(), "wan-pd"); err == nil {
		t.Fatal("expired observation authorized native apply")
	}
	if writes != 0 {
		t.Fatal("address written after source expiry during observation")
	}
}

func TestLANLifetimeCommandsPreserveInfiniteAndFreeBSDDeadlines(t *testing.T) {
	lease := routerstate.PDLease{CurrentPrefix: "2001:db8::/60", LastReplyAt: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), PreferredSeconds: infinitePDLifetime, ValidSeconds: infinitePDLifetime}
	pref, valid, err := currentPDAddressLifetimes(lease, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), false)
	if err != nil || pref != infinitePDLifetime || valid != infinitePDLifetime {
		t.Fatalf("infinite protocol lifetime: %d %d %v", pref, valid, err)
	}
	name, args := ipv6DelegatedAddressApplyCommand(platform.OSFreeBSD, "lan0", "2001:db8::1/64", 79, 109)
	if name != "ifconfig" || strings.Join(args, " ") != "lan0 inet6 2001:db8::1 prefixlen 64 pltime 79 vltime 109 alias" {
		t.Fatalf("FreeBSD finite native command: %s %+v", name, args)
	}
	_, args = ipv6DelegatedAddressApplyCommand(platform.OSLinux, "lan0", "2001:db8::1/64", pref, valid)
	if !strings.HasSuffix(strings.Join(args, " "), "preferred_lft forever valid_lft forever") {
		t.Fatalf("infinite native command: %+v", args)
	}
}

type pdPendingSaveFault struct {
	mapStore
	failure error
}

func (s pdPendingSaveFault) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if kind == "IPv6DelegatedAddress" && status["reason"] == "AddressApplying" {
		return s.failure
	}
	return s.mapStore.SaveObjectStatus(version, kind, name, status)
}

func TestLANCurrentLifetimePendingSaveFailurePreventsNativeWrite(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	_, store, controller := finiteLANFixture(now)
	failure := errors.New("pending owner save failed")
	controller.Store = pdPendingSaveFault{mapStore: store, failure: failure}
	writes := 0
	controller.Command = func(context.Context, string, ...string) error { writes++; return nil }
	if err := controller.reconcile(context.Background(), "wan-pd"); !errors.Is(err, failure) {
		t.Fatalf("pending save failure lost: %v", err)
	}
	if writes != 0 {
		t.Fatal("native write preceded pending ownership save")
	}
}

func TestLANCurrentNativeObservationFailureAndForeignAddressPreventWrites(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(map[bool]string{false: "read-error", true: "foreign-same-host"}[foreign], func(t *testing.T) {
			now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
			_, _, controller := finiteLANFixture(now)
			controller.ObserveAddress = nil
			failure := errors.New("native address read failed")
			controller.ObserveAddress = lanProbeFixture(nil, func(context.Context, string) ([]string, error) {
				if foreign {
					return []string{"2001:db8:1200:1::1/128"}, nil
				}
				return nil, failure
			}, nil)
			writes := 0
			controller.Command = func(context.Context, string, ...string) error { writes++; return nil }
			err := controller.reconcile(context.Background(), "wan-pd")
			if err == nil {
				t.Fatal("unknown or foreign current address authorized effect")
			}
			if !foreign && !errors.Is(err, failure) {
				t.Fatalf("native read failure lost: %v", err)
			}
			if writes != 0 {
				t.Fatal("unknown native observation reached mutation")
			}
		})
	}
}

func TestDelegatedAddressObservationSeparatesTentativeFromAbsent(t *testing.T) {
	output := []byte("7: lan0 inet6 2001:db8::1/64 scope global tentative valid_lft 100sec preferred_lft 80sec\n")
	active, err := parseDelegatedAddressObservation(output, "2001:db8::1/64", platform.OSLinux, false)
	if err != nil || !active.present || active.usable {
		t.Fatalf("tentative address became absent/usable: %+v %v", active, err)
	}
	staged, err := parseDelegatedAddressObservation(output, "2001:db8::1/64", platform.OSLinux, true)
	if err != nil || !staged.present || !staged.usable {
		t.Fatalf("standby staging lost exact tentative address: %+v %v", staged, err)
	}
	if _, err := parseDelegatedAddressObservation([]byte("inet6 invalid/64"), "2001:db8::1/64", platform.OSLinux, false); err == nil {
		t.Fatal("malformed native read became absent")
	}
}

func TestLANCurrentExpiryDuringApplyFencesAmbiguousPresentAddress(t *testing.T) {
	acquired := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	now := acquired
	_, store, c := finiteLANFixture(acquired)
	c.Now = func() time.Time { return now }
	present := false
	writes := 0
	c.ObserveAddress = lanProbeFixture(func(context.Context, string, string) bool { return present }, nil, nil)
	c.Command = func(_ context.Context, _ string, args ...string) error {
		writes++
		if args[2] == "del" {
			t.Fatal("expired finite tuple inferred native ownership")
		}
		present = true
		now = acquired.Add(121 * time.Second)
		return nil
	}
	if err := c.reconcile(t.Context(), "wan-pd"); err == nil {
		t.Fatal("expired present native address acknowledged")
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	receipt, err := delegatedReceipt(status)
	if err != nil || !receipt.has("2001:db8:1200:1::1/64") || status["reason"] != "NativeOwnershipUnknown" || writes != 1 {
		t.Fatalf("ambiguous expired receipt lost fence: %+v %v", status, err)
	}
	present = false // operator/native expiry confirms absence; no delete is guessed.
	if err := c.reconcile(t.Context(), "wan-pd"); err != nil {
		t.Fatal(err)
	}
}

func TestLAN128RenumberRetainsExactOldOwnerUntilWithdrawalSucceeds(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	router, store, controller := finiteLANFixture(now)
	spec, _ := router.Spec.Resources[2].IPv6DelegatedAddressSpec()
	spec.PrefixLength = 128
	router.Spec.Resources[2].Spec = spec
	old := "2001:db8:1100:1::1/128"
	_ = store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address", map[string]any{"phase": "Applied", "address": old, "interface": "lan", "ifname": "lan0", "prefixSource": "wan-pd", "dryRun": false})
	recordLANReceiptForTest(t, store, "lan-address", "lan0", "wan-pd", old)
	controller.ObserveAddress = lanProbeFixture(func(context.Context, string, string) bool { return true }, nil, nil)
	var commands []string
	controller.Command = func(_ context.Context, name string, args ...string) error {
		command := name + " " + strings.Join(args, " ")
		commands = append(commands, command)
		if strings.Contains(command, " addr del ") {
			return errors.New("old address removal failed")
		}
		return nil
	}
	installLANIOFixture(&controller, old)
	if err := controller.reconcile(context.Background(), "wan-pd"); err == nil {
		t.Fatal("failed old source removal reported success")
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	receipt, err := delegatedReceipt(status)
	if err != nil || !receipt.has(old) || status["reason"] != "AddressWithdrawing" {
		t.Fatalf("renumber lost exact old owner: %#v", status)
	}
	for _, command := range commands {
		if strings.Contains(command, " addr del ") && command != "ip -6 addr del "+old+" dev lan0" {
			t.Fatalf("renumber guessed another native address: %s", command)
		}
	}
}

func TestPreviousDelegatedLifetimeFloorsAndBoundsFarFuture(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	if got := remainingIPv6ValidLifetime(now, now.Add(1900*time.Millisecond)); got != 1 {
		t.Fatalf("fractional native lifetime extended source: %d", got)
	}
	if got := remainingIPv6ValidLifetime(now, now.Add(999*time.Millisecond)); got != 0 {
		t.Fatalf("subsecond native lifetime extended source: %d", got)
	}
	if got := remainingIPv6ValidLifetime(now, time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)); got != int64(infinitePDLifetime) {
		t.Fatalf("infinite previous deadline overflowed: %d", got)
	}
	if got := remainingIPv6ValidLifetime(now, time.Date(9998, 1, 1, 0, 0, 0, 0, time.UTC)); got != int64(infinitePDLifetime)-1 {
		t.Fatalf("far finite deadline was negative/infinite: %d", got)
	}
}

func TestLAN128RenumberRejectsConflictingNativeOwners(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	router, store, controller := finiteLANFixture(now)
	spec, _ := router.Spec.Resources[2].IPv6DelegatedAddressSpec()
	spec.PrefixLength = 128
	router.Spec.Resources[2].Spec = spec
	old := "2001:db8:1100:1::1/128"
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "independent"}, Spec: api.VirtualAddressSpec{Family: "ipv6", Interface: "lan", Address: old}})
	_ = store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address", map[string]any{"phase": "Applied", "address": old, "interface": "lan", "ifname": "lan0", "prefixSource": "wan-pd", "dryRun": false})
	recordLANReceiptForTest(t, store, "lan-address", "lan0", "wan-pd", old)
	recordLANReceiptForTest(t, store, "independent", "lan0", "other-pd", old)
	var commands []string
	controller.Command = func(_ context.Context, name string, args ...string) error {
		commands = append(commands, name+" "+strings.Join(args, " "))
		return nil
	}
	if err := controller.reconcile(context.Background(), "wan-pd"); err == nil {
		t.Fatal("conflicting current ownership was hidden")
	}
	for _, command := range commands {
		if strings.Contains(command, " addr del ") {
			t.Fatalf("independent address deleted during old-owner cleanup: %s", command)
		}
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	receipt, err := delegatedReceipt(status)
	if err != nil || !receipt.has(old) {
		t.Fatal("conflicting owner erased recoverable old receipt")
	}
}

func TestLANSinglePrefixRevocationWithdrawsPriorInfiniteReceipt(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	_, store, controller := finiteLANFixture(now)
	lease, _, _ := store.ReadPDLease("wan-pd")
	lease.PreviousPrefixes = []routerstate.PDPreviousPrefix{{Prefix: "2001:db8:1100::/60", ExpiresAt: time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)}}
	_ = store.SavePDLease("wan-pd", lease)
	current, old := "2001:db8:1200:1::1/64", "2001:db8:1100:1::1/64"
	present := map[string]bool{}
	controller.ObserveAddress = lanProbeFixture(func(_ context.Context, _ string, address string) bool { return present[address] }, nil, nil)
	var commands []string
	controller.Command = func(_ context.Context, name string, args ...string) error {
		commands = append(commands, name+" "+strings.Join(args, " "))
		if len(args) > 3 && args[1] == "addr" {
			if args[2] == "replace" || args[2] == "add" {
				present[args[3]] = true
			}
			if args[2] == "del" {
				delete(present, args[3])
			}
		}
		return nil
	}
	if err := controller.reconcile(context.Background(), "wan-pd"); err != nil {
		t.Fatal(err)
	}
	if !present[old] || len(previousDelegatedAddressesFromStatus(store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address"))) != 1 {
		t.Fatal("canonical infinite previous prefix was not actually applied and recorded")
	}
	// The protocol client retains omitted live prefixes in its complete
	// association. Removing only this entry models its explicit zero lifetime;
	// the different current prefix remains Bound and usable.
	lease.PreviousPrefixes = nil
	_ = store.SavePDLease("wan-pd", lease)
	commands = nil
	if err := controller.reconcile(context.Background(), "wan-pd"); err != nil {
		t.Fatal(err)
	}
	want := "ip -6 addr del " + old + " dev lan0"
	if !reflect.DeepEqual(commands, []string{want}) {
		t.Fatalf("revoked infinite receipt minted continued authority: %+v", commands)
	}
	if present[old] || !present[current] {
		t.Fatalf("per-prefix revoke changed unrelated current authority: %+v", present)
	}
	if previous := previousDelegatedAddressesFromStatus(store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")); len(previous) != 0 {
		t.Fatalf("revoked receipt remained live: %+v", previous)
	}
	if observed, _, _ := store.ReadPDLease("wan-pd"); observed.Phase != daemonapi.ResourcePhaseBound || observed.CurrentPrefix != lease.CurrentPrefix {
		t.Fatal("regression accidentally exercised whole-lease Release")
	}
}
