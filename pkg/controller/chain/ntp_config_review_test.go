// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type ntpReviewReadStore struct {
	*routerstate.SQLiteStore
	ownerKey string
	after    func()
}

func (s *ntpReviewReadStore) ReadObjectStatus(version, kind, name string) (map[string]any, error) {
	status, err := s.SQLiteStore.ReadObjectStatus(version, kind, name)
	if err == nil && version == api.RouterAPIVersion && kind == "Router" && name == s.ownerKey && s.after != nil {
		s.after()
	}
	return status, err
}

func TestNTPReviewWithdrawalRechecksRecordAndFileAfterWait(t *testing.T) {
	for _, kind := range []string{"NTPClient", "NTPServer"} {
		for _, change := range []string{"file replaced during receipt check", "receipt changed before absent retry"} {
			t.Run(kind+"/"+change, func(t *testing.T) {
				store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				path := filepath.Join(t.TempDir(), "routerd.conf")
				owner := ntpConfigOwner{store: store, kind: kind}
				view := Store(store)
				router := ntpRouter(api.NTPClientSpec{Provider: "chrony", Managed: true, Servers: []string{"192.0.2.123"}})
				if kind == "NTPServer" {
					router = ntpServerRouter(api.NTPServerSpec{Provider: "chrony", Managed: true, Servers: []string{"192.0.2.123"}, AllowCIDRs: []string{"192.0.2.0/24"}})
				}
				resourceName := router.Spec.Resources[0].Metadata.Name
				effects, failReload := 0, false
				command := func(_ context.Context, name string, args ...string) ([]byte, error) {
					if name == "systemctl" && len(args) > 0 && (args[0] == "is-enabled" || args[0] == "is-active") {
						return nil, errors.New("inactive test provider")
					}
					if strings.Contains(strings.Join(args, " "), "restart") {
						effects++
						if failReload {
							return nil, errors.New("injected provider reload failure")
						}
					}
					return nil, nil
				}
				reconcile := func() error {
					if kind == "NTPClient" {
						return (NTPClientController{Router: router, DeclaredRouter: router, Store: view, ConfigPath: path, Command: command}).Reconcile(t.Context())
					}
					return (NTPServerController{Router: router, DeclaredRouter: router, Store: view, ConfigPath: path, Command: command}).Reconcile(t.Context())
				}
				if err := reconcile(); err != nil {
					t.Fatal(err)
				}
				router = &api.Router{}
				if change == "receipt changed before absent retry" {
					failReload = true
					if err := reconcile(); err == nil {
						t.Fatal("withdrawal did not retain the injected reload failure")
					}
					if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("fixture did not reach pending absent withdrawal: %v", err)
					}
					failReload = false
				}
				effects = 0
				injected := false
				foreign := []byte("# independent operator configuration\nserver 192.0.2.200 iburst\n")
				var expected *ntpConfigRecord
				wrapped := &ntpReviewReadStore{SQLiteStore: store, ownerKey: "_" + kind + "-configs"}
				wrapped.after = func() {
					current, err := owner.get(resourceName)
					if err != nil {
						t.Fatal(err)
					}
					if current == nil || !current.Withdrawing {
						return
					}
					wrapped.after = nil
					injected = true
					expected = current
					if change == "receipt changed before absent retry" {
						next := *current
						next.ReloadPending = !current.ReloadPending
						if err := owner.save(resourceName, current, &next); err != nil {
							t.Fatal(err)
						}
						expected = &next
					} else {
						replacement := path + ".operator"
						if err := os.WriteFile(replacement, foreign, 0600); err != nil {
							t.Fatal(err)
						}
						if err := os.Rename(replacement, path); err != nil {
							t.Fatal(err)
						}
					}
				}
				view = wrapped
				reconcileErr := reconcile()
				actual, readErr := owner.get(resourceName)
				data, fileErr := os.ReadFile(path)
				filePreserved := errors.Is(fileErr, os.ErrNotExist)
				if change == "file replaced during receipt check" {
					filePreserved = fileErr == nil && string(data) == string(foreign)
				}
				if !injected || reconcileErr == nil || effects != 0 || !filePreserved || readErr != nil || actual == nil || expected == nil || *actual != *expected {
					t.Fatalf("stale withdrawal crossed ownership boundary: injected=%v err=%v serviceEffects=%d file=%q fileErr=%v actual=%+v expected=%+v readErr=%v", injected, reconcileErr, effects, data, fileErr, actual, expected, readErr)
				}
			})
		}
	}
}

func TestNTPReviewCanceledCreationCannotWriteAfterReceiptWait(t *testing.T) {
	for _, kind := range []string{"NTPClient", "NTPServer"} {
		t.Run(kind, func(t *testing.T) {
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			owner := ntpConfigOwner{store: store, kind: kind}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			path := filepath.Join(t.TempDir(), "routerd.conf")
			router := ntpRouter(api.NTPClientSpec{Provider: "chrony", Managed: true, Servers: []string{"192.0.2.123"}})
			if kind == "NTPServer" {
				router = ntpServerRouter(api.NTPServerSpec{Provider: "chrony", Managed: true, Servers: []string{"192.0.2.123"}, AllowCIDRs: []string{"192.0.2.0/24"}})
			}
			name := router.Spec.Resources[0].Metadata.Name
			injected := false
			wrapped := &ntpReviewReadStore{SQLiteStore: store, ownerKey: "_" + kind + "-configs"}
			wrapped.after = func() {
				receipt, err := owner.get(name)
				if err != nil {
					t.Fatal(err)
				}
				if receipt != nil && receipt.NextDigest != "" && receipt.Digest == "" {
					wrapped.after = nil
					injected = true
					cancel()
				}
			}
			commandCalls := 0
			command := func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
				commandCalls++
				return nil, ctx.Err()
			}
			var reconcileErr error
			if kind == "NTPClient" {
				reconcileErr = (NTPClientController{Router: router, DeclaredRouter: router, Store: wrapped, ConfigPath: path, Command: command}).Reconcile(ctx)
			} else {
				reconcileErr = (NTPServerController{Router: router, DeclaredRouter: router, Store: wrapped, ConfigPath: path, Command: command}).Reconcile(ctx)
			}
			data, fileErr := os.ReadFile(path)
			receipt, readErr := owner.get(name)
			if !injected || !errors.Is(reconcileErr, context.Canceled) || !errors.Is(fileErr, os.ErrNotExist) || commandCalls != 0 || readErr != nil || receipt == nil || receipt.Digest != "" || receipt.NextDigest == "" {
				t.Fatalf("cancelled creation wrote after storage wait: injected=%v err=%v file=%q fileErr=%v commands=%d receipt=%+v readErr=%v", injected, reconcileErr, data, fileErr, commandCalls, receipt, readErr)
			}
		})
	}
}
