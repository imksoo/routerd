// SPDX-License-Identifier: BSD-3-Clause

package bfd

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestBFDReviewCLIOnlyWantedPeerCannotAuthorizeApply(t *testing.T) {
	foreignHeader := " peer 10.99.0.2 vrf default interface operator0"
	at := strings.Index(nativePeersText, foreignHeader)
	if at < 0 {
		t.Fatal("missing independent foreign tuple in the FRR text fixture")
	}
	for _, scenario := range []struct {
		name, initialJSON, initialText string
	}{
		{"other-peer-present", "[" + foreignPeer + "]", "BFD Peers:\n" + nativePeersText[at:]},
		{"empty-json", "[]", "BFD Peers:\n"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			reads, writes := 0, 0
			controller := Controller{Router: bfdRouter(), Store: store, OS: platform.OSLinux, RuntimeDir: t.TempDir()}
			controller.Command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if args[0] == "-f" {
					writes++
					return nil, errors.New("review blocked configuration of a newly appeared foreign tuple")
				}
				switch args[1] {
				case "show bfd peers json":
					reads++
					return []byte(scenario.initialJSON), nil
				case "show bfd peers":
					if reads == 1 {
						return []byte(scenario.initialText), nil
					}
					// The wanted native tuples appeared after the second JSON
					// snapshot and before its CLI observation completed.
					return []byte(nativePeersText), nil
				default:
					t.Fatalf("unexpected native BFD command: %v", args)
					return nil, nil
				}
			}
			err = controller.Reconcile(t.Context())
			if err == nil || writes != 0 {
				t.Fatalf("CLI-only foreign wanted tuple authorized native configuration: JSON reads=%d effects=%d err=%v", reads, writes, err)
			}
		})
	}
}

func TestBFDReviewUnreadableCLIWithEmptyJSONRetainsReceipt(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	controller := Controller{Router: &api.Router{}, Store: store, OS: platform.OSLinux, RuntimeDir: t.TempDir()}
	sessions, _, err := (Controller{Router: bfdRouter()}).sessions()
	if err != nil {
		t.Fatal(err)
	}
	previous := nativeReceipt{Held: sessions, Pending: []session{}}
	if err := controller.saveNativeReceipt(previous); err != nil {
		t.Fatal(err)
	}
	writes := 0
	controller.Command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "-f" {
			writes++
			return nil, nil
		}
		if args[1] == "show bfd peers json" {
			return []byte("[]"), nil
		}
		return []byte("peer observation unavailable\n"), nil
	}
	reconcileErr := controller.Reconcile(t.Context())
	actual, readErr := controller.readNativeReceipt()
	if reconcileErr == nil || writes != 0 || readErr != nil || !slices.Equal(actual.Held, previous.Held) || !slices.Equal(actual.Pending, previous.Pending) {
		t.Fatalf("unknown CLI became absence and forgot ownership: effects=%d actual=%+v previous=%+v reconcile=%v read=%v", writes, actual, previous, reconcileErr, readErr)
	}
}

func TestBFDReviewPreApplyAbortCannotGrantOwnershipOnRetry(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	controller := Controller{Router: bfdRouter(), Store: store, OS: platform.OSLinux, RuntimeDir: t.TempDir()}
	foreignHeader := " peer 10.99.0.2 vrf default interface operator0"
	at := strings.Index(nativePeersText, foreignHeader)
	if at < 0 {
		t.Fatal("missing foreign tuple fixture")
	}
	reads, writes, attempt := 0, 0, 0
	controller.Command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "-f" {
			writes++
			return nil, errors.New("review blocked modification of independently created peers")
		}
		if args[1] == "show bfd peers json" {
			reads++
			if attempt == 0 {
				return []byte("[" + foreignPeer + "]"), nil
			}
			return []byte("[" + nativePeerA + "," + nativePeerB + "," + foreignPeer + "]"), nil
		}
		if attempt == 0 && reads == 1 {
			return []byte("BFD Peers:\n" + nativePeersText[at:]), nil
		}
		return []byte(nativePeersText), nil
	}
	firstErr := controller.Reconcile(t.Context())
	if firstErr == nil || writes != 0 {
		t.Fatalf("pre-apply mismatch did not abort before all native effects: writes=%d err=%v", writes, firstErr)
	}
	attempt = 1
	secondErr := controller.Reconcile(t.Context())
	adopted, readErr := controller.readNativeReceipt()
	if readErr != nil {
		t.Fatal(readErr)
	}
	controller.Router = &api.Router{}
	withdrawErr := controller.Reconcile(t.Context())
	if secondErr == nil || len(adopted.Held) != 0 || writes != 0 {
		t.Fatalf("known no-effect abort became foreign ownership on retry: held=%+v effects=%d first=%v retry=%v removal=%v", adopted.Held, writes, firstErr, secondErr, withdrawErr)
	}
}
