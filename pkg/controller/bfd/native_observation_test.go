// SPDX-License-Identifier: BSD-3-Clause

package bfd

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestBFDObservationCannotOverwriteChangedReceipt(t *testing.T) {
	for _, changeAt := range []int{1, 2, 3} {
		t.Run(string(rune('0'+changeAt)), func(t *testing.T) {
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			controller := Controller{Router: &api.Router{}, Store: store, RuntimeDir: t.TempDir()}
			sessions, _, err := (Controller{Router: bfdRouter()}).sessions()
			if err != nil {
				t.Fatal(err)
			}
			if err := controller.saveNativeReceipt(nativeReceipt{Held: sessions, Pending: []session{}}); err != nil {
				t.Fatal(err)
			}
			replacement := nativeReceipt{Held: append([]session(nil), sessions...), Pending: []session{}}
			replacement.Held[0].MinRxMS++
			reads, writes := 0, 0
			controller.Command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if args[0] == "-f" {
					writes++
					return nil, nil
				}
				if args[1] == "show bfd peers" {
					return []byte(nativePeersText), nil
				}
				reads++
				if reads == changeAt {
					if err := controller.saveNativeReceipt(replacement); err != nil {
						t.Fatal(err)
					}
				}
				if writes != 0 {
					return []byte(`[]`), nil
				}
				return []byte("[" + nativePeerA + "," + nativePeerB + "]"), nil
			}
			if err := controller.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "receipt changed") {
				t.Fatalf("changed receipt accepted: writes=%d err=%v", writes, err)
			}
			wantWrites := 0
			if changeAt == 3 {
				wantWrites = 1
			}
			actual, readErr := controller.readNativeReceipt()
			if writes != wantWrites || readErr != nil || !slices.Equal(actual.Held, replacement.Held) || !slices.Equal(actual.Pending, replacement.Pending) {
				t.Fatalf("changed receipt/effect not retained: writes=%d want=%d", writes, wantWrites)
			}
		})
	}
}

func TestBFDMismatchedTextObservationCannotAuthorizeWithdrawal(t *testing.T) {
	for _, text := range []string{"BFD Peers:\n", strings.ReplaceAll(nativePeersText, "configured", "dynamic"), strings.ReplaceAll(nativePeersText, "wg0", "changed0")} {
		store := testStore{}
		controller := Controller{Router: &api.Router{}, Store: store, RuntimeDir: t.TempDir()}
		sessions, _, _ := (Controller{Router: bfdRouter()}).sessions()
		receipt := nativeReceipt{Held: sessions, Pending: []session{}}
		if err := controller.saveNativeReceipt(receipt); err != nil {
			t.Fatal(err)
		}
		writes := 0
		controller.Command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if args[0] == "-f" {
				writes++
			}
			if args[1] == "show bfd peers" {
				return []byte(text), nil
			}
			return []byte("[" + nativePeerA + "," + nativePeerB + "]"), nil
		}
		err := controller.Reconcile(t.Context())
		actual, readErr := controller.readNativeReceipt()
		if err == nil || writes != 0 || readErr != nil || !slices.Equal(actual.Held, receipt.Held) || !slices.Equal(actual.Pending, receipt.Pending) {
			t.Fatalf("unproven text observation allowed withdrawal: writes=%d err=%v", writes, err)
		}
	}
}
