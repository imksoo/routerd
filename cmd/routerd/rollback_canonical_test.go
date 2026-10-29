// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestRollbackCommitsCanonicalAndRetainsApplySource(t *testing.T) {
	_, isolated := faultApplyFixture(t)
	originalDefaults := platformDefaults
	platformDefaults.SysconfDir = filepath.Join(t.TempDir(), "routerd")
	t.Cleanup(func() { platformDefaults = originalDefaults })
	storedYAML := strings.Replace(testRouterYAML("rollback-selected"), "resources: []", "resources:\n    - apiVersion: net.routerd.net/v1alpha1\n      kind: Interface\n      metadata:\n        name: rollback-loopback\n      spec:\n        ifname: lo\n        managed: false\n        owner: external", 1)
	store, err := routerstate.OpenSQLite(isolated.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	generation := seedGeneration(t, store, "selected", storedYAML, true, "Applied")
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
	flags := registerApplyFlags(fs, true, false)
	if err := fs.Parse([]string{"--config", isolated.ConfigPath, "--state-file", isolated.StatePath, "--ledger-file", isolated.LedgerPath, "--status-file", isolated.StatusFile, "--skip-service-manager"}); err != nil {
		t.Fatal(err)
	}
	router, opts, err := loadRollbackApply(isolated.StatePath, generation, flags)
	if err != nil {
		t.Fatal(err)
	}
	// The existing per-attempt collaborator excludes host controllers while
	// preserving the real apply, generation and canonical commit operations.
	opts.attemptHooks = isolated.attemptHooks
	result, err := runApplyChainOnce(context.Background(), router, opts, io.Discard, nil)
	if err != nil {
		t.Fatalf("rollback apply: %v", err)
	}
	canonical, err := config.Load(isolated.ConfigPath)
	if err != nil || canonical.Metadata.Name != "rollback-selected" {
		t.Fatalf("canonical rollback lost: router=%+v err=%v", canonical, err)
	}
	store, err = routerstate.OpenSQLiteReadOnly(isolated.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if path := store.ObjectApplySource(api.NetAPIVersion, "Interface", "rollback-loopback"); path != isolated.ConfigPath {
		t.Fatalf("apply source points away from canonical: %q", path)
	}
	if _, err := os.Stat(isolated.ConfigPath); err != nil {
		t.Fatalf("canonical file disappeared after rollback: %v", err)
	}
	rows, err := store.ListGenerations(1)
	if err != nil || len(rows) != 1 || rows[0].Generation != result.Generation || !configCommitPhase(rows[0].Phase) {
		t.Fatalf("rollback terminal generation: rows=%+v err=%v", rows, err)
	}
}
