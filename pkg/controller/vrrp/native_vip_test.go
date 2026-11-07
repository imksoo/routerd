// SPDX-License-Identifier: BSD-3-Clause

package vrrp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// The fixture owns only the native address observation; the original command
// collaborator still decides whether each real mutation succeeds and records it.
func withNativeVIPFixture(t *testing.T, c *Controller, address string, present bool, vhid int) {
	t.Helper()
	command := c.Command
	prefix := netip.MustParsePrefix(address)
	c.Command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if c.currentOS() == platform.OSLinux && name == "ip" && len(args) == 5 && args[0] == "-j" {
			addresses := []map[string]any{}
			if present {
				addresses = append(addresses, map[string]any{"local": prefix.Addr().String(), "prefixlen": prefix.Bits()})
			}
			return json.Marshal([]map[string]any{{"ifname": args[4], "addr_info": addresses}})
		}
		if c.currentOS() == platform.OSFreeBSD && name == "ifconfig" && len(args) == 1 {
			out := args[0] + ": flags=...\n"
			if present {
				out += fmt.Sprintf("\tinet %s netmask 0xffffffff", prefix.Addr())
				if vhid != 0 {
					out += fmt.Sprintf(" vhid %d", vhid)
				}
				out += "\n"
				if vhid != 0 {
					out += fmt.Sprintf("\tcarp: MASTER vhid %d advbase 1 advskew 104\n", vhid)
				}
			}
			return []byte(out), nil
		}
		out, err := command(ctx, name, args...)
		if err == nil {
			joined := " " + strings.Join(args, " ") + " "
			if strings.Contains(joined, " replace ") || strings.Contains(joined, " alias ") {
				present = true
			}
			if strings.Contains(joined, " del ") || strings.Contains(joined, " -alias ") {
				present = false
			}
		}
		return out, err
	}
}

func seedOwnedVIP(t *testing.T, c *Controller, name, address string, vhid int) {
	t.Helper()
	backend := c.staticVirtualAddressBackend()
	if vhid != 0 {
		backend = "carp"
	}
	if err := c.saveNativeVIPJournal(nativeVIPJournal{Applied: []nativeVIP{{Kind: "VirtualAddress", Name: name, IfName: "ens18", Address: address, Backend: backend, VHID: vhid}}}); err != nil {
		t.Fatal(err)
	}
}

type vipReceiptFaultStore struct {
	*routerstate.SQLiteStore
	failFinal bool
}

func (s *vipReceiptFaultStore) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if name == nativeVIPStatusName && s.failFinal && len(status["pending"].([]nativeVIP)) == 0 {
		return errors.New("final native VIP receipt failed")
	}
	return s.SQLiteStore.SaveObjectStatus(version, kind, name, status)
}

func TestNativeVIPAcquisitionRecoversThroughGARPAndReceiptFailure(t *testing.T) {
	for _, failure := range []string{"garp", "receipt"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state.db")
			store, err := routerstate.OpenSQLite(path)
			if err != nil {
				t.Fatal(err)
			}
			fault := &vipReceiptFaultStore{SQLiteStore: store, failFinal: failure == "receipt"}
			router := vrrpRouter("static")
			spec := router.Spec.Resources[1].Spec.(api.VirtualAddressSpec)
			spec.GratuitousARP = true
			router.Spec.Resources[1].Spec = spec
			controller := Controller{Router: router, Store: fault, OperatingSystem: platform.OSLinux, ConfigPath: filepath.Join(dir, "missing-keepalived")}
			adds, announcements, deletes := 0, 0, 0
			controller.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name == "ip" && args[0] == "addr" {
					if args[1] == "replace" {
						journal, err := controller.readNativeVIPJournal()
						if err != nil || len(journal.Pending) != 1 {
							t.Fatalf("native effect preceded pending intent: %+v err=%v", journal, err)
						}
						adds++
					} else if args[1] == "del" {
						deletes++
					}
				}
				if name == "arping" {
					announcements++
					if failure == "garp" && announcements == 1 {
						return nil, errors.New("GARP send failed")
					}
				}
				return nil, nil
			}
			withNativeVIPFixture(t, &controller, "10.240.70.10/32", false, 0)
			if err := controller.Reconcile(t.Context()); err == nil {
				t.Fatal("uncertain acquisition reported success")
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
			if err := controller.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if adds != 1 || announcements != 2 {
				t.Fatalf("acquisition/GARP recovery lost intent: adds=%d announcements=%d", adds, announcements)
			}
			if err := controller.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if adds != 1 || announcements != 2 {
				t.Fatal("completed acquisition repeated native effects")
			}
			controller.Router = &api.Router{}
			if err := controller.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			journal, err := controller.readNativeVIPJournal()
			if err != nil || deletes != 1 || len(journal.Applied)+len(journal.Pending) != 0 {
				t.Fatalf("withdrawal failed: deletes=%d journal=%+v err=%v", deletes, journal, err)
			}
		})
	}
}

func TestNativeVIPDoesNotAdoptForeignAddressOrDisplayHistory(t *testing.T) {
	store := mapStore{api.NetAPIVersion + "/VirtualAddress/vip": {"phase": "Applied", "appliedAddress": "10.240.70.10/32", "ifname": "ens18"}}
	mutations := 0
	controller := Controller{Router: vrrpRouter("static"), Store: store, OperatingSystem: platform.OSLinux, ConfigPath: filepath.Join(t.TempDir(), "missing"), Command: func(context.Context, string, ...string) ([]byte, error) { mutations++; return nil, nil }}
	withNativeVIPFixture(t, &controller, "10.240.70.10/32", true, 0)
	if err := controller.Reconcile(t.Context()); err == nil || mutations != 0 {
		t.Fatalf("display history adopted foreign native address: mutations=%d err=%v", mutations, err)
	}
	controller.Router = &api.Router{}
	if err := controller.Reconcile(t.Context()); err != nil || mutations != 0 {
		t.Fatalf("display history authorized native deletion: mutations=%d err=%v", mutations, err)
	}
}

func TestNativeCARPWithdrawalRetainsExactVHIDOwnership(t *testing.T) {
	for _, backend := range []string{"static", "vrrp"} {
		t.Run(backend, func(t *testing.T) {
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			var mutations []string
			controller := Controller{Router: vrrpRouter(backend), Store: store, OperatingSystem: platform.OSFreeBSD, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name == "ifconfig" {
					mutations = append(mutations, strings.Join(args, " "))
				}
				return []byte("0"), nil
			}}
			vhid := 0
			if backend == "vrrp" {
				vhid = 50
			}
			withNativeVIPFixture(t, &controller, "10.240.70.10/32", false, vhid)
			if err := controller.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			controller.Router = &api.Router{}
			if err := controller.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(mutations) != 2 || mutations[1] != "ens18 inet 10.240.70.10/32 -alias" {
				t.Fatalf("native %s withdrawal missing: %v", backend, mutations)
			}
		})
	}
	t.Run("foreign-vhid", func(t *testing.T) {
		mutations := 0
		controller := Controller{Router: &api.Router{}, Store: mapStore{}, OperatingSystem: platform.OSFreeBSD, Command: func(context.Context, string, ...string) ([]byte, error) { mutations++; return nil, nil }}
		seedOwnedVIP(t, &controller, "vip", "10.240.70.10/32", 50)
		withNativeVIPFixture(t, &controller, "10.240.70.10/32", true, 51)
		if err := controller.Reconcile(t.Context()); err == nil || mutations != 0 {
			t.Fatalf("changed native VHID allowed delete: mutations=%d err=%v", mutations, err)
		}
		journal, err := controller.readNativeVIPJournal()
		if err != nil || len(journal.Applied) != 1 {
			t.Fatalf("foreign VHID erased previous receipt: %+v err=%v", journal, err)
		}
	})
}
