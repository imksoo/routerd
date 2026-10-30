// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/logstore"
	"gopkg.in/yaml.v3"
)

func TestCanonicalBarrierRejectedCommitDoesNotRunCandidateEffects(t *testing.T) {
	_, opts := faultApplyFixture(t)
	router, encoded, queryPath := barrierRetentionCandidate(t)
	opts.ConfigYAMLOverride = encoded
	failure := errors.New("canonical rename rejected")
	opts.attemptHooks.writeCanonical = func(string, []byte) (config.AtomicWriteOutcome, error) { return config.AtomicWriteOutcome{}, failure }
	outcome, err := runApplyChainOnceWithOutcome(context.Background(), router, opts, io.Discard, nil)
	if !errors.Is(err, failure) || outcome.Canonical != canonicalNotReplaced {
		t.Fatalf("wrong failure outcome: %+v %v", outcome, err)
	}
	db, err := sql.Open("sqlite", queryPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM dns_queries WHERE question_name='sentinel.example'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("rejected canonical commit executed actual candidate log-retention deletion")
	}
	if outcome.Stage != "canonical-write" {
		t.Fatalf("effect stage entered before canonical failure: %q", outcome.Stage)
	}
}

func barrierRetentionCandidate(t *testing.T) (*api.Router, string, string) {
	t.Helper()
	queryPath := filepath.Join(t.TempDir(), "queries.db")
	log, err := logstore.OpenDNSQueryLog(queryPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Record(t.Context(), logstore.DNSQuery{Timestamp: time.Now().Add(-48 * time.Hour), ClientAddress: "192.0.2.10", QuestionName: "sentinel.example", QuestionType: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	candidate, err := config.LoadBytes([]byte(dnsRuntimeYAML("127.0.0.1")), "candidate")
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := candidate.Spec.Resources[0].DNSResolverSpec()
	if err != nil {
		t.Fatal(err)
	}
	resolver.QueryLog = api.DNSResolverQueryLogSpec{Enabled: true, Path: queryPath}
	candidate.Spec.Resources[0].Spec = resolver
	candidate.Spec.Resources = append(candidate.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogRetention"}, Metadata: api.ObjectMeta{Name: "candidate-only"}, Spec: api.LogRetentionSpec{Signals: []string{"dnsQueries"}, Retention: "24h"}})
	data, err := yaml.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	return candidate, string(data), queryPath
}

func TestCanonicalBarrierRequiresDurabilityAcknowledgement(t *testing.T) {
	router, opts := faultApplyFixture(t)
	opts.attemptHooks.writeCanonical = func(string, []byte) (config.AtomicWriteOutcome, error) {
		return config.AtomicWriteOutcome{Replaced: true}, nil
	}
	outcome, err := runApplyChainOnceWithOutcome(context.Background(), router, opts, io.Discard, nil)
	if err == nil || outcome.DurabilityConfirmed || !strings.Contains(err.Error(), "apply failed") {
		t.Fatalf("unconfirmed canonical durability authorized apply: %+v %v", outcome, err)
	}
}

func TestCanonicalBarrierLiveReloadCannotPublishRejectedCandidateEffects(t *testing.T) {
	for _, mode := range []string{"rejected", "directory-sync", "durable"} {
		t.Run(mode, func(t *testing.T) {
			_, opts := faultApplyFixture(t)
			opts.Sandbox = true
			_, encoded, queryPath := barrierRetentionCandidate(t)
			current, err := config.Load(opts.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			initial := current
			store := faultGenerationStore(t, opts.StatePath, false)
			gate := &sync.RWMutex{}
			opts.MutationGate = gate
			runner := &controllerchain.Runner{Router: current, Store: store, Bus: bus.NewWithStore(store), Opts: controllerchain.Options{EnabledControllers: []string{"log-retention"}, MutationGate: gate}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if err := runner.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer func() {
				cancel()
				waitCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				if err := runner.Wait(waitCtx); err != nil && !errors.Is(err, context.Canceled) {
					t.Error(err)
				}
			}()
			failure := errors.New("candidate canonical write rejected")
			if mode != "durable" {
				opts.attemptHooks.writeCanonical = func(path string, data []byte) (config.AtomicWriteOutcome, error) {
					if mode == "rejected" {
						return config.AtomicWriteOutcome{}, failure
					}
					outcome, err := config.AtomicWriteFileWithOutcome(path, data)
					if err != nil {
						return outcome, err
					}
					outcome.DurabilityConfirmed = false
					return outcome, failure
				}
			}
			mutator := serveConfigMutator{configPath: opts.ConfigPath, statePath: opts.StatePath, baseOpts: opts, cache: &resultCache{}, getRouter: func() *api.Router { return current }, setRouter: func(next *api.Router) { current = next }, reloadWithOutcome: runner.ReloadRuntimeWithOutcome, activeRuntime: runner.ActiveRuntimeSnapshot, updateRuntime: runner.UpdateRuntimeRouter}
			result, err := mutator.apply(nil, controlapi.ApplyRequest{CandidateYAML: encoded, Replace: true})
			if mode == "durable" {
				if err != nil || result == nil {
					t.Fatalf("durable apply failed: %v", err)
				}
			} else {
				if !errors.Is(err, failure) || result != nil {
					t.Fatalf("commit rejection hidden: %v", err)
				}
			}
			db, err := sql.Open("sqlite", queryPath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var count int
			if err := db.QueryRow(`SELECT COUNT(*) FROM dns_queries WHERE question_name='sentinel.example'`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if mode == "durable" {
				if count != 0 || current == initial || runner.ActiveRuntimeSnapshot().Router == initial {
					t.Fatalf("durable candidate did not own actual retention effect/runtime: count=%d", count)
				}
			} else {
				if count != 1 || current != initial || runner.ActiveRuntimeSnapshot().Router != initial {
					t.Fatalf("rejected candidate activated actual worker: count=%d currentChanged=%t", count, current != initial)
				}
			}
			canonical, err := os.ReadFile(opts.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "rejected" && !strings.Contains(string(canonical), "old-router") {
				t.Fatal("rejected rename replaced canonical")
			}
		})
	}
}
