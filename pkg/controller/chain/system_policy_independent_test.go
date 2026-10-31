//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestReviewDPKGSelectionCannotTurnInstalledPackageIntoMissing(t *testing.T) {
	dpkg, err := exec.LookPath("dpkg-query")
	if err != nil {
		t.Skip("requires the actual dpkg-query")
	}
	for _, selection := range []string{"install", "hold"} {
		t.Run(selection, func(t *testing.T) {
			directory := t.TempDir()
			status := `Package: routerd-private-installed-review
Status: ` + selection + ` ok installed
Architecture: all
Version: 1
Maintainer: Routerd Test <test@example.invalid>
Description: private installed package review
`
			path := filepath.Join(directory, "status")
			if err := os.WriteFile(path, []byte(status), 0600); err != nil {
				t.Fatal(err)
			}
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			installAttempts := 0
			controller := PackageController{
				Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
					TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Package"}, Metadata: api.ObjectMeta{Name: "installed-review"},
					Spec: api.PackageSpec{Packages: []api.OSPackageSetSpec{{OS: "debian", Manager: "apt", Names: []string{"routerd-private-installed-review"}}}},
				}}}},
				Store: store, OSName: "debian",
				Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
					if name == "dpkg-query" {
						actual := append([]string{"--admindir=" + directory}, args...)
						output, err := exec.CommandContext(ctx, dpkg, actual...).CombinedOutput()
						t.Logf("actual dpkg-query: output=%q err=%v", output, err)
						return output, err
					}
					// A mistaken install decision is observed at the existing boundary.
					// Never dispatch apt, sudo, or any real package database mutation.
					installAttempts++
					return nil, errors.New("installation boundary was not authorized by an installed package observation")
				},
			}
			reconcileErr := controller.Reconcile(t.Context())
			actual, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			published, err := store.ReadObjectStatus(api.SystemAPIVersion, "Package", "installed-review")
			if err != nil {
				t.Fatal(err)
			}
			if reconcileErr != nil || installAttempts != 0 || published["phase"] != "Applied" || published["changed"] != false || string(actual) != status {
				t.Fatalf("installed package selection triggered mutation: selection=%s installs=%d err=%v status=%+v privateDBUnchanged=%v", selection, installAttempts, reconcileErr, published, string(actual) == status)
			}
		})
	}
}

func TestReviewOptionalSystemPathsCannotHideRequiredStateWriteFailure(t *testing.T) {
	for _, kind := range []string{"Sysctl", "Package"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			store, err := routerstate.OpenSQLite(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			fault, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer fault.Close()
			if _, err := fault.Exec("CREATE TRIGGER reject_system_status BEFORE INSERT ON objects BEGIN SELECT RAISE(ABORT,'fixture required system status failure'); END"); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "Sysctl":
				sysctl, lookupErr := exec.LookPath("sysctl")
				if lookupErr != nil {
					t.Skip("requires the actual read-only sysctl")
				}
				controller := SysctlController{
					Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
						TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"}, Metadata: api.ObjectMeta{Name: "optional-review"},
						Spec: api.SysctlSpec{Key: "routerd.review.unavailable", Value: "1", Optional: true},
					}}}},
					Store: store,
					Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
						if name != "sysctl" || len(args) != 2 || args[0] != "-n" || args[1] != "routerd.review.unavailable" {
							t.Fatalf("unexpected native operation: %s %v", name, args)
						}
						out, err := exec.CommandContext(ctx, sysctl, args...).CombinedOutput()
						if err == nil {
							t.Fatal("private missing-key observation unexpectedly succeeded")
						}
						return out, err
					},
				}
				err = controller.Reconcile(t.Context())
			case "Package":
				controller := PackageController{
					Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
						TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Package"}, Metadata: api.ObjectMeta{Name: "unsupported-review"},
						Spec: api.PackageSpec{Packages: []api.OSPackageSetSpec{{OS: "freebsd", Manager: "pkg", Names: []string{"private-unreached"}}}},
					}}}},
					Store: store, OSName: "debian",
					Command: func(context.Context, string, ...string) ([]byte, error) {
						t.Fatal("missing OS selection dispatched a command")
						return nil, nil
					},
				}
				err = controller.Reconcile(t.Context())
			}
			if rows, readErr := store.ListObjectStatuses(); readErr != nil || len(rows) != 0 {
				t.Fatalf("private SQLite read boundary failed: rows=%+v err=%v", rows, readErr)
			}
			if err == nil || !strings.Contains(err.Error(), "fixture required system status failure") {
				t.Fatalf("required state write failure was hidden: kind=%s err=%v", kind, err)
			}
		})
	}
}
