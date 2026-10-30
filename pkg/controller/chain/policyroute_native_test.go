// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type policyNativeTestStore struct {
	*routerstate.SQLiteStore
	failPending, failApplied bool
	afterSave                func(policyIPv4Journal)
}

func (s *policyNativeTestStore) SaveObjectStatus(v, k, n string, status map[string]any) error {
	if n == policyIPv4JournalName {
		receipts := status["receipts"].([]policyIPv4Receipt)
		pending := false
		for _, r := range receipts {
			pending = pending || r.Pending != ""
		}
		if pending && s.failPending || !pending && s.failApplied {
			return errors.New("policy journal write failed")
		}
	}
	if err := s.SQLiteStore.SaveObjectStatus(v, k, n, status); err != nil {
		return err
	}
	if n == policyIPv4JournalName && s.afterSave != nil {
		j := policyIPv4Journal{Receipts: append([]policyIPv4Receipt{}, status["receipts"].([]policyIPv4Receipt)...)}
		s.afterSave(j)
	}
	return nil
}

// The fixture supplies native response shapes and changes them only after an
// actual production mutation command. It contains no reconciliation logic.
type policyNativeCommands struct {
	t                                                      *testing.T
	route, rule, foreignRule                               string
	index                                                  int
	calls                                                  []string
	failRead, failAdd, failDelete, ignoreAdd, ignoreDelete bool
	beforeWrite                                            func()
}

func (f *policyNativeCommands) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	switch call {
	case "ip -o link show dev wan0":
		return []byte(fmt.Sprintf("%d: wan0: <BROADCAST,UP> mtu 1500 state UP\n", f.index)), nil
	case "ip link show dev wan0":
		return nil, nil
	case "ip -4 route show default table 100":
		if f.failRead {
			return nil, errors.New("route inventory failed")
		}
		return []byte(f.route), nil
	case "ip -4 rule show priority 100":
		if f.failRead {
			return nil, errors.New("rule inventory failed")
		}
		return []byte(f.foreignRule + f.rule), nil
	}
	if strings.Contains(call, "-4 route add ") || strings.Contains(call, "-4 rule add ") || strings.Contains(call, "-4 route del ") || strings.Contains(call, "-4 rule del ") {
		if f.beforeWrite != nil {
			f.beforeWrite()
		}
		add := strings.Contains(call, " add ")
		if add && f.failAdd || !add && f.failDelete {
			return nil, errors.New("native acknowledgement lost")
		}
		switch call {
		case "ip -4 route add default via 192.0.2.1 dev wan0 table 100 metric 50 proto static":
			if !f.ignoreAdd {
				f.route = "default via 192.0.2.1 dev wan0 proto static metric 50\n"
			}
		case "ip -4 route del default via 192.0.2.1 dev wan0 table 100 metric 50 proto static":
			if !f.ignoreDelete {
				f.route = ""
			}
		case "ip -4 rule add priority 100 from all fwmark 0x100/0xffffffff table 100 protocol static":
			if !f.ignoreAdd {
				f.rule = "100: from all fwmark 0x100 lookup 100 proto static\n"
			}
		case "ip -4 rule del priority 100 from all fwmark 0x100/0xffffffff table 100 protocol static":
			if !f.ignoreDelete {
				f.rule = ""
			}
		default:
			f.t.Fatalf("unexpected native policy mutation: %s", call)
		}
		return nil, nil
	}
	f.t.Fatalf("unexpected native policy command: %s", call)
	return nil, nil
}
func (f *policyNativeCommands) mutations() []string {
	var out []string
	for _, s := range f.calls {
		if strings.Contains(s, " add ") || strings.Contains(s, " del ") || strings.Contains(s, " replace ") || strings.Contains(s, " flush ") {
			out = append(out, s)
		}
	}
	return out
}
func policyNativeRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "wan0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "policy"}, Spec: api.EgressRoutePolicySpec{Mode: "mark", Candidates: []api.EgressRoutePolicyCandidate{{Name: "wan", Interface: "wan", GatewaySource: "static", Gateway: "192.0.2.1", Table: 100, Priority: 100, Mark: 0x100}}}},
	}}}
}
func newPolicyNativeFixture(t *testing.T) (IPv4PolicyRouteController, *policyNativeTestStore, *policyNativeCommands, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	raw, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	store := &policyNativeTestStore{SQLiteStore: raw}
	t.Cleanup(func() { _ = store.Close() })
	native := &policyNativeCommands{t: t, index: 17}
	c := IPv4PolicyRouteController{Router: policyNativeRouter(), Store: store, OperatingSystem: platform.OSLinux, NativeScope: func(context.Context) (string, error) { return "linux:fixture:1", nil }, CommandOutput: native.command}
	return c, store, native, path
}
func applyPolicyNative(t *testing.T, c IPv4PolicyRouteController) error {
	t.Helper()
	return c.applyRouteTables(t.Context(), c.aliases())
}

func TestPolicyIPv4NativeAcquisitionAndExactWithdrawal(t *testing.T) {
	c, store, native, _ := newPolicyNativeFixture(t)
	native.foreignRule = "100: from all fwmark 0x999 lookup 999 proto static\n"
	store.afterSave = func(j policyIPv4Journal) {
		for _, r := range j.Receipts {
			if r.Pending == "add" {
				if r.Scope == "" {
					t.Fatal("pending effect lacks scope")
				}
			}
		}
	}
	if err := applyPolicyNative(t, c); err != nil {
		t.Fatal(err)
	}
	if got := native.mutations(); len(got) != 2 {
		t.Fatalf("acquisition writes=%v", got)
	}
	j, err := c.readPolicyIPv4Journal()
	if err != nil || len(j.Receipts) != 2 {
		t.Fatalf("journal=%+v, %v", j, err)
	}
	for _, r := range j.Receipts {
		if r.Pending != "" {
			t.Fatalf("unacknowledged effect=%+v", r)
		}
	}
	native.calls = nil
	if err := applyPolicyNative(t, c); err != nil {
		t.Fatal(err)
	}
	if len(native.mutations()) != 0 {
		t.Fatalf("unchanged desired replayed effects: %v", native.mutations())
	}
	c.Router = &api.Router{}
	if err := applyPolicyNative(t, c); err != nil {
		t.Fatal(err)
	}
	if got := native.mutations(); len(got) != 2 || !strings.Contains(got[0], "rule del") || !strings.Contains(got[1], "route del") {
		t.Fatalf("withdrawal=%v", got)
	}
	if native.foreignRule != "100: from all fwmark 0x999 lookup 999 proto static\n" {
		t.Fatal("unrelated same-priority rule changed")
	}
}
func TestPolicyIPv4NativeRefusesForeignPresenceBeforeAnyMutation(t *testing.T) {
	for _, kind := range []string{"route", "rule"} {
		t.Run(kind, func(t *testing.T) {
			c, _, native, _ := newPolicyNativeFixture(t)
			if kind == "route" {
				native.route = "default via 192.0.2.1 dev wan0 proto static metric 50\n"
			} else {
				native.rule = "100: from all fwmark 0x100 lookup 100 proto static\n"
			}
			if err := applyPolicyNative(t, c); err == nil {
				t.Fatal("unowned native effect was adopted")
			}
			if len(native.mutations()) != 0 {
				t.Fatalf("foreign collision changed native state: %v", native.mutations())
			}
		})
	}
}
func TestPolicyIPv4NativePendingWritePrecedesEveryEffect(t *testing.T) {
	c, store, native, _ := newPolicyNativeFixture(t)
	store.failPending = true
	if err := applyPolicyNative(t, c); err == nil {
		t.Fatal("failed journal allowed acquisition")
	}
	if len(native.mutations()) != 0 {
		t.Fatal(native.mutations())
	}
}
func TestPolicyIPv4NativeLostReplyAndReopenFencePresence(t *testing.T) {
	c, store, native, path := newPolicyNativeFixture(t)
	store.failApplied = true
	if err := applyPolicyNative(t, c); err == nil {
		t.Fatal("failed ACK journal reported success")
	}
	if native.route == "" {
		t.Fatal("regression never reached real native-effect boundary")
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
	native.calls = nil
	if err := applyPolicyNative(t, c); err == nil {
		t.Fatal("reopened pending presence was adopted")
	}
	if len(native.mutations()) != 0 {
		t.Fatal(native.mutations())
	}
	native.route = ""
	if err := applyPolicyNative(t, c); err != nil {
		t.Fatal(err)
	}
	if len(native.mutations()) != 2 {
		t.Fatalf("known absence did not release fence: %v", native.mutations())
	}
}
func TestPolicyIPv4NativeRereadsAuthorityAfterPendingWrite(t *testing.T) {
	c, store, native, _ := newPolicyNativeFixture(t)
	source := c.Router.Spec.Resources[1].Spec.(api.EgressRoutePolicySpec)
	source.Candidates[0].DependsOn = []api.ResourceDependencySpec{{Resource: "Interface/condition", Phase: "Up"}}
	c.Router.Spec.Resources[1].Spec = source
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "condition", map[string]any{"phase": "Up"}); err != nil {
		t.Fatal(err)
	}
	store.afterSave = func(j policyIPv4Journal) {
		for _, r := range j.Receipts {
			if r.Pending == "add" {
				store.afterSave = nil
				if err := store.SQLiteStore.SaveObjectStatus(api.NetAPIVersion, "Interface", "condition", map[string]any{"phase": "Down"}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if err := applyPolicyNative(t, c); err == nil {
		t.Fatal("changed authority was acknowledged")
	}
	if len(native.mutations()) != 0 {
		t.Fatal(native.mutations())
	}
}
func TestPolicyIPv4NativeReadFailureAndWorldChangePreserveOwnership(t *testing.T) {
	c, _, native, _ := newPolicyNativeFixture(t)
	if err := applyPolicyNative(t, c); err != nil {
		t.Fatal(err)
	}
	native.calls = nil
	native.failRead = true
	c.Router = &api.Router{}
	if err := applyPolicyNative(t, c); err == nil {
		t.Fatal("failed native read authorized cleanup")
	}
	if len(native.mutations()) != 0 {
		t.Fatal(native.mutations())
	}
	native.failRead = false
	c.NativeScope = func(context.Context) (string, error) { return "linux:another-world:2", nil }
	if err := applyPolicyNative(t, c); err != nil {
		t.Fatal(err)
	}
	if len(native.mutations()) != 0 {
		t.Fatal(native.mutations())
	}
	j, err := c.readPolicyIPv4Journal()
	if err != nil || len(j.Receipts) != 2 {
		t.Fatalf("old-world receipt discarded: %+v %v", j, err)
	}
}
