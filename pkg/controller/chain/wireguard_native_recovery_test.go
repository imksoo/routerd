// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type wgNativeReceiptFault struct {
	*routerstate.SQLiteStore
	failBinding bool
}

func (s *wgNativeReceiptFault) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if name == wireGuardNativeStatusName && s.failBinding {
		for _, record := range status["interfaces"].([]wireGuardNativeRecord) {
			if record.IfIndex > 0 {
				return errors.New("interface binding receipt failed")
			}
		}
	}
	return s.SQLiteStore.SaveObjectStatus(version, kind, name, status)
}

func TestWireGuardNativeRecoversCreatedInterfaceAfterReceiptFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	controller := WireGuardController{Store: &wgNativeReceiptFault{SQLiteStore: store, failBinding: true}, OS: platform.OSLinux}
	failDelete := true
	creates, deletes := 0, 0
	controller.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "ip" && len(args) > 1 && args[0] == "link" {
			if args[1] == "add" {
				records, err := controller.readNativeWireGuard()
				if err != nil || len(records) != 1 || !records[0].Pending || records[0].Token == "" {
					t.Fatalf("native creation preceded durable pending ownership: %+v err=%v", records, err)
				}
				creates++
			}
			if args[1] == "delete" {
				deletes++
				if failDelete {
					return nil, errors.New("delete failed")
				}
			}
		}
		return nil, nil
	}
	native := withWireGuardNativeFixture(t, &controller)
	if _, err := controller.ensureNativeWireGuard(t.Context(), "managed", "wg-managed"); err == nil {
		t.Fatal("creation success plus binding-save failure accepted")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	controller.Store = store
	if err := controller.cleanupNativeWireGuard(t.Context(), nil); err == nil {
		t.Fatal("failed destroy acknowledged")
	}
	records, err := controller.readNativeWireGuard()
	if err != nil || len(records) != 1 {
		t.Fatalf("failed destroy lost ownership: %+v err=%v", records, err)
	}
	failDelete = false
	if err := controller.cleanupNativeWireGuard(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	records, err = controller.readNativeWireGuard()
	if err != nil || len(records) != 0 || creates != 1 || deletes != 2 || len(native.links) != 0 {
		t.Fatalf("native recovery did not converge: records=%+v creates=%d deletes=%d native=%+v err=%v", records, creates, deletes, native.links, err)
	}
}

func TestWireGuardNativeRejectsForeignInterfaceAndUnreadableJournal(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	mutations := 0
	controller := WireGuardController{Store: store, OS: platform.OSLinux, Command: func(context.Context, string, ...string) ([]byte, error) { mutations++; return nil, nil }}
	native := withWireGuardNativeFixture(t, &controller)
	native.links["operator-wg"] = nativeWireGuardLink{Exists: true, IfIndex: 9, Kind: "wireguard"}
	if _, err := controller.ensureNativeWireGuard(t.Context(), "managed", "operator-wg"); err == nil || mutations != 0 {
		t.Fatalf("foreign interface adopted: mutations=%d err=%v", mutations, err)
	}
	if err := store.SaveObjectStatus(api.RouterAPIVersion, "Router", wireGuardNativeStatusName, map[string]any{"interfaces": "corrupt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.ensureNativeWireGuard(t.Context(), "managed", "new-wg"); err == nil || mutations != 0 {
		t.Fatalf("corrupt journal allowed effects: mutations=%d err=%v", mutations, err)
	}
}

func TestWireGuardNativeFreeBSDUsesOwnedDescriptionAndDestroy(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		t.Run(map[bool]string{false: "acknowledged", true: "partial-create"}[ambiguous], func(t *testing.T) {
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			controller := WireGuardController{Store: store, OS: platform.OSFreeBSD}
			exists, token := false, ""
			creates, deletes := 0, 0
			controller.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name != "ifconfig" {
					t.Fatalf("FreeBSD lifecycle used %s %v", name, args)
				}
				if len(args) == 1 && args[0] == "-l" {
					if exists {
						return []byte("lo0 wg-owned\n"), nil
					}
					return []byte("lo0\n"), nil
				}
				if len(args) == 1 {
					return []byte("wg-owned: flags=... mtu 1420\n\tdescription: routerd-wg:" + token + "\n\tgroups: wg\n"), nil
				}
				if args[0] == "wg" && args[1] == "create" {
					creates++
					records, err := controller.readNativeWireGuard()
					if err != nil || len(records) != 1 || !records[0].Pending {
						t.Fatalf("FreeBSD create preceded pending receipt: %+v err=%v", records, err)
					}
					exists = true
					if ambiguous {
						return nil, errors.New("description ioctl failed after creation")
					}
					token = strings.TrimPrefix(args[len(args)-1], "routerd-wg:")
					return nil, nil
				}
				if len(args) == 2 && args[1] == "destroy" {
					deletes++
					exists = false
					return nil, nil
				}
				t.Fatalf("unexpected FreeBSD mutation %v", args)
				return nil, nil
			}
			_, err = controller.ensureNativeWireGuard(t.Context(), "owned", "wg-owned")
			if ambiguous {
				if err == nil {
					t.Fatal("ambiguous unmarked interface adopted")
				}
				if _, err := controller.ensureNativeWireGuard(t.Context(), "owned", "wg-owned"); err == nil {
					t.Fatal("retry adopted tokenless interface")
				}
				if err := controller.cleanupNativeWireGuard(t.Context(), nil); err == nil {
					t.Fatal("unknown ownership authorized destroy")
				}
				if creates != 1 || deletes != 0 || !exists {
					t.Fatalf("ambiguous interface mutated again: creates=%d deletes=%d exists=%v", creates, deletes, exists)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := controller.cleanupNativeWireGuard(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			if creates != 1 || deletes != 1 || exists {
				t.Fatalf("native FreeBSD ownership lifecycle incomplete: creates=%d deletes=%d exists=%v", creates, deletes, exists)
			}
		})
	}
}

func TestWireGuardNativeReadbackFailureDoesNotPublishUp(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-owned"}, Spec: api.WireGuardInterfaceSpec{PrivateKey: wgFixturePrivate}}}}}
	controller := WireGuardController{Router: router, Store: store, OS: platform.OSLinux}
	withWireGuardNativeFixture(t, &controller)
	run, stdin := controller.Command, controller.CommandStdin
	configured := false
	failure := errors.New("native readback failed")
	controller.Command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if configured && name == "wg" {
			return nil, failure
		}
		return run(ctx, name, args...)
	}
	controller.CommandStdin = func(ctx context.Context, data []byte, name string, args ...string) ([]byte, error) {
		out, err := stdin(ctx, data, name, args...)
		configured = err == nil
		return out, err
	}
	if err := controller.Reconcile(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("readback failure accepted: %v", err)
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "WireGuardInterface", "wg-owned")
	if err != nil || status["phase"] == "Up" {
		t.Fatalf("unknown native result published Up: %+v err=%v", status, err)
	}
	records, err := controller.readNativeWireGuard()
	if err != nil || len(records) != 1 || !records[0].Pending {
		t.Fatalf("unknown native result lost recovery receipt: %+v err=%v", records, err)
	}
}

func TestWireGuardNativePreservesForeignBareFirewallRule(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-owned"}, Spec: api.WireGuardInterfaceSpec{PrivateKey: wgFixturePrivate, ListenPort: 51820}}}}}
	controller := WireGuardController{Router: router, Store: store, OS: platform.OSLinux}
	native := withWireGuardNativeFixture(t, &controller)
	const foreign = "-p udp --dport 51820 -j ACCEPT"
	native.rules[foreign] = true
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.rules) != 2 || !native.rules[foreign] {
		t.Fatalf("owned insertion adopted foreign rule: %+v", native.rules)
	}
	controller.Router = &api.Router{}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.rules) != 1 || !native.rules[foreign] {
		t.Fatalf("owned withdrawal deleted foreign rule: %+v", native.rules)
	}
}

func TestWireGuardNativeRecreatedInterfaceKeepsReceiptAndRefusesDelete(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	controller := WireGuardController{Store: store, OS: platform.OSLinux}
	native := withWireGuardNativeFixture(t, &controller)
	native.own(t, "owned", "wg-owned")
	recreated := native.links["wg-owned"]
	recreated.IfIndex = 9
	native.links["wg-owned"] = recreated
	if err := controller.cleanupNativeWireGuard(t.Context(), nil); err == nil {
		t.Fatal("changed native interface index accepted")
	}
	records, err := controller.readNativeWireGuard()
	if err != nil || len(records) != 1 || len(native.links) != 1 {
		t.Fatalf("recreated interface/ownership mutated: records=%+v native=%+v err=%v", records, native.links, err)
	}
}

func TestWireGuardNativeRepairsPresharedKeyDriftWithUnchangedConfig(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const psk = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-owned"}, Spec: api.WireGuardInterfaceSpec{PrivateKey: wgFixturePrivate}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardPeer"}, Metadata: api.ObjectMeta{Name: "peer"}, Spec: api.WireGuardPeerSpec{Interface: "wg-owned", PublicKey: wgFixturePublic, AllowedIPs: []string{"10.1.2.3/32"}, PresharedKey: psk}},
	}}}
	configures := 0
	controller := WireGuardController{Router: router, Store: store, OS: platform.OSLinux, CommandStdin: func(context.Context, []byte, string, ...string) ([]byte, error) {
		configures++
		return nil, nil
	}}
	native := withWireGuardNativeFixture(t, &controller)
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(t.Context()); err != nil || configures != 1 {
		t.Fatalf("unchanged native config was reapplied: configurations=%d err=%v", configures, err)
	}
	native.dumps["wg-owned"] = strings.Replace(native.dumps["wg-owned"], "\t"+psk+"\t", "\t(none)\t", 1)
	if err := controller.Reconcile(t.Context()); err != nil || configures != 2 {
		t.Fatalf("native PSK drift survived unchanged config hash: configurations=%d err=%v", configures, err)
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "WireGuardInterface", "wg-owned")
	if err != nil || status["phase"] != "Up" || !strings.Contains(native.dumps["wg-owned"], "\t"+psk+"\t") {
		t.Fatalf("PSK repair not verified: phase=%v err=%v", status["phase"], err)
	}
}
