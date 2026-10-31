//go:build linux || freebsd

// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func persistentReviewRouter(optional bool, runtime *bool) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"}, Metadata: api.ObjectMeta{Name: "owned-review"},
		Spec: api.SysctlSpec{Key: "routerd.review.unavailable", Value: "1", Persistent: true, Optional: optional, Runtime: runtime},
	}}}}
}

func TestReviewUnknownSysctlDeclarationCannotWithdrawAcquiredBlock(t *testing.T) {
	directory := t.TempDir()
	store, err := routerstate.OpenSQLite(filepath.Join(directory, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime := false
	path := filepath.Join(directory, "sysctl.conf.local")
	controller := SysctlController{Router: persistentReviewRouter(false, &runtime), Store: store, OS: platform.OSFreeBSD, PersistentPath: path,
		Command: func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("persistent-only owner dispatched a native operation")
			return nil, nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	authorityName := routerstate.SysctlPersistentOwnerName("owned-review", "routerd.review.unavailable")
	acquired, err := store.ReadObjectStatus(api.SystemAPIVersion, "Sysctl", authorityName)
	if err != nil || acquired[sysctlPersistentOwnerKey] == nil {
		t.Fatalf("actual acquisition produced no receipt: status=%+v err=%v", acquired, err)
	}
	controller.Router = nil
	reconcileErr := controller.Reconcile(t.Context())
	after, readErr := os.ReadFile(path)
	current, statusErr := store.ReadObjectStatus(api.SystemAPIVersion, "Sysctl", authorityName)
	if reconcileErr == nil || readErr != nil || statusErr != nil || !bytes.Equal(before, after) || !reflect.DeepEqual(acquired[sysctlPersistentOwnerKey], current[sysctlPersistentOwnerKey]) {
		t.Fatalf("unknown Router withdrew a proven block: err=%v readErr=%v statusErr=%v before=%q after=%q receiptBefore=%+v receiptAfter=%+v", reconcileErr, readErr, statusErr, before, after, acquired[sysctlPersistentOwnerKey], current[sysctlPersistentOwnerKey])
	}
}

func TestReviewOptionalUnknownRuntimeKeyDoesNotPersistSkippedSetting(t *testing.T) {
	sysctl, err := exec.LookPath("sysctl")
	if err != nil {
		t.Skip("requires actual read-only sysctl")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "sysctl.conf.local")
	before := []byte("# operator setting remains unchanged\noperator.unrelated = 7\n")
	if err := os.WriteFile(path, before, 0640); err != nil {
		t.Fatal(err)
	}
	store, err := routerstate.OpenSQLite(filepath.Join(directory, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	controller := SysctlController{Router: persistentReviewRouter(true, nil), Store: store, OS: platform.OSFreeBSD, PersistentPath: path,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name != "sysctl" || len(args) != 2 || args[0] != "-n" || args[1] != "routerd.review.unavailable" {
				t.Fatalf("unexpected native mutation: %s %v", name, args)
			}
			out, err := exec.CommandContext(ctx, sysctl, args...).CombinedOutput()
			if err == nil {
				t.Fatal("missing-key observation unexpectedly succeeded")
			}
			return out, err
		},
	}
	reconcileErr := controller.Reconcile(t.Context())
	after, readErr := os.ReadFile(path)
	current, statusErr := store.ReadObjectStatus(api.SystemAPIVersion, "Sysctl", "owned-review")
	if reconcileErr != nil || readErr != nil || statusErr != nil || current["phase"] != "Skipped" || current[sysctlPersistentOwnerKey] != nil || !bytes.Equal(before, after) {
		t.Fatalf("Skipped optional setting changed boot policy: err=%v readErr=%v statusErr=%v before=%q after=%q status=%+v", reconcileErr, readErr, statusErr, before, after, current)
	}
}
