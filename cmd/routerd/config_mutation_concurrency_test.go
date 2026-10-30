// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
)

func TestConfigMutationConcurrentPartialUpdatesPreserveEarlierCommit(t *testing.T) {
	for _, noReconcile := range []bool{true, false} {
		for _, operations := range [][2]string{{"upsert-alpha", "upsert-beta"}, {"upsert-alpha", "delete"}, {"delete", "upsert-beta"}} {
			t.Run(fmt.Sprintf("noReconcile=%t/%s/%s", noReconcile, operations[0], operations[1]), func(t *testing.T) {
				m := concurrentMutationFixture(t)
				writing := make(chan struct{})
				release := make(chan struct{})
				var releaseOnce, writeOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				defer unblock()
				m.baseOpts.attemptHooks = &applyAttemptHooks{writeCanonical: func(path string, data []byte) (config.AtomicWriteOutcome, error) {
					writeOnce.Do(func() {
						close(writing)
						<-release
					})
					return config.AtomicWriteFileWithOutcome(path, data)
				}}
				first := make(chan error, 1)
				go func() { first <- runConcurrentMutation(m, operations[0], noReconcile, false) }()
				awaitMutationSignal(t, writing, first)

				// Admission is checked once on entry and again immediately before
				// the gate is acquired. Wait until the second request is queued while
				// the first still owns the transaction and has not renamed its file.
				queued := make(chan struct{})
				secondMutator := m
				checks := 0
				secondMutator.runtimeMutationError = func() error {
					checks++
					if checks == 2 {
						close(queued)
					}
					return nil
				}
				second := make(chan error, 1)
				go func() { second <- runConcurrentMutation(secondMutator, operations[1], noReconcile, false) }()
				awaitMutationSignal(t, queued, second)
				unblock()
				awaitMutationResult(t, first)
				awaitMutationResult(t, second)

				router, err := config.Load(m.configPath)
				if err != nil {
					t.Fatal(err)
				}
				got := map[string]string{}
				for _, resource := range router.Spec.Resources {
					spec, err := resource.HostnameSpec()
					if err != nil {
						t.Fatal(err)
					}
					got[resource.Metadata.Name] = spec.Hostname
				}
				want := map[string]string{}
				if operations[0] != "delete" && operations[1] != "delete" {
					want["original"] = "original.example"
				}
				for _, operation := range operations {
					switch operation {
					case "upsert-alpha":
						want["alpha"] = "alpha.example"
					case "upsert-beta":
						want["beta"] = "beta.example"
					}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("committed resources = %v, want %v", got, want)
				}
			})
		}
	}
}

func TestConfigMutationDryRunDoesNotAcquireMutationGate(t *testing.T) {
	for _, operation := range []string{"upsert-alpha", "delete"} {
		t.Run(operation, func(t *testing.T) {
			m := concurrentMutationFixture(t)
			before, err := os.ReadFile(m.configPath)
			if err != nil {
				t.Fatal(err)
			}
			m.baseOpts.MutationGate.Lock()
			defer m.baseOpts.MutationGate.Unlock()
			result := make(chan error, 1)
			go func() { result <- runConcurrentMutation(m, operation, false, true) }()
			awaitMutationResult(t, result)
			after, err := os.ReadFile(m.configPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("dry run changed canonical configuration")
			}
			if _, err := os.Stat(m.statePath); !os.IsNotExist(err) {
				t.Fatalf("dry run created state database: %v", err)
			}
		})
	}
}

func concurrentMutationFixture(t *testing.T) serveConfigMutator {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	statePath := filepath.Join(dir, "routerd.db")
	input := `apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: concurrent-updates}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Hostname
      metadata: {name: original}
      spec: {hostname: original.example}
`
	if err := os.WriteFile(configPath, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	current, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	return serveConfigMutator{
		configPath: configPath,
		statePath:  statePath,
		baseOpts: applyOptions{
			ConfigPath: configPath, StatePath: statePath,
			LedgerPath: filepath.Join(dir, "ledger.db"),
			Sandbox:    true, SkipServiceManager: true, MutationGate: &sync.RWMutex{},
		},
		cache:                   &resultCache{},
		applyRuntimeWithOutcome: testCommitRuntime(func() *api.Router { return current }),
		getRouter:               func() *api.Router { return current },
		setRouter:               func(next *api.Router) { current = next },
	}
}

func runConcurrentMutation(m serveConfigMutator, operation string, noReconcile, dryRun bool) error {
	if operation == "delete" {
		_, err := m.delete(nil, controlapi.DeleteRequest{Target: "Hostname/original", NoReconcile: noReconcile, DryRun: dryRun})
		return err
	}
	name := "alpha"
	if operation == "upsert-beta" {
		name = "beta"
	}
	candidate := fmt.Sprintf(`apiVersion: routerd.net/v1alpha1
kind: Router
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Hostname
      metadata: {name: %s}
      spec: {hostname: %s.example}
`, name, name)
	_, err := m.apply(nil, controlapi.ApplyRequest{CandidateYAML: candidate, NoReconcile: noReconcile, DryRun: dryRun})
	return err
}

func awaitMutationSignal(t *testing.T, signal <-chan struct{}, result <-chan error) {
	t.Helper()
	select {
	case <-signal:
	case err := <-result:
		t.Fatalf("mutation returned before reaching transaction barrier: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for mutation transaction")
	}
}

func awaitMutationResult(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for mutation result")
	}
}
