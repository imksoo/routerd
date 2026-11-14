// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/config"
	routerstate "github.com/imksoo/routerd/pkg/state"
	statuswriter "github.com/imksoo/routerd/pkg/status"
)

// Reopen the actual database before observing: the in-memory store generation
// and the result cache must not carry the successful attempt across restart.
func TestInitialGenerationRestart(t *testing.T) {
	const source = "apiVersion: routerd.net/v1alpha1\nkind: Router\nmetadata:\n  name: restart\nspec:\n  resources: []\n"
	const changedSource = "apiVersion: routerd.net/v1alpha1\nkind: Router\nmetadata:\n  name: changed\nspec:\n  resources: []\n"
	for _, tc := range []struct {
		pdClientDefault  bool
		name             string
		errorKind        string
		phase            string
		latest           string
		changedSource    bool
		changedEffective bool
		noYAML           bool
		empty            bool
		existing         bool
		restore          bool
	}{
		{name: "healthy", phase: "Healthy", restore: true},
		{name: "applied", phase: "Applied", restore: true},
		{name: "default-pd-client", phase: "Healthy", pdClientDefault: true, restore: true},
		{name: "latest-error", phase: "Healthy", latest: "Error"},
		{name: "latest-committed", phase: "Healthy", latest: "Committed"},
		{name: "latest-unfinished", phase: "Healthy", latest: "unfinished"},
		{name: "changed-source", phase: "Healthy", changedSource: true},
		{name: "changed-effective", phase: "Healthy", changedEffective: true},
		{name: "missing-yaml", phase: "Healthy", noYAML: true},
		{name: "empty", empty: true},
		{name: "existing-generation", phase: "Healthy", existing: true},
		{name: "database-error", phase: "Healthy", errorKind: "database"},
		{name: "configuration-error", phase: "Healthy", errorKind: "configuration"},
		{name: "hash-error", phase: "Healthy", errorKind: "hash"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := source
			if tc.pdClientDefault {
				source = `apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: restart
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: eth0
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv6PrefixDelegation
      metadata:
        name: wan-pd
      spec:
        interface: wan
`
			}
			dir := t.TempDir()
			configPath := filepath.Join(dir, "router.yaml")
			if err := os.WriteFile(configPath, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			router, err := config.Load(configPath)
			if err != nil {
				t.Fatal(err)
			}
			dbPath := filepath.Join(dir, "state.db")
			store, err := routerstate.OpenSQLite(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = store.Close() }()
			var successful int64
			if !tc.empty {
				effective, _, err := routerWithIPv6PDClientOptions(router, string(platformDefaults.OS))
				if err != nil {
					t.Fatal(err)
				}
				hash, err := configGenerationHash(source, effective)
				if err != nil {
					t.Fatal(err)
				}
				successful, err = store.BeginGeneration(hash)
				if err != nil {
					t.Fatal(err)
				}
				if !tc.noYAML {
					if err := store.RecordGenerationConfig(successful, source); err != nil {
						t.Fatal(err)
					}
				}
				if err := store.FinishGeneration(successful, tc.phase, nil); err != nil {
					t.Fatal(err)
				}
				if tc.latest != "" {
					newer, err := store.BeginGeneration(hash)
					if err != nil {
						t.Fatal(err)
					}
					if err := store.RecordGenerationConfig(newer, source); err != nil {
						t.Fatal(err)
					}
					if tc.latest != "unfinished" {
						if err := store.FinishGeneration(newer, tc.latest, nil); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = routerstate.OpenSQLite(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			if tc.changedSource {
				if err := os.WriteFile(configPath, []byte(changedSource), 0600); err != nil {
					t.Fatal(err)
				}
				router, err = config.Load(configPath)
				if err != nil {
					t.Fatal(err)
				}
			}
			opts := applyOptions{ConfigPath: configPath}
			if tc.changedEffective {
				// The same YAML with different effective apply settings must not match.
				opts = applyOptions{ConfigYAMLOverride: source}
				router.Spec.Apply.Mode = "progressive"
			}
			engine := apply.New()
			engine.Store = store
			observed, err := engine.ObserveEffective(filterRouterByWhen(router, store), router)
			if err != nil {
				t.Fatal(err)
			}
			if observed.Generation != 0 {
				t.Fatalf("fresh observation already has generation %d", observed.Generation)
			}
			want := int64(0)
			if tc.restore {
				want = successful
			}
			if tc.existing {
				observed.Generation = 789
				want = 789
			}
			switch tc.errorKind {
			case "database":
				db, err := sql.Open("sqlite", dbPath)
				if err != nil {
					t.Fatal(err)
				}
				_, execErr := db.Exec("ALTER TABLE generations RENAME TO unavailable_generations")
				closeErr := db.Close()
				if execErr != nil {
					t.Fatal(execErr)
				}
				if closeErr != nil {
					t.Fatal(closeErr)
				}
			case "configuration":
				opts.ConfigPath = dir // Reading a directory is a real configuration I/O error.
			case "hash":
				opts = applyOptions{ConfigYAMLOverride: "invalid: ["}
			}
			initialGeneration := observed.Generation
			result, err := restoreInitialGeneration(observed, store, router, opts)
			if tc.errorKind != "" {
				if err == nil {
					t.Fatalf("%s error was hidden", tc.errorKind)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if observed.Generation != initialGeneration {
				t.Fatal("restoration mutated original observation")
			}
			cache := &resultCache{}
			cache.Store(result)
			statusPath := filepath.Join(dir, "status.json")
			if err := statuswriter.Write(statusPath, result); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(statusPath)
			if err != nil {
				t.Fatal(err)
			}
			var status apply.Result
			if err := json.Unmarshal(data, &status); err != nil {
				t.Fatal(err)
			}
			if status.Generation != want || cache.Load().Generation != want {
				t.Fatalf("restart status/cache generations = %d/%d, want %d", status.Generation, cache.Load().Generation, want)
			}
			if status.Phase != observed.Phase {
				t.Fatalf("restoration changed observed phase to %s", status.Phase)
			}
			next, err := engine.ObserveEffective(filterRouterByWhen(router, store), router)
			if err != nil {
				t.Fatal(err)
			}
			cache.Store(next)
			if cache.Load().Generation != want {
				t.Fatalf("later observation lost generation: got %d, want %d", cache.Load().Generation, want)
			}
		})
	}
}
