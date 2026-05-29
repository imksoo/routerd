// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/ingressdrain"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/resource"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestShowIPv6PDTableIncludesSpecStateLedger(t *testing.T) {
	dir := t.TempDir()
	configPath := writeShowConfig(t, dir)
	statePath := filepath.Join(dir, "state.json")
	ledgerPath := filepath.Join(dir, "artifacts.json")
	store := routerstate.New()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{
		CurrentPrefix:  "2001:db8:1200:1220::/60",
		LastPrefix:     "2001:db8:1200:1220::/60",
		LastObservedAt: "2026-04-28T01:02:03Z",
		DUIDText:       "00:03:00:01:02:00:5e:10:20:30",
		IAID:           "0",
	}), "test")
	if err := store.Save(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}
	ledger := resource.NewLedger()
	ledger.Remember([]resource.Artifact{{
		Kind:  "dhcp.ipv6.prefixDelegation",
		Name:  "ens18",
		Owner: "net.routerd.net/v1alpha1/DHCPv6PrefixDelegation/wan-pd",
	}})
	if err := ledger.Save(ledgerPath); err != nil {
		t.Fatalf("save ledger: %v", err)
	}

	var out bytes.Buffer
	err := run([]string{"show", "dhcpv6pd", "--config", configPath, "--state-file", statePath, "--ledger-file", ledgerPath}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("show ipv6pd: %v", err)
	}
	got := out.String()
	for _, want := range []string{"KIND", "DHCPv6PrefixDelegation", "wan-pd", "1 artifacts", "current=2001:db8:1200:1220::/60"} {
		if !strings.Contains(got, want) {
			t.Fatalf("show output missing %q:\n%s", want, got)
		}
	}
}

func TestDrainAndUndrainIngressBackend(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "routerd.db")
	var out bytes.Buffer
	if err := run([]string{"drain", "ingress/kubernetes-api", "backend=cp-01", "--duration=10m", "--state-file", statePath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("drain: %v", err)
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
	if err := run([]string{"undrain", "ingress/kubernetes-api", "--backend", "cp-01", "--state-file", statePath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("undrain: %v", err)
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

func TestRestartDNSResolverSelectsSingleResource(t *testing.T) {
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
	for _, binary := range []string{"systemctl", "rc-service", "service", "nixos-rebuild"} {
		if err := os.WriteFile(filepath.Join(binDir, binary), []byte("#!/bin/sh\necho "+binary+" \"$@\" >> \""+logPath+"\"\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out bytes.Buffer
	if err := run([]string{"restart-dns-resolver", "--config", configPath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("restart-dns-resolver: %v", err)
	}
	if !strings.Contains(out.String(), "DNSResolver/lan-resolver") {
		t.Fatalf("output = %s", out.String())
	}
	commands, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(commands))
	if !strings.Contains(got, "restart") || !strings.Contains(got, "routerd") || !strings.Contains(got, "dns") || !strings.Contains(got, "resolver") {
		t.Fatalf("commands = %q", got)
	}
}

func TestEventsCommandListsStateDatabaseEvents(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "test"}, "routerd.test.event", daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: "net.routerd.net/v1alpha1", Kind: "Interface", Name: "wan"}
	event.Reason = "TestEvent"
	event.Message = "event from test"
	if _, err := store.RecordBusEvent(context.Background(), event); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close state: %v", err)
	}

	var out bytes.Buffer
	if err := run([]string{"events", "--state-file", statePath, "--topic", "routerd.test.event"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("events: %v", err)
	}
	got := out.String()
	for _, want := range []string{"routerd.test.event", "Interface/wan", "TestEvent", "event from test"} {
		if !strings.Contains(got, want) {
			t.Fatalf("events output missing %q:\n%s", want, got)
		}
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
}

func TestDNSQueriesCommandReadsLogDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dns-queries.db")
	store, err := logstore.OpenDNSQueryLog(path)
	if err != nil {
		t.Fatalf("open query log: %v", err)
	}
	if err := store.Record(context.Background(), logstore.DNSQuery{
		Timestamp:     time.Now().UTC(),
		ClientAddress: "172.18.0.10",
		QuestionName:  "www.example.com",
		QuestionType:  "A",
		ResponseCode:  "NOERROR",
		Upstream:      "default",
	}); err != nil {
		t.Fatalf("record query: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close query log: %v", err)
	}

	var out bytes.Buffer
	if err := run([]string{"dns-queries", "--db", path, "--since", "1h"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("dns-queries: %v", err)
	}
	got := out.String()
	for _, want := range []string{"www.example.com", "172.18.0.10", "NOERROR"} {
		if !strings.Contains(got, want) {
			t.Fatalf("dns query output missing %q:\n%s", want, got)
		}
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
		Status: func(r *http.Request) (*controlapi.Status, error) {
			result := controlapi.NewStatus(&apply.Result{Phase: "Healthy", Generation: 7})
			return &result, nil
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
		{[]string{"status", "--socket", socketPath, "--json"}, `"phase": "Healthy"`},
		{[]string{"status", "--socket", socketPath, "-o", "json"}, `"generation": 7`},
		{[]string{"dns-queries", "--socket", socketPath, "--limit", "3"}, "socket.example"},
		{[]string{"traffic-flows", "--socket", socketPath}, "1.1.1.1"},
		{[]string{"firewall-logs", "--socket", socketPath}, "deny-test"},
		{[]string{"set-log-level", "--socket", socketPath, "debug"}, `"level": "debug"`},
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

func TestTrafficFlowsCommandReadsLogDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic-flows.db")
	store, err := logstore.OpenTrafficFlowLog(path)
	if err != nil {
		t.Fatalf("open traffic log: %v", err)
	}
	if err := store.UpsertActive(context.Background(), logstore.TrafficFlow{
		StartedAt:     time.Now().UTC(),
		ClientAddress: "172.18.0.10",
		ClientPort:    12345,
		PeerAddress:   "1.1.1.1",
		PeerPort:      443,
		Protocol:      "tcp",
	}); err != nil {
		t.Fatalf("record flow: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close traffic log: %v", err)
	}

	var out bytes.Buffer
	if err := run([]string{"traffic-flows", "--db", path, "--since", "1h"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("traffic-flows: %v", err)
	}
	got := out.String()
	for _, want := range []string{"172.18.0.10", "1.1.1.1", "tcp"} {
		if !strings.Contains(got, want) {
			t.Fatalf("traffic flow output missing %q:\n%s", want, got)
		}
	}
}

func TestFirewallLogsCommandReadsLogDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall-logs.db")
	store, err := logstore.OpenFirewallLog(path)
	if err != nil {
		t.Fatalf("open firewall log: %v", err)
	}
	if err := store.Record(context.Background(), logstore.FirewallLogEntry{
		Timestamp:  time.Now().UTC(),
		Action:     "drop",
		SrcAddress: "172.18.0.10",
		DstAddress: "198.51.100.10",
		Protocol:   "tcp",
		L3Proto:    "ipv4",
		RuleName:   "deny-test",
	}); err != nil {
		t.Fatalf("record firewall log: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close firewall log: %v", err)
	}
	var out bytes.Buffer
	if err := run([]string{"firewall-logs", "--db", path, "--since", "1h", "--action", "drop"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("firewall-logs: %v", err)
	}
	got := out.String()
	for _, want := range []string{"172.18.0.10", "198.51.100.10", "deny-test"} {
		if !strings.Contains(got, want) {
			t.Fatalf("firewall log output missing %q:\n%s", want, got)
		}
	}
}

func TestShowKindNameYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := writeShowConfig(t, dir)
	statePath := filepath.Join(dir, "state.json")
	ledgerPath := filepath.Join(dir, "artifacts.json")
	if err := routerstate.New().Save(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}
	if err := (resource.NewLedger()).Save(ledgerPath); err != nil {
		t.Fatalf("save ledger: %v", err)
	}

	var out bytes.Buffer
	err := run([]string{"show", "if/wan", "-o", "yaml", "--config", configPath, "--state-file", statePath, "--ledger-file", ledgerPath}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("show if/wan yaml: %v", err)
	}
	got := out.String()
	for _, want := range []string{"kind: Interface", "name: wan", "ifname: ens18"} {
		if !strings.Contains(got, want) {
			t.Fatalf("yaml output missing %q:\n%s", want, got)
		}
	}
}

func TestShowDiffAndLedgerModes(t *testing.T) {
	dir := t.TempDir()
	configPath := writeShowConfig(t, dir)
	statePath := filepath.Join(dir, "state.json")
	ledgerPath := filepath.Join(dir, "artifacts.json")
	if err := routerstate.New().Save(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}
	ledger := resource.NewLedger()
	ledger.Remember([]resource.Artifact{{
		Kind:  "net.link",
		Name:  "ens18",
		Owner: "net.routerd.net/v1alpha1/Interface/wan",
	}})
	if err := ledger.Save(ledgerPath); err != nil {
		t.Fatalf("save ledger: %v", err)
	}

	var diffOut bytes.Buffer
	if err := run([]string{"show", "interface/wan", "--diff", "--config", configPath, "--state-file", statePath, "--ledger-file", ledgerPath}, &diffOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("show diff: %v", err)
	}
	if got := diffOut.String(); !strings.Contains(got, "DIFF") || !strings.Contains(got, "fields") {
		t.Fatalf("diff output = %s", got)
	}

	var ledgerOut bytes.Buffer
	if err := run([]string{"show", "interface/wan", "--ledger", "--config", configPath, "--state-file", statePath, "--ledger-file", ledgerPath}, &ledgerOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("show ledger: %v", err)
	}
	if got := ledgerOut.String(); !strings.Contains(got, "1 artifacts") {
		t.Fatalf("ledger output = %s", got)
	}
}

func TestShowBGPVRRPAndIngressTables(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: routerdtest0
        managed: false
        owner: external
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSZone
      metadata:
        name: lan-zone
      spec:
        zone: lain.local
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSResolver
      metadata:
        name: lan-resolver
      spec:
        listen:
          - name: lan
            addresses: [127.0.0.1]
            port: 53
            sources: [local]
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSForwarder
      metadata:
        name: local
      spec:
        resolver: DNSResolver/lan-resolver
        match: [lain.local]
        zoneRefs: [DNSZone/lan-zone]
    - apiVersion: net.routerd.net/v1alpha1
      kind: VirtualAddress
      metadata:
        name: k8s-api-vip
      spec:
        interface: lan
        address: 192.168.123.250/32
        hostname: k8s-api.lain.local
        mode: vrrp
        vrrp:
          virtualRouterID: 66
          priority: 150
          peers: [192.168.123.111]
    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPRouter
      metadata:
        name: lan
      spec:
        asn: 64512
        routerID: 192.168.123.125
        gracefulRestart:
          enabled: true
    - apiVersion: firewall.routerd.net/v1alpha1
      kind: IngressService
      metadata:
        name: kubernetes-api
      spec:
        listen:
          interface: lan
          address: 192.168.123.250
          protocol: tcp
          port: 6443
        hostname: k8s-api.lain.local
        backends:
          - name: cp-01
            address: 192.168.123.11
            port: 6443
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	statePath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "lan", map[string]any{
		"phase":            "Established",
		"establishedPeers": 1,
		"acceptedPrefixes": 2,
		"peers": []map[string]any{{
			"address":           "192.168.123.111",
			"asn":               64513,
			"state":             "Established",
			"messagesReceived":  12,
			"messagesSent":      11,
			"prefixesReceived":  2,
			"lastErrorReason":   "Idle",
			"lastEstablishedAt": time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339Nano),
		}},
	}); err != nil {
		t.Fatalf("save bgp: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "k8s-api-vip", map[string]any{
		"phase":                "Applied",
		"address":              "192.168.123.250/32",
		"hostname":             "k8s-api.lain.local",
		"interface":            "lan",
		"role":                 "master",
		"priority":             150,
		"basePriority":         150,
		"virtualRouterID":      66,
		"lastRoleTransitionAt": time.Now().Add(-3 * time.Minute).UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("save vrrp: %v", err)
	}
	if err := store.SaveObjectStatus(api.FirewallAPIVersion, "IngressService", "kubernetes-api", map[string]any{
		"phase":           "Active",
		"hostname":        "k8s-api.lain.local",
		"listenAddress":   "192.168.123.250",
		"selection":       "failover",
		"healthyBackends": 1,
		"totalBackends":   1,
		"activeBackend":   map[string]any{"name": "cp-01", "address": "192.168.123.11", "port": 6443},
		"backends": []map[string]any{{
			"name":            "cp-01",
			"address":         "cp-01.lain.local",
			"resolvedAddress": "192.168.123.11",
			"port":            6443,
			"healthy":         true,
			"healthyCount":    7,
			"lastHealthyAt":   time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339Nano),
		}},
	}); err != nil {
		t.Fatalf("save ingress: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close state: %v", err)
	}
	for _, tt := range []struct {
		target string
		want   []string
	}{
		{"bgp", []string{"ROUTER", "lan", "192.168.123.111", "Established", "12", "11", "-"}},
		{"vrrp", []string{"VIP", "k8s-api.lain.local", "master", "66"}},
		{"ingress", []string{"SERVICE", "kubernetes-api", "cp-01/192.168.123.11:6443", "Healthy(7/0)"}},
	} {
		var out bytes.Buffer
		if err := run([]string{"show", tt.target, "--config", configPath, "--state-file", statePath, "--ledger-file", filepath.Join(dir, "missing-ledger.db")}, &out, &bytes.Buffer{}); err != nil {
			t.Fatalf("show %s: %v", tt.target, err)
		}
		got := out.String()
		for _, want := range tt.want {
			if !strings.Contains(got, want) {
				t.Fatalf("show %s output missing %q:\n%s", tt.target, want, got)
			}
		}
	}

	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestCommand(t, filepath.Join(binDir, "sysctl"), `#!/bin/sh
if [ "$1" = "-n" ] && [ "$2" = "net.ipv4.ip_forward" ]; then echo 1; exit 0; fi
if [ "$1" = "-n" ] && [ "$2" = "net.ipv6.conf.all.forwarding" ]; then echo 1; exit 0; fi
exit 1
`)
	writeTestCommand(t, filepath.Join(binDir, "nft"), `#!/bin/sh
cat <<'EOF'
table ip routerd_nat {
  chain prerouting {
    iifname "ens18" ip daddr 192.168.123.250 tcp dport 6443 counter dnat to 192.168.123.11:6443 comment "routerd IngressService kubernetes-api"
  }
  chain postrouting {
    iifname "ens18" ip daddr 192.168.123.11 tcp dport 6443 ct original ip daddr 192.168.123.250 ct original proto-dst 6443 counter masquerade comment "routerd IngressService kubernetes-api hairpin"
  }
}
EOF
`)
	writeTestCommand(t, filepath.Join(binDir, "conntrack"), `#!/bin/sh
cat <<'EOF'
tcp      6 431999 ESTABLISHED src=192.168.123.20 dst=192.168.123.250 sport=54000 dport=6443 src=192.168.123.11 dst=192.168.123.20 sport=6443 dport=54000
EOF
`)
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)
	var verbose bytes.Buffer
	if err := run([]string{"show", "ingress", "--verbose", "--config", configPath, "--state-file", statePath, "--ledger-file", filepath.Join(dir, "missing-ledger.db")}, &verbose, &bytes.Buffer{}); err != nil {
		t.Fatalf("show ingress --verbose: %v", err)
	}
	got := verbose.String()
	for _, want := range []string{"DATAPLANE", "NFT_DNAT", "NFT_SNAT", "kubernetes-api"} {
		if !strings.Contains(got, want) {
			t.Fatalf("verbose ingress output missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(got), " "), "kubernetes-api 1 1 1 1 1") {
		t.Fatalf("verbose ingress dataplane counts were not rendered:\n%s", got)
	}
	if !strings.Contains(got, "hairpinMode=auto hairpinRequired=true nft_snat=present") {
		t.Fatalf("verbose ingress output missing hairpin detail:\n%s", got)
	}
}

func TestShowBGPUsesStoredGoBGPStatus(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPRouter
      metadata:
        name: lan
      spec:
        asn: 64512
        routerID: 192.168.123.125
    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPPeer
      metadata:
        name: worker
      spec:
        routerRef: BGPRouter/lan
        peerASN: 64513
        peers: [192.168.123.111]
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	statePath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "lan", map[string]any{
		"phase":            "Established",
		"backend":          "gobgp",
		"establishedPeers": 1,
		"acceptedPrefixes": 2,
		"peers": []map[string]any{{
			"address":          "192.168.123.111",
			"asn":              64513,
			"state":            "ESTABLISHED",
			"messagesReceived": 12,
			"messagesSent":     11,
			"prefixesReceived": 2,
			"established":      true,
		}},
		"prefixes": []map[string]any{{
			"prefix":          "10.250.0.0/24",
			"valid":           true,
			"selectDeferred":  true,
			"selectionReason": "selectDeferred: waiting for graceful-restart EOR",
		}, {
			"prefix":    "10.250.0.10/32",
			"valid":     true,
			"best":      true,
			"installed": true,
		}},
	}); err != nil {
		t.Fatalf("save bgp: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close state: %v", err)
	}
	var out bytes.Buffer
	if err := run([]string{"show", "bgp", "--config", configPath, "--state-file", statePath, "--ledger-file", filepath.Join(dir, "missing-ledger.db")}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("show bgp: %v", err)
	}
	got := out.String()
	for _, want := range []string{"lan", "1/1", "192.168.123.111", "64513", "ESTABLISHED", "12", "11", "2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("show bgp output missing %q:\n%s", want, got)
		}
	}
	for _, want := range []string{"10.250.0.0/24", "selectDeferred", "waiting for graceful-restart EOR", "10.250.0.10/32", "yes"} {
		if !strings.Contains(got, want) {
			t.Fatalf("show bgp output missing route diagnostic %q:\n%s", want, got)
		}
	}
}

func writeTestCommand(t *testing.T, path, script string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestIngressHairpinDataplaneDetailWarnsWhenAutoSNATMissing(t *testing.T) {
	spec := api.IngressServiceSpec{
		Listen: api.IngressListenSpec{Address: "192.168.1.248", Protocol: "tcp", Port: 6443},
		Backends: []api.IngressBackendSpec{
			{Name: "cp-01", Address: "192.168.1.54", Port: 6443},
		},
	}
	status := map[string]any{
		"listenAddress": "192.168.1.248",
		"backends": []map[string]any{{
			"name":            "cp-01",
			"resolvedAddress": "192.168.1.54",
			"port":            6443,
			"healthy":         true,
		}},
	}
	got := ingressHairpinDataplaneDetail(spec, status, 0)
	want := "hairpinMode=auto hairpinRequired=true nft_snat=missing"
	if got != want {
		t.Fatalf("detail = %q, want %q", got, want)
	}
}

func TestIPOutputHasAddress(t *testing.T) {
	output := `2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500
    inet 192.168.123.250/32 scope global eth0
       valid_lft forever preferred_lft forever
    inet6 2001:db8::250/128 scope global
       valid_lft forever preferred_lft forever
`
	if !ipOutputHasAddress(output, "192.168.123.250/32", "ipv4") {
		t.Fatalf("IPv4 VIP was not detected")
	}
	if !ipOutputHasAddress(output, "2001:db8::250/128", "ipv6") {
		t.Fatalf("IPv6 VIP was not detected")
	}
	if ipOutputHasAddress(output, "192.168.123.251/32", "ipv4") {
		t.Fatalf("unexpected IPv4 VIP match")
	}
}

func TestDescribeOrphans(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	if err := os.WriteFile(configPath, []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources: []
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	statePath := filepath.Join(dir, "state.json")
	ledgerPath := filepath.Join(dir, "artifacts.json")
	if err := routerstate.New().Save(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}
	ledger := resource.NewLedger()
	ledger.Remember([]resource.Artifact{{
		Kind:  "systemd.service",
		Name:  "routerd-stale.service",
		Owner: "net.routerd.net/v1alpha1/DSLiteTunnel/stale",
	}})
	if err := ledger.Save(ledgerPath); err != nil {
		t.Fatalf("save ledger: %v", err)
	}
	var out bytes.Buffer
	if err := run([]string{"describe", "orphans", "--config", configPath, "--state-file", statePath, "--ledger-file", ledgerPath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("describe orphans: %v", err)
	}
	if got := out.String(); strings.Contains(got, "routerd-stale.service") {
		t.Fatalf("orphan output = %s", got)
	}
}

func TestShowPDLegacySubcommandRemoved(t *testing.T) {
	configPath := writeShowConfig(t, t.TempDir())
	dir := t.TempDir()
	var out bytes.Buffer
	err := run([]string{"show", "pd", "--config", configPath, "--state-file", filepath.Join(dir, "state.json"), "--ledger-file", filepath.Join(dir, "artifacts.json")}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("show pd alias: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "DHCPv6PrefixDelegation") {
		t.Fatalf("show pd output = %s", got)
	}
}

func TestGetKindAndListKinds(t *testing.T) {
	configPath := writeShowConfig(t, t.TempDir())
	var out bytes.Buffer
	if err := run([]string{"get", "pd", "--config", configPath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("get pd: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "DHCPv6PrefixDelegation") || !strings.Contains(got, "wan-pd") || strings.Contains(got, "STATE") {
		t.Fatalf("get output = %s", got)
	}

	out.Reset()
	if err := run([]string{"get", "--list-kinds", "--config", configPath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("get --list-kinds: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "Interface") || !strings.Contains(got, "NAT44Rule") {
		t.Fatalf("list kinds output = %s", got)
	}
}

func TestDescribeIPv6PDIncludesStatusLedgerEvents(t *testing.T) {
	dir := t.TempDir()
	configPath := writeShowConfig(t, dir)
	dbPath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite state: %v", err)
	}
	generation, err := store.BeginGeneration("test")
	if err != nil {
		t.Fatalf("begin generation: %v", err)
	}
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{
		CurrentPrefix:  "2001:db8:1200:1220::/60",
		LastPrefix:     "2001:db8:1200:1220::/60",
		T1:             "7200",
		T2:             "12600",
		PLTime:         "14400",
		VLTime:         "14400",
		LastObservedAt: "2026-04-28T01:02:03Z",
		LastReplyAt:    "2026-04-28T01:02:04Z",
		LastRequestAt:  "2026-04-28T01:02:02Z",
		LastRenewAt:    "2026-04-28T03:02:04Z",
		DUIDText:       "00:03:00:01:02:00:00:00:00:02",
		IAID:           "1",
	}), "test")
	if err := store.RecordEvent("net.routerd.net/v1alpha1", "DHCPv6PrefixDelegation", "wan-pd", "Normal", "PrefixObserved", "observed delegated prefix"); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.FinishGeneration(generation, "Healthy", nil); err != nil {
		t.Fatalf("finish generation: %v", err)
	}
	ledger, err := resource.OpenSQLiteLedger(dbPath)
	if err != nil {
		t.Fatalf("open sqlite ledger: %v", err)
	}
	ledger.Remember([]resource.Artifact{{Kind: "dhcp.ipv6.prefixDelegation", Name: "ens18", Owner: "net.routerd.net/v1alpha1/DHCPv6PrefixDelegation/wan-pd"}})
	if err := ledger.Close(); err != nil {
		t.Fatalf("close sqlite ledger: %v", err)
	}

	var out bytes.Buffer
	err = run([]string{"describe", "pd/wan-pd", "--config", configPath, "--state-file", dbPath, "--ledger-file", dbPath}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("describe pd: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"Currently observable:",
		"Current delegated prefix:",
		"Last delegated prefix:",
		"Client DUID:",
		"IAID:",
		"Last Reply at:",
		"Last Request at:",
		"Last Renew at:",
		"T1:",
		"7200s",
		"Next T1 at:",
		"2026-04-28T03:02:04Z",
		"Valid lifetime expires at:",
		"2026-04-28T05:02:04Z",
		"Last Apply Generation:",
		"PrefixObserved",
		"dhcp.ipv6.prefixDelegation/ens18",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("describe output missing %q:\n%s", want, got)
		}
	}
}

func TestDescribeInventoryHost(t *testing.T) {
	dir := t.TempDir()
	configPath := writeShowConfig(t, dir)
	dbPath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite state: %v", err)
	}
	if _, err := store.BeginGeneration("test"); err != nil {
		t.Fatalf("begin generation: %v", err)
	}
	status := map[string]any{
		"os": map[string]any{
			"goos":          "linux",
			"kernelName":    "Linux",
			"kernelRelease": "6.8.0-test",
		},
		"virtualization": map[string]any{"type": "kvm"},
		"serviceManager": "systemd",
		"commands":       map[string]any{"nft": true, "pf": false},
	}
	if err := store.SaveObjectStatus("routerd.net/v1alpha1", "Inventory", "host", status); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	if err := store.RecordEvent("routerd.net/v1alpha1", "Inventory", "host", "Normal", "InventoryObserved", "host inventory changed"); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.FinishGeneration(0, "Healthy", nil); err != nil {
		t.Fatalf("finish generation: %v", err)
	}

	var out bytes.Buffer
	err = run([]string{"describe", "inventory/host", "--config", configPath, "--state-file", dbPath, "--ledger-file", dbPath}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("describe inventory: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Kind:", "Inventory", "Currently observable:", "OS:", "linux", "Virtualization:", "kvm", "Service Manager:", "systemd", "InventoryObserved"} {
		if !strings.Contains(got, want) {
			t.Fatalf("describe inventory output missing %q:\n%s", want, got)
		}
	}
}

func TestDescribeShowsStatusReasonMessageAndRemediation(t *testing.T) {
	dir := t.TempDir()
	configPath := writeShowConfig(t, dir)
	dbPath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite state: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "wan", map[string]any{
		"phase":   "Drifted",
		"reason":  "NftablesRuleMissing",
		"message": "expected accept rule, found drop",
	}); err != nil {
		t.Fatalf("save interface status: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite state: %v", err)
	}

	var out bytes.Buffer
	err = run([]string{"describe", "interface/wan", "--config", configPath, "--state-file", dbPath, "--ledger-file", dbPath}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("describe interface: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"Phase:",
		"Drifted",
		"Reason:",
		"NftablesRuleMissing",
		"Message:",
		"expected accept rule, found drop",
		"Remediation:",
		"run `routerd apply` to reconcile this resource",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("describe interface output missing %q:\n%s", want, got)
		}
	}
}

func TestDescribeHealthyStatusOmitsRemediation(t *testing.T) {
	dir := t.TempDir()
	configPath := writeShowConfig(t, dir)
	dbPath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite state: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "wan", map[string]any{
		"phase": "Healthy",
	}); err != nil {
		t.Fatalf("save interface status: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite state: %v", err)
	}

	var out bytes.Buffer
	err = run([]string{"describe", "interface/wan", "--config", configPath, "--state-file", dbPath, "--ledger-file", dbPath}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("describe interface: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Phase:") || !strings.Contains(got, "Healthy") {
		t.Fatalf("describe healthy output missing phase:\n%s", got)
	}
	if strings.Contains(got, "Remediation:") {
		t.Fatalf("describe healthy output includes remediation:\n%s", got)
	}
}

func TestDescribeUsesObjectStatusForControllerManagedResource(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: TailscaleNode
      metadata:
        name: home
      spec:
        hostname: homert02
        advertiseExitNode: true
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	dbPath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite state: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "TailscaleNode", "home", map[string]any{
		"phase":        "Running",
		"backendState": "Running",
		"tailnetName":  "example@example.com",
		"peerCount":    7,
	}); err != nil {
		t.Fatalf("save tailscale status: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite state: %v", err)
	}
	var out bytes.Buffer
	err = run([]string{"describe", "tailscale/home", "--config", configPath, "--state-file", dbPath, "--ledger-file", dbPath}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("describe tailscale: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Currently observable:", "yes", "backendState:", "Running", "tailnetName:", "example@example.com", "peerCount:", "7"} {
		if !strings.Contains(got, want) {
			t.Fatalf("describe tailscale output missing %q:\n%s", want, got)
		}
	}
}

func TestShowDerivedResourcesListsGeneratedServiceUnits(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: HealthCheck
      metadata:
        name: internet
      spec:
        target: 1.1.1.1
        protocol: tcp
        port: 443
    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallEventLog
      metadata:
        name: nflog
      spec:
        enabled: true
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	dbPath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite state: %v", err)
	}
	if err := store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", "routerd-healthcheck@internet.service", map[string]any{
		"phase":    "Applied",
		"source":   "HealthCheck/internet",
		"unitName": "routerd-healthcheck@internet.service",
		"path":     "/etc/systemd/system/routerd-healthcheck@internet.service",
	}); err != nil {
		t.Fatalf("save service unit status: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite state: %v", err)
	}

	var out bytes.Buffer
	err = run([]string{"show", "derived-resources", "--config", configPath, "--state-file", dbPath, "--ledger-file", dbPath}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("show derived-resources: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"KIND",
		"ServiceUnit",
		"routerd.service",
		"Router/test",
		"routerd-healthcheck@internet.service",
		"HealthCheck/internet",
		"Applied",
		"routerd-firewall-logger.service",
		"FirewallEventLog/nflog",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("derived resources output missing %q:\n%s", want, got)
		}
	}
}

func TestShowDerivedResourcesHidesAndMarksStaleState(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources: []
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	dbPath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite state: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "PPPoEInterface", "wan", map[string]any{
		"phase":  "Applied",
		"ifname": "ppp0",
	}); err != nil {
		t.Fatalf("save stale status: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite state: %v", err)
	}

	var out bytes.Buffer
	err = run([]string{"show", "derived-resources", "--config", configPath, "--state-file", dbPath}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("show derived-resources: %v", err)
	}
	if strings.Contains(out.String(), "PPPoEInterface") {
		t.Fatalf("default derived resources output includes stale status:\n%s", out.String())
	}

	out.Reset()
	err = run([]string{"show", "derived-resources", "--config", configPath, "--state-file", dbPath, "--include-stale", "-o", "json"}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("show derived-resources --include-stale: %v", err)
	}
	got := out.String()
	for _, want := range []string{"PPPoEInterface", `"stale": true`, `"phase": "Stale"`, `"reason": "UnsupportedResourceKind"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("include-stale output missing %q:\n%s", want, got)
		}
	}
}

func TestDiagnoseEgressShowsPolicyHealthAndNAT(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: EgressRoutePolicy
      metadata:
        name: ipv4-default
      spec:
        family: ipv4
        selection: highest-weight-ready
        candidates:
          - name: ds-lite
            source: DSLiteTunnel/ds-lite
            device: ds-routerd
            gatewaySource: none
            weight: 80
            healthCheck: internet
    - apiVersion: net.routerd.net/v1alpha1
      kind: NAT44Rule
      metadata:
        name: lan-to-wan
      spec:
        type: masquerade
        egressPolicyRef: ipv4-default
        sourceRanges:
          - 172.18.0.0/16
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	dbPath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite state: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default", map[string]any{"phase": "Applied", "selectedCandidate": "ds-lite", "selectedDevice": "ds-routerd"}); err != nil {
		t.Fatalf("save egress status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", "internet", map[string]any{"phase": "Healthy"}); err != nil {
		t.Fatalf("save health status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "NAT44Rule", "lan-to-wan", map[string]any{"phase": "Active", "activeEgressInterface": "ds-routerd"}); err != nil {
		t.Fatalf("save nat status: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite state: %v", err)
	}

	var out bytes.Buffer
	err = run([]string{"diagnose", "egress", "ipv4-default", "--config", configPath, "--state-file", dbPath, "--no-host"}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("diagnose egress: %v", err)
	}
	got := out.String()
	for _, want := range []string{"DIAGNOSE", "egress", "selectedCandidate", "ds-lite", "HealthCheck", "internet", "NAT44Rule", "lan-to-wan"} {
		if !strings.Contains(got, want) {
			t.Fatalf("diagnose output missing %q:\n%s", want, got)
		}
	}
}

func TestDefaultStatePathUsesPlatformStateDir(t *testing.T) {
	if got := defaultStatePath(); got == "" || filepath.Base(got) != "routerd.db" {
		t.Fatalf("default state path = %q", got)
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
