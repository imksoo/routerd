// SPDX-License-Identifier: BSD-3-Clause

package bfd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const nativePeerA = `{"peer":"10.99.0.2","vrf":"default","interface":"wg0","status":"up","type":"configured","receive-interval":300,"transmit-interval":300,"detect-multiplier":3}`
const nativePeerB = `{"peer":"10.99.0.3","vrf":"default","interface":"wg0","status":"up","type":"configured","receive-interval":300,"transmit-interval":300,"detect-multiplier":3}`
const foreignPeer = `{"peer":"10.99.0.2","vrf":"default","interface":"operator0","status":"up","type":"configured","receive-interval":300,"transmit-interval":300,"detect-multiplier":3}`

const nativeOwnedPeersText = `BFD Peers:
 peer 10.99.0.2 vrf default interface wg0
  Peer Type: configured
  Local timers:
   Detect-multiplier: 3
   Receive interval: 300ms
   Transmission interval: 300ms
 peer 10.99.0.3 vrf default interface wg0
  Peer Type: configured
  Local timers:
   Detect-multiplier: 3
   Receive interval: 300ms
   Transmission interval: 300ms
`

const nativeForeignPeerText = `BFD Peers:
 peer 10.99.0.2 vrf default interface operator0
  Peer Type: configured
  Local timers:
   Detect-multiplier: 3
   Receive interval: 300ms
   Transmission interval: 300ms
`

const nativePeersText = nativeOwnedPeersText + nativeForeignPeerText

type receiptFaultStore struct {
	*routerstate.SQLiteStore
	failFinal bool
	intentErr error
}

func (s *receiptFaultStore) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if name == nativeStatusName {
		pending := status["pending"].([]session)
		if len(pending) != 0 && s.intentErr != nil {
			return s.intentErr
		}
		if len(pending) == 0 && s.failFinal {
			return errors.New("receipt final write failed")
		}
	}
	return s.SQLiteStore.SaveObjectStatus(version, kind, name, status)
}

func TestBFDRecoversUncertainAddAndWithdrawsOnlyOwnedPeers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	fault := &receiptFaultStore{SQLiteStore: store, failFinal: true}
	installed := false
	failedDelete := true
	controller := Controller{Router: bfdRouter(), Store: fault, OS: platform.OSLinux, RuntimeDir: dir}
	controller.Command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "-c" && args[1] == "show bfd peers" {
			if installed {
				return []byte(nativePeersText), nil
			}
			return []byte(nativeForeignPeerText), nil
		}
		if args[0] == "-c" {
			if installed {
				return []byte("[" + nativePeerA + "," + nativePeerB + "," + foreignPeer + "]"), nil
			}
			return []byte("[" + foreignPeer + "]"), nil
		}
		config, err := os.ReadFile(args[1])
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(config), "no peer") {
			if !strings.Contains(string(config), "no peer 10.99.0.2 interface wg0") || !strings.Contains(string(config), "no peer 10.99.0.3 interface wg0") || strings.Contains(string(config), "operator0") {
				t.Fatalf("withdrawal did not preserve foreign exact tuple: %s", config)
			}
			if failedDelete {
				return nil, errors.New("native delete failed")
			}
			installed = false
		} else {
			// The actual persistence boundary must already authorize recovery
			// before the collaborator can mutate native peer state.
			receipt, err := controller.readNativeReceipt()
			if err != nil || len(receipt.Pending) != 2 {
				t.Fatalf("effect preceded durable intent: %#v err=%v", receipt, err)
			}
			installed = true
		}
		return nil, nil
	}
	if err := controller.Reconcile(t.Context()); err == nil {
		t.Fatal("native success plus failed final receipt accepted")
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
	controller.Router = &api.Router{}
	if err := controller.Reconcile(t.Context()); err == nil {
		t.Fatal("failed native withdrawal accepted")
	}
	receipt, err := controller.readNativeReceipt()
	if err != nil || len(receipt.Held)+len(receipt.Pending) != 2 {
		t.Fatalf("failed withdrawal forgot uncertain additions: %#v err=%v", receipt, err)
	}
	failedDelete = false
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	receipt, err = controller.readNativeReceipt()
	if err != nil || len(receipt.Held) != 0 || len(receipt.Pending) != 0 || installed {
		t.Fatalf("owned withdrawal did not converge: %#v err=%v", receipt, err)
	}
}

func TestBFDRejectsForeignNativePeerAndUnreadableObservationBeforeEffects(t *testing.T) {
	for _, input := range []string{"[" + nativePeerA + "]", "not json", "null"} {
		t.Run(input, func(t *testing.T) {
			writes := 0
			controller := Controller{Router: bfdRouter(), Store: testStore{}, OS: platform.OSLinux, RuntimeDir: t.TempDir(), Command: func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if args[0] == "-f" {
					writes++
				}
				return []byte(input), nil
			}}
			if err := controller.Reconcile(t.Context()); err == nil || writes != 0 {
				t.Fatalf("unsafe native adoption/read accepted: writes=%d err=%v", writes, err)
			}
		})
	}
}

func TestBFDIntentWriteFailurePreventsNativeApply(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	failure := errors.New("intent unavailable")
	writes := 0
	controller := Controller{Router: bfdRouter(), Store: &receiptFaultStore{SQLiteStore: store, intentErr: failure}, OS: platform.OSLinux, RuntimeDir: t.TempDir(), Command: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "-c" && args[1] == "show bfd peers" {
			return []byte("BFD Peers:\n"), nil
		}
		if args[0] == "-f" {
			writes++
		}
		return []byte(`[]`), nil
	}}
	if err := controller.Reconcile(t.Context()); !errors.Is(err, failure) || writes != 0 {
		t.Fatalf("native apply proceeded without intent: writes=%d err=%v", writes, err)
	}
}

func TestBFDUnreadableOwnershipPreventsNativeCommands(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "closed", true: "corrupt"}[corrupt], func(t *testing.T) {
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if corrupt {
				if err := store.SaveObjectStatus(api.RouterAPIVersion, "Router", nativeStatusName, map[string]any{"held": "unreadable", "pending": []session{}}); err != nil {
					t.Fatal(err)
				}
			} else if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			commands := 0
			controller := Controller{Router: bfdRouter(), Store: store, OS: platform.OSLinux, RuntimeDir: t.TempDir(), Command: func(context.Context, string, ...string) ([]byte, error) {
				commands++
				return nil, nil
			}}
			if err := controller.Reconcile(t.Context()); err == nil || commands != 0 {
				t.Fatalf("ownership read failure allowed native access: commands=%d err=%v", commands, err)
			}
		})
	}
}
