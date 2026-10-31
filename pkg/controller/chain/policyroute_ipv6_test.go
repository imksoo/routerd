// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// This boundary fixture changes native response shapes only in response to the
// production owner's explicit add/delete command. It has no ownership algorithm.
type policyIPv6Commands struct {
	t                                         *testing.T
	source, route                             string
	rules                                     map[int]string
	calls                                     []string
	index                                     int
	readFailure, sourceReadFailure, lostReply bool
}

func policyCommandValue(args []string, key string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key {
			return args[i+1]
		}
	}
	return ""
}
func (f *policyIPv6Commands) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	switch call {
	case "ip -o link show dev wan0":
		return []byte(fmt.Sprintf("%d: wan0: <BROADCAST,UP> mtu 1500 state UP\n", f.index)), nil
	case "ip -6 -o addr show dev wan0 scope global":
		if f.sourceReadFailure {
			return nil, errors.New("required source inventory failed")
		}
		if f.source == "" {
			return nil, nil
		}
		return []byte(fmt.Sprintf("%d: wan0 inet6 %s/64 scope global\n", f.index, f.source)), nil
	case "ip -6 -N route show table all exact default":
		if f.readFailure {
			return nil, errors.New("route inventory failed")
		}
		return []byte(f.route), nil
	}
	if name != "ip" || len(args) < 3 || args[0] != "-6" {
		f.t.Fatalf("unexpected native policy command %s", call)
	}
	if args[1] == "-N" && args[2] == "rule" && args[3] == "show" {
		if f.readFailure {
			return nil, errors.New("rule inventory failed")
		}
		priority, _ := strconv.Atoi(policyCommandValue(args, "priority"))
		return []byte(f.rules[priority]), nil
	}
	if args[1] == "route" && (args[2] == "add" || args[2] == "del") {
		for _, token := range []string{"via", "dev", "table", "metric", "proto", "src"} {
			if policyCommandValue(args, token) == "" {
				f.t.Fatalf("incomplete native deletion/acquisition identity: %s", call)
			}
		}
		if args[2] == "add" {
			f.route = "default via fe80::1 dev wan0 table 120 proto 4 src " + policyCommandValue(args, "src") + " metric 50 pref medium\n"
		} else {
			f.route = ""
		}
	} else if args[1] == "rule" && (args[2] == "add" || args[2] == "del") {
		priority, _ := strconv.Atoi(policyCommandValue(args, "priority"))
		if policyCommandValue(args, "protocol") != "static" {
			f.t.Fatalf("missing deletion/acquisition protocol: %s", call)
		}
		if args[2] == "add" {
			from := strings.TrimSuffix(policyCommandValue(args, "from"), "/128")
			row := strconv.Itoa(priority) + ": from " + from
			if input := policyCommandValue(args, "iif"); input != "" {
				row += " iif " + input
			}
			row += " lookup " + policyCommandValue(args, "table")
			if suppress := policyCommandValue(args, "suppress_prefixlength"); suppress != "" {
				row += " suppress_prefixlength " + suppress
			}
			f.rules[priority] = row + " proto 4\n"
		} else {
			delete(f.rules, priority)
		}
	} else {
		f.t.Fatalf("unexpected native policy command %s", call)
	}
	if f.lostReply {
		return nil, errors.New("native acknowledgement lost")
	}
	return nil, nil
}
func (f *policyIPv6Commands) mutations() []string {
	var calls []string
	for _, call := range f.calls {
		if strings.Contains(call, " add ") || strings.Contains(call, " del ") || strings.Contains(call, " replace ") || strings.Contains(call, " flush ") {
			calls = append(calls, call)
		}
	}
	return calls
}
func newPolicyIPv6Fixture(t *testing.T) (IPv4PolicyRouteController, *policyNativeTestStore, *policyIPv6Commands, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	raw, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	store := &policyNativeTestStore{SQLiteStore: raw}
	t.Cleanup(func() { _ = store.Close() })
	native := &policyIPv6Commands{t: t, source: "2001:db8::2", rules: map[int]string{}, index: 17}
	c := IPv4PolicyRouteController{Router: hostPolicyRouter(true), Store: store, OperatingSystem: platform.OSLinux, NativeScope: func(context.Context) (string, error) { return "linux:fixture:1", nil }, CommandOutput: native.command}
	return c, store, native, path
}
func TestPolicyIPv6NativeAcquisitionRepairAndExactWithdrawal(t *testing.T) {
	c, _, native, _ := newPolicyIPv6Fixture(t)
	if err := c.reconcileIPv6HostPolicies(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.mutations()) != 3 || strings.Contains(strings.Join(native.calls, "\n"), "replace") {
		t.Fatal(native.calls)
	}
	journal, err := c.readPolicyRouteJournal(6)
	if err != nil || len(journal.Receipts) != 3 {
		t.Fatalf("journal=%+v %v", journal, err)
	}
	writes := native.mutations()
	if !strings.Contains(writes[0], "route add") || !strings.Contains(writes[1], "priority 10119") || !strings.Contains(writes[2], "priority 10120") {
		t.Fatalf("route/guard/selector acquisition order: %v", writes)
	}
	if !journal.Receipts[1].Effect.SuppressMainDefault || journal.Receipts[1].Effect.Table != 254 {
		t.Fatal(journal)
	}
	for _, receipt := range journal.Receipts {
		if receipt.Pending != "" || receipt.Scope == "" || receipt.Effect.Family != 6 {
			t.Fatal(receipt)
		}
	}
	native.calls = nil
	if err := c.reconcileIPv6HostPolicies(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.mutations()) != 0 {
		t.Fatalf("unchanged desired replayed: %v", native.mutations())
	}
	delete(native.rules, 10120)
	if err := c.reconcileIPv6HostPolicies(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.mutations()) != 1 || !strings.Contains(native.mutations()[0], "rule add") {
		t.Fatal(native.mutations())
	}
	native.calls = nil
	c.Router = hostPolicyRouter(false)
	if err := c.reconcileIPv6HostPolicies(t.Context()); err != nil {
		t.Fatal(err)
	}
	if writes := native.mutations(); len(writes) != 3 || !strings.Contains(writes[0], "priority 10120") || !strings.Contains(writes[1], "priority 10119") || !strings.Contains(writes[1], "suppress_prefixlength 0") || !strings.Contains(writes[2], "route del") {
		t.Fatal(writes)
	}
}
func TestPolicyIPv6NativeForeignPresenceNeverAdoptsOrReplaces(t *testing.T) {
	for _, kind := range []string{"route", "rule", "broader-rule", "guard", "guard-without-suppress", "guard-other-suppress"} {
		t.Run(kind, func(t *testing.T) {
			c, _, native, _ := newPolicyIPv6Fixture(t)
			switch kind {
			case "route":
				native.route = "default via fe80::1 dev wan0 table 120 proto 4 src 2001:db8::2 metric 50 pref medium\n"
			case "rule":
				native.rules[10120] = "10120: from all iif lo lookup 120 proto 4\n"
			case "guard":
				native.rules[10119] = "10119: from all iif lo lookup 254 suppress_prefixlength 0 proto 4\n"
			case "guard-without-suppress":
				native.rules[10119] = "10119: from all iif lo lookup 254 proto 4\n"
			case "guard-other-suppress":
				native.rules[10119] = "10119: from all iif lo lookup 254 suppress_prefixlength 64 proto 4\n"
			default:
				native.rules[10120] = "10120: from all lookup 999 proto boot\n"
			}
			if err := c.reconcileIPv6HostPolicies(t.Context()); err == nil {
				t.Fatal("foreign policy accepted")
			}
			if len(native.mutations()) != 0 {
				t.Fatal(native.mutations())
			}
		})
	}
}
func TestPolicyIPv6NativeLostAcknowledgementReopenRetainsFence(t *testing.T) {
	c, store, native, path := newPolicyIPv6Fixture(t)
	native.lostReply = true
	if err := c.reconcileIPv6HostPolicies(t.Context()); err == nil {
		t.Fatal("lost native reply acknowledged")
	}
	if native.route == "" {
		t.Fatal("did not reach effect")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	c.Store = raw
	native.lostReply = false
	native.calls = nil
	if err := c.reconcileIPv6HostPolicies(t.Context()); err == nil {
		t.Fatal("pending presence adopted after reopen")
	}
	if len(native.mutations()) != 0 {
		t.Fatal(native.mutations())
	}
	native.route = ""
	if err := c.reconcileIPv6HostPolicies(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.mutations()) != 3 {
		t.Fatal(native.mutations())
	}
}
func TestPolicyIPv6NativeSourceFailurePreservesAndKnownAbsenceWithdraws(t *testing.T) {
	c, _, native, _ := newPolicyIPv6Fixture(t)
	if err := c.reconcileIPv6HostPolicies(t.Context()); err != nil {
		t.Fatal(err)
	}
	native.calls = nil
	native.sourceReadFailure = true
	if err := c.reconcileIPv6HostPolicies(t.Context()); err == nil {
		t.Fatal("required source read failure swallowed")
	}
	if len(native.mutations()) != 0 {
		t.Fatal(native.mutations())
	}
	native.sourceReadFailure = false
	native.source = ""
	if err := c.reconcileIPv6HostPolicies(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.mutations()) != 3 {
		t.Fatal(native.mutations())
	}
	status := c.Store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "host-ipv6-ra")
	if status["phase"] != "Pending" || status["reason"] != "PreferredSourceUnavailable" {
		t.Fatal(status)
	}
}
func TestPolicyIPv6NativeSourceChangeDuringPersistenceCannotActivate(t *testing.T) {
	c, store, native, _ := newPolicyIPv6Fixture(t)
	store.afterSave = func(policyRouteJournal) { store.afterSave = nil; native.source = "2001:db8::9" }
	if err := c.reconcileIPv6HostPolicies(t.Context()); err == nil {
		t.Fatal("changed preferred source accepted")
	}
	if len(native.mutations()) != 0 {
		t.Fatal(native.mutations())
	}
}
func TestPolicyIPv6NativeHostRoutingRetainsPhysicalWANAndDSLiteSourceScope(t *testing.T) {
	c, store, native, _ := newPolicyIPv6Fixture(t)
	c.Router.Spec.Resources = append(c.Router.Spec.Resources,
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "delegated"}, Spec: api.DSLiteTunnelSpec{Interface: "wan-vmac", LocalAddressSource: "delegatedAddress"}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "physical"}, Spec: api.DSLiteTunnelSpec{Interface: "wan"}},
	)
	for name, source := range map[string]string{"delegated": "2001:db8::23", "physical": "2001:db8::2"} {
		if err := store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", name, map[string]any{"phase": "Up", "localIPv6": source}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.reconcileIPv6HostPolicies(t.Context()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(native.mutations(), "\n")
	for _, want := range []string{"route add default via fe80::1 dev wan0 table 120", "rule add priority 10119 from all iif lo table 254 suppress_prefixlength 0 protocol static", "rule add priority 10100 from 2001:db8::23/128 table 254 protocol static", "rule add priority 10120 from all iif lo table 120 protocol static"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s: %s", want, joined)
		}
	}
	if strings.Contains(joined, "from 2001:db8::2/128") || strings.Contains(joined, "dev wan-vmac") {
		t.Fatal(joined)
	}
	data, err := render.NftablesIPv4PolicyRoutes(c.Router)
	if err != nil || strings.Contains(string(data), "120") {
		t.Fatalf("host routing leaked into IPv4 nft: %s %v", data, err)
	}
}

func TestPolicyIPv6NativeSourceReplacementStopsSelectorsBeforeRoutes(t *testing.T) {
	c, _, native, _ := newPolicyIPv6Fixture(t)
	if err := c.reconcileIPv6HostPolicies(t.Context()); err != nil {
		t.Fatal(err)
	}
	native.calls = nil
	native.source = "2001:db8::9"
	if err := c.reconcileIPv6HostPolicies(t.Context()); err != nil {
		t.Fatal(err)
	}
	writes := native.mutations()
	if len(writes) != 4 || !strings.Contains(writes[0], "rule del") || !strings.Contains(writes[1], "route del") || !strings.Contains(writes[2], "route add") || !strings.Contains(writes[3], "rule add") {
		t.Fatal(writes)
	}
	if !strings.Contains(writes[1], "src 2001:db8::2") || !strings.Contains(writes[2], "src 2001:db8::9") {
		t.Fatal(writes)
	}
}

func TestPolicyIPv6NativeAllTableObservationIgnoresUnrelatedRADefault(t *testing.T) {
	c, _, native, _ := newPolicyIPv6Fixture(t)
	native.route = "default via fe80::9 dev foreign proto 9 metric 50 expires 30sec pref high\nunreachable default table 999 proto 4 metric 50 pref medium\n"
	effect := policyRouteEffect{Family: 6, Kind: "route", Table: 120, Gateway: "fe80::1", Interface: "wan0", Source: "2001:db8::2", Metric: 50, IfIndex: 17}
	match, collision, err := c.observePolicyRouteEffect(t.Context(), effect)
	if err != nil || match || collision {
		t.Fatalf("foreign RA/default observation=%v %v %v", match, collision, err)
	}
}

func TestPolicyIPv6NativeFamilyJournalDoesNotWithdrawIPv4(t *testing.T) {
	c, _, ipv4, _ := newPolicyNativeFixture(t)
	if err := applyPolicyNative(t, c); err != nil {
		t.Fatal(err)
	}
	before, err := c.readPolicyRouteJournal(4)
	if err != nil {
		t.Fatal(err)
	}
	ipv6 := &policyIPv6Commands{t: t, source: "2001:db8::2", rules: map[int]string{}, index: 17}
	c.Router = hostPolicyRouter(true)
	c.CommandOutput = ipv6.command
	ipv4.calls = nil
	if err := c.reconcileIPv6HostPolicies(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := c.readPolicyRouteJournal(4)
	if err != nil || len(after.Receipts) != len(before.Receipts) {
		t.Fatalf("IPv4 receipts=%+v %v", after, err)
	}
	for i := range before.Receipts {
		if before.Receipts[i] != after.Receipts[i] {
			t.Fatalf("IPv4 receipt changed: %+v -> %+v", before, after)
		}
	}
	if len(ipv4.mutations()) != 0 {
		t.Fatal(ipv4.mutations())
	}
}

func TestPolicyIPv6NativeOwnedReceiptCannotHideForeignSelectorOverlap(t *testing.T) {
	c, _, native, _ := newPolicyIPv6Fixture(t)
	if err := c.reconcileIPv6HostPolicies(t.Context()); err != nil {
		t.Fatal(err)
	}
	native.calls = nil
	native.rules[10120] += "10120: from all lookup 999 proto 4\n"
	if err := c.reconcileIPv6HostPolicies(t.Context()); err == nil {
		t.Fatal("owned rule masked a foreign overlapping selector")
	}
	if len(native.mutations()) != 0 {
		t.Fatal(native.mutations())
	}
}

// A generated guard must not collide with an existing DS-Lite source selector,
// even when neither effect is present yet.
func TestPolicyIPv6NativeGuardPriorityConflictHasNoEffects(t *testing.T) {
	c, store, native, _ := newPolicyIPv6Fixture(t)
	spec, err := c.Router.Spec.Resources[1].EgressRoutePolicySpec()
	if err != nil {
		t.Fatal(err)
	}
	spec.Candidates[0].Priority = 10101
	c.Router.Spec.Resources[1].Spec = spec
	c.Router.Spec.Resources = append(c.Router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
		Metadata: api.ObjectMeta{Name: "delegated"},
		Spec:     api.DSLiteTunnelSpec{Interface: "wan-vmac", LocalAddressSource: "delegatedAddress"},
	})
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", "delegated", map[string]any{"phase": "Up", "localIPv6": "2001:db8::23"}); err != nil {
		t.Fatal(err)
	}
	if err := c.reconcileIPv6HostPolicies(t.Context()); err == nil {
		t.Fatal("overlapping generated guard priority accepted")
	}
	if len(native.mutations()) != 0 {
		t.Fatal(native.mutations())
	}
	journal, err := c.readPolicyRouteJournal(6)
	if err != nil || len(journal.Receipts) != 0 {
		t.Fatalf("conflict saved ownership: %+v %v", journal, err)
	}
}
