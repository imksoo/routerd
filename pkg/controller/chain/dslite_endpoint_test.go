// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type dsliteEndpointFixtureStore struct {
	*routerstate.SQLiteStore
	failStage, failComplete, failPDRead bool
	failTunnelStatus                    bool
}

func (s *dsliteEndpointFixtureStore) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if kind == "DSLiteTunnel" && s.failTunnelStatus {
		return errors.New("tunnel status write rejected")
	}
	if name == dsliteEndpointStatusName {
		var entries []dsliteEndpointReceipt
		if decodeAppliedDataplaneStatus(status["endpoints"], &entries) {
			for _, entry := range entries {
				if (entry.Pending && s.failStage) || (!entry.Pending && s.failComplete) {
					return errors.New("endpoint receipt write rejected")
				}
			}
		}
	}
	return s.SQLiteStore.SaveObjectStatus(version, kind, name, status)
}

func (s *dsliteEndpointFixtureStore) ReadPDLease(name string) (routerstate.PDLease, bool, error) {
	if s.failPDRead {
		return routerstate.PDLease{}, false, errors.New("PD observation unavailable")
	}
	return s.SQLiteStore.ReadPDLease(name)
}

type dsliteEndpointFixture struct {
	t                          *testing.T
	store                      *dsliteEndpointFixtureStore
	path                       string
	now                        time.Time
	present                    string
	calls, writes              []string
	failRead, failDelete       bool
	failApply, afterEffectFail bool
	afterApplyRead             func()
}

func (f *dsliteEndpointFixture) command(_ context.Context, name string, args ...string) ([]byte, error) {
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	if name == "ip" && len(args) > 4 && args[0] == "-j" && args[2] == "addr" {
		if f.failRead {
			return nil, errors.New("native source observation unavailable")
		}
		if f.present == "" {
			return []byte(`[{"addr_info":[{"family":"inet6","local":"2001:db8:ffff::1","prefixlen":64}]}]`), nil
		}
		address, err := netip.ParsePrefix(f.present)
		if err != nil {
			f.t.Fatal(err)
		}
		if f.afterApplyRead != nil {
			action := f.afterApplyRead
			f.afterApplyRead = nil
			action()
		}
		return []byte(fmt.Sprintf(`[{"addr_info":[{"family":"inet6","local":%q,"prefixlen":%d},{"family":"inet6","local":"2001:db8:ffff::1","prefixlen":64}]}]`, address.Addr().String(), address.Bits())), nil
	}
	if name == "ip" && len(args) > 4 && args[0] == "-6" && args[1] == "addr" {
		f.writes = append(f.writes, line)
		if args[2] == "del" {
			if args[3] != f.present {
				f.t.Fatalf("attempted to delete foreign source %q; owned native tuple=%q", args[3], f.present)
			}
			if f.failDelete {
				return nil, errors.New("source delete rejected")
			}
			f.present = ""
			return nil, nil
		}
		var entries []dsliteEndpointReceipt
		status, err := f.store.ReadObjectStatus(api.RouterAPIVersion, "Router", dsliteEndpointStatusName)
		if err != nil || !decodeAppliedDataplaneStatus(status["endpoints"], &entries) || len(entries) != 1 || entries[0].Address != args[3] || !entries[0].Pending {
			f.t.Fatalf("native write preceded durable exact pending receipt: %+v %v", status, err)
		}
		if f.failApply && !f.afterEffectFail {
			return nil, errors.New("source apply rejected")
		}
		f.present = args[3]
		if f.failApply {
			return nil, errors.New("source apply result lost")
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected endpoint command %s", line)
}

func newDSLiteEndpointFixture(t *testing.T, shared bool) (DSLiteTunnelController, *dsliteEndpointFixture, api.DSLiteTunnelSpec) {
	t.Helper()
	requireLinuxRuntimeFixture(t)
	f := &dsliteEndpointFixture{t: t, path: filepath.Join(t.TempDir(), "state.db"), now: time.Now().UTC().Truncate(time.Second)}
	f.reopen()
	lease := routerstate.PDLease{Client: "routerd", UplinkIfname: "wan0", CurrentPrefix: "2001:db8:1200::/60", LastReplyAt: f.now.Add(-10 * time.Second), PreferredSeconds: 3600, ValidSeconds: 7200, Phase: daemonapi.ResourcePhaseBound}
	if err := f.store.SavePDLease("wan-pd", lease); err != nil {
		t.Fatal(err)
	}
	suffix, prefixLength := "::100", 64
	if shared {
		suffix, prefixLength = "::1", 128
	}
	spec := api.DSLiteTunnelSpec{Interface: "wan", TunnelName: "ds-lite-ra", LocalAddressSource: "delegatedAddress", LocalDelegatedAddress: "lan-source", LocalAddressSuffix: suffix, AFTRIPv6: "2001:db8::ffff"}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "wan0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-source"}, Spec: api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "wan", SubnetID: "1", AddressSuffix: "::1", PrefixLength: prefixLength}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "dslite"}, Spec: spec},
	}}}
	controller := DSLiteTunnelController{Router: router, Store: f.store, Now: func() time.Time { return f.now }, CommandOutput: f.command}
	return controller, f, spec
}

func (f *dsliteEndpointFixture) reopen() {
	f.t.Helper()
	store, err := routerstate.OpenSQLite(f.path)
	if err != nil {
		f.t.Fatal(err)
	}
	f.store = &dsliteEndpointFixtureStore{SQLiteStore: store}
	f.t.Cleanup(func() { _ = store.Close() })
}

func (f *dsliteEndpointFixture) changeLease(change func(*routerstate.PDLease)) {
	f.t.Helper()
	lease, _, err := f.store.SQLiteStore.ReadPDLease("wan-pd")
	if err != nil {
		f.t.Fatal(err)
	}
	change(&lease)
	if err := f.store.SavePDLease("wan-pd", lease); err != nil {
		f.t.Fatal(err)
	}
}

func TestDSLiteCustomDelegatedSourceUsesFiniteLifetimeAndWithdrawsBeforeRenumber(t *testing.T) {
	c, f, _ := newDSLiteEndpointFixture(t, false)
	installFakeDSLiteIP(t, "ds-lite-ra: ip/ipv6 remote 2001:db8::ffff local 2001:db8:1200:1::100 dev wan0 encaplimit none", false)
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatalf("custom source/tunnel apply: %v", err)
	}
	if len(f.writes) != 1 || f.writes[0] != "ip -6 addr replace 2001:db8:1200:1::100/128 dev wan0 preferred_lft 0 valid_lft 7190" {
		t.Fatalf("custom source did not use remaining upstream lifetime: %v", f.writes)
	}
	f.changeLease(func(lease *routerstate.PDLease) { lease.CurrentPrefix = "2001:db8:1240::/60" })
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatalf("renumbered source/tunnel apply: %v", err)
	}
	if len(f.writes) != 3 || !strings.Contains(f.writes[1], "addr del 2001:db8:1200:1::100/128") || !strings.Contains(f.writes[2], "addr replace 2001:db8:1240:1::100/128") {
		t.Fatalf("renumber did not withdraw exact old source before installing new: %v", f.writes)
	}
	if status := f.store.ObjectStatus(api.NetAPIVersion, "DSLiteTunnel", "dslite"); status["phase"] != "Up" || status["localIPv6"] != "2001:db8:1240:1::100" {
		t.Fatalf("renumbered source was not published after native effects: %+v", status)
	}
}

func TestDSLiteSourceFailureRetainsPendingAndRevocationCleanupAcrossRestart(t *testing.T) {
	c, f, spec := newDSLiteEndpointFixture(t, false)
	f.failApply, f.afterEffectFail = true, true
	if _, err := c.ensureDelegatedEndpoint(t.Context(), "dslite", spec, "wan0", "2001:db8:1200:1::100"); err == nil {
		t.Fatal("uncertain native apply reported success")
	}
	receipts, err := c.endpointReceipts()
	if err != nil || len(receipts) != 1 || !receipts[0].Pending {
		t.Fatalf("uncertain effect lost pending receipt: %+v %v", receipts, err)
	}
	f.changeLease(func(lease *routerstate.PDLease) { lease.CurrentPrefix, lease.Phase = "", daemonapi.ResourcePhaseIdle })
	f.failDelete = true
	if err := c.cleanupDelegatedEndpoints(t.Context()); err == nil {
		t.Fatal("failed withdrawal reported success")
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	c.Store = f.store
	f.failDelete = false
	if err := c.cleanupDelegatedEndpoints(t.Context()); err != nil {
		t.Fatal(err)
	}
	receipts, err = c.endpointReceipts()
	if err != nil || len(receipts) != 0 || f.present != "" {
		t.Fatalf("restart did not finish pending cleanup: %+v %v", receipts, err)
	}
}

func TestDSLiteSourceJournalFailureStopsEffectsAndCompletionFailureRecovers(t *testing.T) {
	c, f, spec := newDSLiteEndpointFixture(t, false)
	f.store.failStage = true
	if _, err := c.ensureDelegatedEndpoint(t.Context(), "dslite", spec, "wan0", "2001:db8:1200:1::100"); err == nil || len(f.writes) != 0 {
		t.Fatal("native write occurred after pending persistence failure")
	}
	f.store.failStage, f.store.failComplete = false, true
	if _, err := c.ensureDelegatedEndpoint(t.Context(), "dslite", spec, "wan0", "2001:db8:1200:1::100"); err == nil {
		t.Fatal("completion persistence failure was hidden")
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	c.Store = f.store
	if pending, err := c.ensureDelegatedEndpoint(t.Context(), "dslite", spec, "wan0", "2001:db8:1200:1::100"); pending != "" || err != nil {
		t.Fatalf("pending completion recovery: %s %v", pending, err)
	}
	receipts, err := c.endpointReceipts()
	if err != nil || len(receipts) != 1 || receipts[0].Pending {
		t.Fatalf("successful retry not recorded: %+v %v", receipts, err)
	}
}

func TestDSLiteSharedLANSourceNeverDuplicatesItsNativeAddress(t *testing.T) {
	c, f, spec := newDSLiteEndpointFixture(t, true)
	source, _, err := c.delegatedEndpointSource(spec)
	if err != nil {
		t.Fatal(err)
	}
	f.present = source.Address + "/128"
	if err := f.store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-source", map[string]any{"phase": "Applied", "address": f.present, "interface": "wan", "ifname": "wan0", "prefixSource": "wan-pd", "dryRun": false, "validUntil": pdLifetimeDeadline(source.Lease.LastReplyAt, source.Lease.ValidSeconds)}); err != nil {
		t.Fatal(err)
	}
	if pending, err := c.ensureDelegatedEndpoint(t.Context(), "dslite", spec, "wan0", source.Address); pending != "" || err != nil {
		t.Fatalf("shared LAN source not usable: %s %v", pending, err)
	}
	receipts, err := c.endpointReceipts()
	if err != nil || len(receipts) != 0 || len(f.writes) != 0 {
		t.Fatalf("DS-Lite acquired duplicate LAN ownership: %+v writes=%v %v", receipts, f.writes, err)
	}
	status := f.store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-source")
	status["ifname"] = "old0"
	if err := f.store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-source", status); err != nil {
		t.Fatal(err)
	}
	if ready, err := c.sharedEndpointReady(source); err != nil || ready {
		t.Fatalf("old native interface receipt acknowledged the new source: ready=%t err=%v", ready, err)
	}
	f.changeLease(func(lease *routerstate.PDLease) { lease.CurrentPrefix, lease.Phase = "", daemonapi.ResourcePhaseIdle })
	if err := c.cleanupDelegatedEndpoints(t.Context()); err != nil || len(f.writes) != 0 || f.present == "" {
		t.Fatal("DS-Lite attempted to withdraw the LAN-owned address")
	}
}

func TestDSLiteCustomToSharedLANSourceWithdrawsBeforeChangingOwner(t *testing.T) {
	c, f, spec := newDSLiteEndpointFixture(t, false)
	if _, err := c.ensureDelegatedEndpoint(t.Context(), "dslite", spec, "wan0", "2001:db8:1200:1::100"); err != nil {
		t.Fatal(err)
	}
	next := c.Router.Clone()
	lan, _ := next.Spec.Resources[1].IPv6DelegatedAddressSpec()
	lan.PrefixLength, lan.AddressSuffix = 128, "::100"
	next.Spec.Resources[1].Spec = lan
	c.Router = &next
	if err := c.cleanupDelegatedEndpoints(t.Context()); err != nil {
		t.Fatal(err)
	}
	receipts, err := c.endpointReceipts()
	if err != nil || len(receipts) != 0 || f.present != "" || len(f.writes) != 2 || !strings.Contains(f.writes[1], "addr del 2001:db8:1200:1::100/128") {
		t.Fatalf("custom source was not withdrawn before LAN acquisition: %+v writes=%v %v", receipts, f.writes, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if pending, err := c.ensureDelegatedEndpoint(ctx, "dslite", spec, "wan0", "2001:db8:1200:1::100"); pending != "LocalIPv6NotReady" || err != nil || len(f.writes) != 2 {
		t.Fatalf("DS-Lite duplicated the new LAN owner's apply: %s %v writes=%v", pending, err, f.writes)
	}
}

func TestDSLiteRechecksPDRevocationAfterReadinessBeforeTunnelEffects(t *testing.T) {
	c, f, _ := newDSLiteEndpointFixture(t, false)
	logPath := installFakeDSLiteIP(t, "", false)
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f.afterApplyRead = func() {
		f.changeLease(func(lease *routerstate.PDLease) { lease.CurrentPrefix, lease.Phase = "", daemonapi.ResourcePhaseIdle })
	}
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status := f.store.ObjectStatus(api.NetAPIVersion, "DSLiteTunnel", "dslite")
	if status["phase"] != "Pending" || f.present != "" || len(f.writes) != 2 {
		t.Fatalf("revoked source reached tunnel Up: status=%+v writes=%v", status, f.writes)
	}
	if calls := readDSLiteIPCalls(t, logPath); calls != "" {
		t.Fatalf("tunnel effects began after authority revocation: %s", calls)
	}
}

func TestDSLiteSourceUnknownObservationsRetainOwnership(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprintf("native=%t", native), func(t *testing.T) {
			c, f, spec := newDSLiteEndpointFixture(t, false)
			if _, err := c.ensureDelegatedEndpoint(t.Context(), "dslite", spec, "wan0", "2001:db8:1200:1::100"); err != nil {
				t.Fatal(err)
			}
			before := len(f.writes)
			if native {
				f.changeLease(func(lease *routerstate.PDLease) { lease.CurrentPrefix = "" })
				f.failRead = true
			} else {
				f.store.failPDRead = true
			}
			if err := c.cleanupDelegatedEndpoints(t.Context()); err == nil {
				t.Fatal("unknown observation was treated as absent")
			}
			receipts, err := c.endpointReceipts()
			if err != nil || len(receipts) != 1 || len(f.writes) != before {
				t.Fatalf("failed observation changed ownership: %+v %v", receipts, err)
			}
		})
	}
}

func TestDSLiteFiniteEndpointCommandForFreeBSD(t *testing.T) {
	name, args := ipv6DelegatedAddressApplyCommand(platform.OSFreeBSD, "em0", "2001:db8::100/128", 0, 45)
	if name != "ifconfig" || strings.Join(args, " ") != "em0 inet6 2001:db8::100 prefixlen 128 pltime 0 vltime 45 alias" {
		t.Fatalf("FreeBSD delegated source command = %s %v", name, args)
	}
}

func TestDSLiteCustomSourcePreservesForeignSameHostOnAnotherPrefix(t *testing.T) {
	c, f, spec := newDSLiteEndpointFixture(t, false)
	f.present = "2001:db8:1200:1::100/64"
	if _, err := c.ensureDelegatedEndpoint(t.Context(), "dslite", spec, "wan0", "2001:db8:1200:1::100"); err == nil || len(f.writes) != 0 {
		t.Fatalf("custom /128 mutated foreign same-host /64: %v writes=%v", err, f.writes)
	}
	receipt := dsliteEndpointReceipt{Name: "dslite", Interface: "wan0", Address: "2001:db8:1200:1::100/128", PDName: "wan-pd", PDPrefix: "2001:db8:1200::/60", ValidUntil: f.now.Add(time.Hour).Format(time.RFC3339Nano)}
	if err := c.setEndpointReceipt(receipt, false); err != nil {
		t.Fatal(err)
	}
	f.changeLease(func(lease *routerstate.PDLease) { lease.CurrentPrefix, lease.Phase = "", daemonapi.ResourcePhaseIdle })
	if err := c.cleanupDelegatedEndpoints(t.Context()); err != nil || len(f.writes) != 0 || f.present != "2001:db8:1200:1::100/64" {
		t.Fatalf("withdrawal touched foreign prefix after exact owned /128 disappeared: %v writes=%v", err, f.writes)
	}
}

func TestDSLitePendingStatusWriteFailureIsReported(t *testing.T) {
	c, f, _ := newDSLiteEndpointFixture(t, false)
	f.changeLease(func(lease *routerstate.PDLease) { lease.CurrentPrefix, lease.Phase = "", daemonapi.ResourcePhaseIdle })
	f.store.failTunnelStatus = true
	if err := c.reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "tunnel status write rejected") {
		t.Fatalf("failed pending publication was hidden: %v", err)
	}
}
