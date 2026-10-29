// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/ingressdrain"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/platform"
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
	if state, ok, err := ingressdrain.Current(store, "kubernetes-api", "cp-01"); err != nil || !ok || state.DrainedUntil == "" {
		t.Fatalf("drain state = %#v ok=%v err=%v", state, ok, err)
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
	if _, ok, err := ingressdrain.Current(store, "kubernetes-api", "cp-01"); err != nil || ok {
		t.Fatalf("drain state still present: err=%v", err)
	}
}

func TestRestartDNSResolverSelectsSingleResource(t *testing.T) {
	requireLinuxDoctorFixture(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	if err := os.WriteFile(configPath, []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSResolver
      metadata:
        name: lan-resolver
      spec:
        listen:
          - addresses: ["127.0.0.1"]
`), 0644); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "commands.log")
	for _, binary := range []string{"systemctl", "service"} {
		if err := os.WriteFile(filepath.Join(binDir, binary), []byte("#!/bin/sh\necho "+binary+" \"$@\" >> \""+logPath+"\"\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out bytes.Buffer
	if err := run([]string{"restart", "dns-resolver", "--config", configPath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("restart dns-resolver: %v", err)
	}
	if !strings.Contains(out.String(), "DNSResolver/lan-resolver") {
		t.Fatalf("output = %s", out.String())
	}
	commands, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	invocations := strings.Split(strings.TrimSpace(string(commands)), "\n")
	want := []string{"systemctl restart routerd-dns-resolver@lan-resolver.service"}
	if !reflect.DeepEqual(invocations, want) {
		t.Fatalf("selected restart invocations = %#v, want %#v", invocations, want)
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

func TestLogCommandsUseControlSocketByDefault(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "routerd.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer listener.Close()
	server := &http.Server{Handler: controlapi.Handler{
		Get: func(r *http.Request, req controlapi.GetRequest) (*controlapi.GetResult, error) {
			if req.Subject != "status" {
				t.Fatalf("subject = %q, want status", req.Subject)
			}
			result := controlapi.NewStatus(&apply.Result{Phase: "Healthy", Generation: 7}, nil)
			get := controlapi.NewGetResult("status")
			get.Status = &result.Status
			get.Raw = result
			return &get, nil
		},
		DNSQueries: func(r *http.Request, req controlapi.DNSQueriesRequest) (*controlapi.DNSQueries, error) {
			if req.Limit != 3 {
				t.Fatalf("dns limit = %d", req.Limit)
			}
			result := controlapi.NewDNSQueries([]logstore.DNSQuery{{Timestamp: time.Now(), ClientAddress: "172.18.0.10", QuestionName: "socket.example", QuestionType: "A", ResponseCode: "NOERROR"}})
			return &result, nil
		},
		TrafficFlows: func(r *http.Request, req controlapi.TrafficFlowsRequest) (*controlapi.TrafficFlows, error) {
			result := controlapi.NewTrafficFlows([]logstore.TrafficFlow{{StartedAt: time.Now(), ClientAddress: "172.18.0.10", PeerAddress: "1.1.1.1", PeerPort: 443, Protocol: "tcp"}})
			return &result, nil
		},
		FirewallLogs: func(r *http.Request, req controlapi.FirewallLogsRequest) (*controlapi.FirewallLogs, error) {
			result := controlapi.NewFirewallLogs([]logstore.FirewallLogEntry{{Timestamp: time.Now(), Action: "drop", SrcAddress: "172.18.0.10", DstAddress: "198.51.100.10", Protocol: "tcp", L3Proto: "ipv4", RuleName: "deny-test"}})
			return &result, nil
		},
		SetLogLevel: func(r *http.Request, req controlapi.LogLevelRequest) (*controlapi.LogLevelResult, error) {
			if req.Level != "debug" {
				t.Fatalf("level = %q, want debug", req.Level)
			}
			result := controlapi.NewLogLevelResult(req.Level)
			return &result, nil
		},
	}}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"get", "status", "--socket", socketPath, "-o", "json"}, `"phase": "Healthy"`},
		{[]string{"get", "status", "--socket", socketPath, "-o", "json"}, `"generation": 7`},
		{[]string{"get", "dns-queries", "--socket", socketPath, "--limit", "3"}, "socket.example"},
		{[]string{"get", "traffic-flows", "--socket", socketPath}, "1.1.1.1"},
		{[]string{"get", "firewall-logs", "--socket", socketPath}, "deny-test"},
		{[]string{"log-level", "--socket", socketPath, "debug"}, `"level": "debug"`},
	} {
		var out bytes.Buffer
		if err := run(tt.args, &out, &bytes.Buffer{}); err != nil {
			t.Fatalf("%v: %v", tt.args, err)
		}
		if !strings.Contains(out.String(), tt.want) {
			t.Fatalf("%v output missing %q:\n%s", tt.args, tt.want, out.String())
		}
	}
}

func TestConfigLifecycleCommandsUseControlAPI(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "routerd.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer listener.Close()
	var applied controlapi.ApplyRequest
	var planned controlapi.PlanRequest
	var validated controlapi.ValidateRequest
	var deleted controlapi.DeleteRequest
	server := &http.Server{Handler: controlapi.Handler{
		Validate: func(r *http.Request, req controlapi.ValidateRequest) (*controlapi.ValidateResult, error) {
			validated = req
			result := controlapi.NewValidateResult(true, nil, "")
			return &result, nil
		},
		Plan: func(r *http.Request, req controlapi.PlanRequest) (*controlapi.PlanResult, error) {
			planned = req
			result := controlapi.NewPlanResult(&apply.Result{Phase: "Healthy"})
			return &result, nil
		},
		Apply: func(r *http.Request, req controlapi.ApplyRequest) (*controlapi.ApplyResult, error) {
			applied = req
			result := controlapi.NewApplyResult(&apply.Result{Phase: "Healthy", Generation: 10})
			return &result, nil
		},
		Delete: func(r *http.Request, req controlapi.DeleteRequest) (*controlapi.DeleteResult, error) {
			deleted = req
			return &controlapi.DeleteResult{
				TypeMeta: controlapi.TypeMeta{APIVersion: controlapi.APIVersion, Kind: "DeleteResult"},
				Deleted:  []string{"net.routerd.net/v1alpha1/Hostname/appliance"},
				Result:   &apply.Result{Phase: "Healthy", Generation: 10},
			}, nil
		},
	}}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	candidatePath := filepath.Join(t.TempDir(), "candidate.yaml")
	candidateYAML := testRouterYAML("candidate-router")
	if err := os.WriteFile(candidatePath, []byte(candidateYAML), 0644); err != nil {
		t.Fatalf("write candidate: %v", err)
	}
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"validate", "--socket", socketPath, "-f", candidatePath}, `"valid": true`},
		{[]string{"plan", "--socket", socketPath, "-f", candidatePath, "--replace"}, `"kind": "PlanResult"`},
		{[]string{"apply", "--socket", socketPath, "-f", candidatePath, "--replace", "--no-reconcile"}, `"generation": 10`},
		{[]string{"delete", "--socket", socketPath, "--no-reconcile", "Hostname/appliance"}, "Hostname/appliance"},
	} {
		var out bytes.Buffer
		if err := run(tt.args, &out, &bytes.Buffer{}); err != nil {
			t.Fatalf("%v: %v", tt.args, err)
		}
		if !strings.Contains(out.String(), tt.want) {
			t.Fatalf("%v output missing %q:\n%s", tt.args, tt.want, out.String())
		}
	}
	if validated.CandidateYAML != candidateYAML {
		t.Fatalf("validate candidate = %q, want file data", validated.CandidateYAML)
	}
	if !planned.Replace || planned.CandidateYAML != candidateYAML {
		t.Fatalf("plan request = %+v", planned)
	}
	if !applied.Replace || !applied.NoReconcile || applied.CandidateYAML != candidateYAML {
		t.Fatalf("apply request = %+v", applied)
	}
	if !deleted.NoReconcile || deleted.Target != "Hostname/appliance" {
		t.Fatalf("delete request = %+v", deleted)
	}
}

func TestPlanCommandReportsReachedAPIErrorWithoutReachabilityClaim(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "routerd.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer listener.Close()
	server := &http.Server{Handler: controlapi.Handler{
		Plan: func(r *http.Request, req controlapi.PlanRequest) (*controlapi.PlanResult, error) {
			return nil, errors.New("routerd state database is not initialized; restart routerd serve and verify its --state path")
		},
	}}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	err = planCommand([]string{"--socket", socketPath}, io.Discard, strings.NewReader(""))
	if err == nil {
		t.Fatal("planCommand succeeded, want API error")
	}
	if !strings.Contains(err.Error(), "routerd plan failed: routerd state database is not initialized") {
		t.Fatalf("planCommand error is not actionable: %v", err)
	}
	if strings.Contains(err.Error(), "not reachable") {
		t.Fatalf("planCommand mislabeled reached API error: %v", err)
	}
}

func TestValidateCommandProcessExitCodes(t *testing.T) {
	bin := buildRouterctlBinary(t)
	candidatePath := filepath.Join(t.TempDir(), "candidate.yaml")
	if err := os.WriteFile(candidatePath, []byte(testRouterYAML("candidate-router")), 0644); err != nil {
		t.Fatalf("write candidate: %v", err)
	}
	socketPath := filepath.Join(t.TempDir(), "routerd.sock")
	validateResult := controlapi.NewValidateResult(true, nil, "")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	server := &http.Server{Handler: controlapi.Handler{
		Validate: func(r *http.Request, req controlapi.ValidateRequest) (*controlapi.ValidateResult, error) {
			result := validateResult
			return &result, nil
		},
	}}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	for _, tt := range []struct {
		name     string
		socket   string
		file     string
		result   controlapi.ValidateResult
		wantExit int
	}{
		{
			name:     "valid",
			socket:   socketPath,
			file:     candidatePath,
			result:   controlapi.NewValidateResult(true, nil, ""),
			wantExit: 0,
		},
		{
			name:     "invalid",
			socket:   socketPath,
			file:     candidatePath,
			result:   controlapi.NewValidateResult(false, nil, "invalid config"),
			wantExit: 1,
		},
		{
			name:     "execution-error",
			socket:   filepath.Join(t.TempDir(), "missing.sock"),
			file:     candidatePath,
			wantExit: 2,
		},
		{
			name:     "read-error",
			socket:   socketPath,
			file:     filepath.Join(t.TempDir(), "missing.yaml"),
			wantExit: 2,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			validateResult = tt.result
			cmd := exec.Command(bin, "validate", "--socket", tt.socket, "-f", tt.file, "--replace")
			out, err := cmd.CombinedOutput()
			if got := processExitCode(err); got != tt.wantExit {
				t.Fatalf("exit code = %d, want %d (err=%v output=%s)", got, tt.wantExit, err, out)
			}
			if tt.socket == socketPath && tt.file == candidatePath && !strings.Contains(string(out), `"valid": `) {
				t.Fatalf("validate output missing JSON result: %s", out)
			}
		})
	}
}

func buildRouterctlBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "routerctl")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build routerctl: %v\n%s", err, out)
	}
	return bin
}

func processExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
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
	if err == nil || !strings.Contains(err.Error(), "outcome is unconfirmed") {
		t.Fatalf("missing daemon error = %v", err)
	}
}

func TestApplyDriftedPreservesDiagnosticsAndFailsForUncommittedCanonical(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "routerd.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer listener.Close()
	server := &http.Server{Handler: controlapi.Handler{
		Apply: func(r *http.Request, req controlapi.ApplyRequest) (*controlapi.ApplyResult, error) {
			result := controlapi.NewApplyResult(&apply.Result{
				Phase:      "Drifted",
				Generation: 42,
				Warnings:   []string{"Interface/wan differs from the requested state"},
			})
			return &result, nil
		},
	}}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	candidatePath := filepath.Join(t.TempDir(), "candidate.yaml")
	if err := os.WriteFile(candidatePath, []byte(testRouterYAML("candidate-router")), 0644); err != nil {
		t.Fatalf("write candidate: %v", err)
	}
	var out bytes.Buffer
	err = run([]string{"apply", "--socket", socketPath, "-f", candidatePath, "--replace"}, &out, &bytes.Buffer{})
	var exitErr commandExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("apply error = %v, want exit code 1", err)
	}
	if !strings.Contains(err.Error(), "canonical config was not committed") {
		t.Fatalf("apply error = %q, want persisted-state warning", err)
	}
	var result controlapi.ApplyResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode structured apply result: %v\n%s", err, out.String())
	}
	if result.Result.Phase != "Drifted" || result.Result.Generation != 42 {
		t.Fatalf("apply result = %+v", result.Result)
	}
	if len(result.Result.Warnings) != 1 || !strings.Contains(result.Result.Warnings[0], "Interface/wan") {
		t.Fatalf("apply warnings = %#v", result.Result.Warnings)
	}
}

func TestApplyDriftedProcessExitIsNonZero(t *testing.T) {
	bin := buildRouterctlBinary(t)
	socketPath := filepath.Join(t.TempDir(), "routerd.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer listener.Close()
	server := &http.Server{Handler: controlapi.Handler{
		Apply: func(r *http.Request, req controlapi.ApplyRequest) (*controlapi.ApplyResult, error) {
			result := controlapi.NewApplyResult(&apply.Result{Phase: "Drifted", Generation: 43})
			return &result, nil
		},
	}}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	candidatePath := filepath.Join(t.TempDir(), "candidate.yaml")
	if err := os.WriteFile(candidatePath, []byte(testRouterYAML("candidate-router")), 0644); err != nil {
		t.Fatalf("write candidate: %v", err)
	}
	cmd := exec.Command(bin, "apply", "--socket", socketPath, "-f", candidatePath, "--replace")
	out, err := cmd.CombinedOutput()
	if got := processExitCode(err); got != 1 {
		t.Fatalf("exit code = %d, want 1 (err=%v output=%s)", got, err, out)
	}
	for _, want := range []string{`"phase": "Drifted"`, `"generation": 43`, "canonical config was not committed"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("apply output missing %q:\n%s", want, out)
		}
	}
}

func TestReadCandidateYAMLFromStdin(t *testing.T) {
	got, err := readCandidateYAML("-", strings.NewReader("kind: Router\n"))
	if err != nil {
		t.Fatalf("read candidate stdin: %v", err)
	}
	if got != "kind: Router\n" {
		t.Fatalf("candidate = %q", got)
	}
}

func TestRollbackCommandListsAndAppliesGeneration(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	generation, err := store.BeginGeneration("hash")
	if err != nil {
		t.Fatalf("begin generation: %v", err)
	}
	configYAML := testRouterYAML("rollback-router")
	if err := store.RecordGenerationConfig(generation, configYAML); err != nil {
		t.Fatalf("record generation config: %v", err)
	}
	if err := store.FinishGeneration(generation, "Healthy", nil); err != nil {
		t.Fatalf("finish generation: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close state: %v", err)
	}

	var listOut bytes.Buffer
	if err := run([]string{"rollback", "--list", "--state-file", statePath}, &listOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("rollback --list: %v", err)
	}
	if !strings.Contains(listOut.String(), fmt.Sprintf("%d", generation)) || !strings.Contains(listOut.String(), "yes") {
		t.Fatalf("rollback list output:\n%s", listOut.String())
	}

	socketPath := filepath.Join(dir, "routerd.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer listener.Close()
	var applied controlapi.ApplyRequest
	server := &http.Server{Handler: controlapi.Handler{
		Apply: func(r *http.Request, req controlapi.ApplyRequest) (*controlapi.ApplyResult, error) {
			applied = req
			result := controlapi.NewApplyResult(&apply.Result{Phase: "Healthy", Generation: generation + 1})
			return &result, nil
		},
	}}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	var applyOut bytes.Buffer
	if err := run([]string{"rollback", "--to", fmt.Sprintf("%d", generation), "--state-file", statePath, "--socket", socketPath, "--no-reconcile"}, &applyOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("rollback --to: %v", err)
	}
	if !applied.Replace || !applied.NoReconcile || applied.CandidateYAML != configYAML {
		t.Fatalf("rollback apply request = %+v", applied)
	}
}

func writeTestCommand(t *testing.T, path, script string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultStatePathUsesPlatformStateDir(t *testing.T) {
	defaults, _ := platform.Current()
	expectedDir := defaults.StateDir
	switch defaults.OS {
	case platform.OSLinux:
		expectedDir = "/var/lib/routerd"
	case platform.OSFreeBSD:
		expectedDir = "/var/db/routerd"
	}
	want := filepath.Join(expectedDir, "routerd.db")
	if got := defaultStatePath(); got != want {
		t.Fatalf("platform state path = %q, want %q", got, want)
	}
}

func writeShowConfig(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
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
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv6PrefixDelegation
      metadata:
        name: wan-pd
      spec:
        interface: wan
        client: networkd
        prefixLength: 60
    - apiVersion: net.routerd.net/v1alpha1
      kind: NAT44Rule
      metadata:
        name: lan-nat
      spec:
        outboundInterface: wan
        sourceCIDRs:
          - 192.0.2.0/24
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
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
