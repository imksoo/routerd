// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/manageddaemon"
)

func TestStoppedBGPActorCannotRemoveNextGenerationFIB(t *testing.T) {
	store, backend, fib := newFIBOwnerFixture(t)
	if _, err := syncFIBFixture(t.Context(), fib, store, fibDesired("192.0.2.1")); err != nil {
		t.Fatal(err)
	}
	old := &Controller{Store: store, FIB: fib, Server: &fakeServer{}, Daemon: manageddaemon.Spec{StatePath: filepath.Join(t.TempDir(), "applied.json")}}
	if err := old.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	// The replacement actor owns the same kernel scope and SQL receipt now.
	current := &nativeFIBSyncer{backend: backend}
	if _, err := syncFIBFixture(t.Context(), current, store, fibDesired("192.0.2.2")); err != nil {
		t.Fatal(err)
	}
	receipt, _, err := store.ReadBGPFIB(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	effectCount, stops := len(backend.effects), 0
	if err := old.Remove(t.Context(), func(context.Context) error { stops++; return nil }); err == nil || !strings.Contains(err.Error(), "generation has stopped") {
		t.Fatalf("stopped actor removal admission=%v", err)
	}
	after, _, err := store.ReadBGPFIB(t.Context())
	if err != nil || !reflect.DeepEqual(receipt, after) || len(backend.effects) != effectCount || stops != 0 {
		t.Fatalf("stale generation changed current ownership: stops=%d effects=%v receipt=%+v err=%v", stops, backend.effects, after, err)
	}
}
