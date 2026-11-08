// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/ingressdrain"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestDrainAndUndrainIngressBackend(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "routerd.db")
	var out bytes.Buffer
	if err := run([]string{"ingress", "drain", "ingress/kubernetes-api", "backend=cp-01", "--duration=10m", "--state-file", statePath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("ingress drain: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `"service": "kubernetes-api"`) || !strings.Contains(got, `"backend": "cp-01"`) || !strings.Contains(got, `"drainedUntil"`) {
		t.Fatalf("drain output = %s", got)
	}
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	if state, ok := ingressdrain.Current(store, "kubernetes-api", "cp-01"); !ok || state.DrainedUntil == "" {
		t.Fatalf("drain state = %#v ok=%v", state, ok)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close state: %v", err)
	}

	out.Reset()
	if err := run([]string{"ingress", "undrain", "ingress/kubernetes-api", "--backend", "cp-01", "--state-file", statePath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("ingress undrain: %v", err)
	}
	store, err = routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("reopen state: %v", err)
	}
	defer store.Close()
	if _, ok := ingressdrain.Current(store, "kubernetes-api", "cp-01"); ok {
		t.Fatalf("drain state still present")
	}
}

func TestDynamicListCommandShowsActiveAndExpired(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	now := time.Now().UTC()
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:         "cloudedge",
		Generation:     1,
		ObservedAt:     now.Add(-2 * time.Hour),
		ExpiresAt:      now.Add(time.Hour),
		Digest:         "sha256:active",
		ResourcesJSON:  `[{"apiVersion":"net.routerd.net/v1alpha1","kind":"Interface","metadata":{"name":"wan"},"spec":{}}]`,
		DirectivesJSON: `[]`,
		Status:         "active",
	}); err != nil {
		t.Fatalf("upsert active dynamic part: %v", err)
	}
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:         "cloudedge",
		Generation:     2,
		ObservedAt:     now.Add(-time.Hour),
		ExpiresAt:      now.Add(-time.Hour),
		Digest:         "sha256:expired",
		ResourcesJSON:  `[]`,
		DirectivesJSON: `[{"op":"mask","target":{"apiVersion":"net.routerd.net/v1alpha1","kind":"Interface","name":"wan"},"reason":"test"}]`,
		Status:         "active",
	}); err != nil {
		t.Fatalf("upsert expired dynamic part: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close state: %v", err)
	}

	var out bytes.Buffer
	if err := run([]string{"dynamic", "list", "--state-file", statePath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("dynamic list: %v", err)
	}
	got := out.String()
	for _, want := range []string{"SOURCE", "GEN", "STATUS", "RESOURCES", "DIRECTIVES", "EXPIRES", "cloudedge"} {
		if !strings.Contains(got, want) {
			t.Fatalf("dynamic list output missing %q:\n%s", want, got)
		}
	}
	for _, want := range []string{"active", "expired", "1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("dynamic list output missing %q:\n%s", want, got)
		}
	}

	out.Reset()
	if err := run([]string{"dynamic", "describe", "cloudedge", "--state-file", statePath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("dynamic describe: %v", err)
	}
	got = out.String()
	for _, want := range []string{"Generation:", "Status:", "expired", "sha256:expired", "Directives:", "mask", "net.routerd.net/v1alpha1/Interface/wan"} {
		if !strings.Contains(got, want) {
			t.Fatalf("dynamic describe output missing %q:\n%s", want, got)
		}
	}
}

func TestDynamicRenderCommandMergesStateDB(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	if err := os.WriteFile(configPath, []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: ens18
        managed: true
        owner: routerd
    - apiVersion: config.routerd.net/v1alpha1
      kind: DynamicOverridePolicy
      metadata:
        name: cloudedge
      spec:
        allow:
          - source: cloudedge
            operations:
              - mask
            targets:
              - apiVersion: net.routerd.net/v1alpha1
                kind: Interface
                name: wan
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	statePath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	now := time.Now().UTC()
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:     "cloudedge",
		Generation: 7,
		ObservedAt: now.Add(-time.Minute),
		ExpiresAt:  now.Add(time.Hour),
		Digest:     "sha256:render",
		ResourcesJSON: `[{
			"apiVersion":"net.routerd.net/v1alpha1",
			"kind":"Interface",
			"metadata":{"name":"wan-dynamic"},
			"spec":{"ifname":"ens19","managed":true,"owner":"routerd"}
		}]`,
		DirectivesJSON: `[{
			"op":"mask",
			"target":{"apiVersion":"net.routerd.net/v1alpha1","kind":"Interface","name":"wan"},
			"reason":"cloud edge replacement"
		}]`,
		Status: "active",
	}); err != nil {
		t.Fatalf("upsert dynamic part: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close state: %v", err)
	}

	var out bytes.Buffer
	if err := run([]string{"dynamic", "render", "--config", configPath, "--state-file", statePath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("dynamic render: %v", err)
	}
	got := out.String()
	for _, want := range []string{"kind: Interface", "name: wan-dynamic", "ifname: ens19"} {
		if !strings.Contains(got, want) {
			t.Fatalf("dynamic render output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ifname: ens18") {
		t.Fatalf("dynamic render output still contains suppressed startup interface:\n%s", got)
	}
}

func TestLedgerIntegrityCheckCommand(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close state: %v", err)
	}

	var out bytes.Buffer
	if err := run([]string{"ledger", "integrity-check", "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("ledger integrity-check: %v", err)
	}
	if !strings.Contains(out.String(), `"result": "ok"`) {
		t.Fatalf("integrity output = %s", out.String())
	}
}

func TestLedgerPruneEventsDryRunCommand(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	if err := store.RecordEvent("net.routerd.net/v1alpha1", "Interface", "wan", "Normal", "OldEvent", "old event"); err != nil {
		t.Fatalf("record old event: %v", err)
	}
	if err := store.RecordEvent("net.routerd.net/v1alpha1", "Interface", "wan", "Normal", "NewEvent", "new event"); err != nil {
		t.Fatalf("record new event: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close state: %v", err)
	}
	db, err := sql.Open("sqlite", statePath)
	if err != nil {
		t.Fatalf("open sqlite directly: %v", err)
	}
	_, err = db.Exec(`UPDATE events SET created_at = ? WHERE reason = ?`, time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339Nano), "OldEvent")
	if err != nil {
		t.Fatalf("backdate old event: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite directly: %v", err)
	}

	var out bytes.Buffer
	if err := run([]string{"ledger", "prune-events", "--state-file", statePath, "--older-than", "24h", "--dry-run"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("ledger prune-events --dry-run: %v", err)
	}
	got := out.String()
	fields := strings.Join(strings.Fields(got), "|")
	if !strings.Contains(got, "MATCHED") || !strings.Contains(fields, "|1|0|true") {
		t.Fatalf("prune dry-run output = %s", got)
	}
	store, err = routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("reopen state: %v", err)
	}
	defer store.Close()
	events := store.Events("net.routerd.net/v1alpha1", "Interface", "wan", 10)
	if len(events) != 2 {
		t.Fatalf("dry-run pruned events: %+v", events)
	}
}

func TestLedgerPruneEventsCommandRecordsAuditEvent(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	if err := store.RecordEvent("net.routerd.net/v1alpha1", "Interface", "wan", "Normal", "OldEvent", "old event"); err != nil {
		t.Fatalf("record old event: %v", err)
	}
	if err := store.RecordEvent("net.routerd.net/v1alpha1", "Interface", "wan", "Normal", "NewEvent", "new event"); err != nil {
		t.Fatalf("record new event: %v", err)
	}
	before, err := store.ListEvents(routerstate.EventQuery{Limit: 20})
	if err != nil {
		t.Fatalf("events before prune: %v", err)
	}
	var preserved routerstate.StoredEvent
	for _, record := range before {
		if record.Reason == "NewEvent" {
			preserved = record
		}
	}
	if preserved.Reason != "NewEvent" {
		t.Fatal("new event fixture missing")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close state: %v", err)
	}
	db, err := sql.Open("sqlite", statePath)
	if err != nil {
		t.Fatalf("open sqlite directly: %v", err)
	}
	_, err = db.Exec(`UPDATE events SET created_at = ? WHERE reason = ?`, time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339Nano), "OldEvent")
	if err != nil {
		t.Fatalf("backdate old event: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite directly: %v", err)
	}

	var out bytes.Buffer
	if err := run([]string{"ledger", "prune-events", "--state-file", statePath, "--older-than", "24h"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("ledger prune-events: %v", err)
	}
	got := out.String()
	fields := strings.Join(strings.Fields(got), "|")
	if !strings.Contains(got, "MATCHED") || !strings.Contains(fields, "|1|1|false") {
		t.Fatalf("prune output = %s", got)
	}
	store, err = routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("reopen state: %v", err)
	}
	defer store.Close()
	events, err := store.ListEvents(routerstate.EventQuery{Topic: "routerd.ledger.events.pruned", Limit: 10})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("audit events = %+v, want 1", events)
	}
	event := events[0]
	if event.Severity != "info" || event.Reason != "EventsPruned" {
		t.Fatalf("audit event metadata = %+v", event)
	}
	if got := fmt.Sprint(event.Attributes["deletedRows"]); got != "1" {
		t.Fatalf("deletedRows attribute = %q, want 1", got)
	}
	cutoff, ok := event.Attributes["cutoff"].(string)
	if !ok || cutoff == "" {
		t.Fatalf("cutoff attribute = %#v", event.Attributes["cutoff"])
	}
	if _, err := time.Parse(time.RFC3339Nano, cutoff); err != nil {
		t.Fatalf("cutoff attribute is not RFC3339Nano: %q: %v", cutoff, err)
	}
	if got := fmt.Sprint(event.Attributes["dryRun"]); got != "false" {
		t.Fatalf("dryRun attribute = %q, want false", got)
	}
	remaining, err := store.ListEvents(routerstate.EventQuery{Limit: 20})
	if err != nil {
		t.Fatalf("remaining events: %v", err)
	}
	reasons := map[string]int{}
	for _, record := range remaining {
		reasons[record.Reason]++
		if record.Reason == "NewEvent" && !reflect.DeepEqual(record, preserved) {
			t.Fatalf("new event changed: before=%#v after=%#v", preserved, record)
		}
	}
	want := map[string]int{"NewEvent": 1, "EventsPruned": 1}
	if !reflect.DeepEqual(reasons, want) {
		t.Fatalf("remaining exact event set = %#v, want %#v", reasons, want)
	}

}

func TestApplyRequiresInputAndExplainsMissingDaemon(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"apply"}, &out, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "requires -f") {
		t.Fatalf("apply without input error = %v", err)
	}

	candidatePath := filepath.Join(t.TempDir(), "candidate.yaml")
	if err := os.WriteFile(candidatePath, []byte(testRouterYAML("candidate-router")), 0644); err != nil {
		t.Fatalf("write candidate: %v", err)
	}
	err = run([]string{"apply", "--socket", filepath.Join(t.TempDir(), "missing.sock"), "-f", candidatePath, "--timeout", "10ms"}, &out, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "start routerd serve") {
		t.Fatalf("missing daemon error = %v", err)
	}
}

func testRouterYAML(name string) string {
	return fmt.Sprintf(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: %s
spec:
  resources: []
`, name)
}
