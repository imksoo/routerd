// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestAnnotateResourceOwnershipFromControllerModes(t *testing.T) {
	resources := annotateResourceOwnership([]routerstate.ObjectStatus{{
		APIVersion: "net.routerd.net/v1alpha1",
		Kind:       "EgressRoutePolicy",
		Name:       "wan-egress",
		Status:     map[string]any{"phase": "Applied"},
	}}, []controlapi.ControllerStatus{{
		Name:          "route",
		Mode:          "live",
		ResourceKinds: []string{"EgressRoutePolicy"},
	}})
	if len(resources) != 1 {
		t.Fatalf("resources = %d", len(resources))
	}
	if resources[0].Owner != "route" || resources[0].Status["owner"] != "route" {
		t.Fatalf("owner metadata not annotated: %+v status=%+v", resources[0], resources[0].Status)
	}
	if resources[0].ManagedBy != "routerd" || resources[0].Management != "managed" {
		t.Fatalf("management metadata = %q/%q", resources[0].ManagedBy, resources[0].Management)
	}
}

func TestGatewayHealthAggregatesGatewayResources(t *testing.T) {
	health := gatewayHealth([]routerstate.ObjectStatus{
		{APIVersion: api.NetAPIVersion, Kind: "DNSResolver", Name: "lan-resolver", Status: map[string]any{"phase": "Applied", "health": "HealthOK", "listeners": 4}},
		{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation", Name: "wan-pd", Status: map[string]any{"phase": "Pending", "reason": "NoPrefix", "waiting": []map[string]string{{"source": "wan", "reason": "NoReply"}}}},
		{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel", Name: "ds-lite", Status: map[string]any{"phase": "Error", "reason": "AFTRResolveFailed"}},
		{APIVersion: api.NetAPIVersion, Kind: "DNSResolver", Name: "missing"},
		{APIVersion: api.NetAPIVersion, Kind: "HealthCheck", Name: "internet", Status: map[string]any{"phase": "Healthy"}},
	})
	if health.Overall != "down" {
		t.Fatalf("overall = %q, want down: %+v", health.Overall, health)
	}
	statuses := map[string]string{}
	for _, component := range health.Components {
		statuses[component.Kind+"/"+component.Name] = component.Status
	}
	for name, want := range map[string]string{
		"DNSResolver/lan-resolver":      "ok",
		"DHCPv6PrefixDelegation/wan-pd": "degraded",
		"DSLiteTunnel/ds-lite":          "down",
		"DNSResolver/missing":           "unknown",
		"HealthCheck/internet":          "pass",
	} {
		if statuses[name] != want {
			t.Fatalf("%s status = %q, want %q: %+v", name, statuses[name], want, health.Components)
		}
	}
	if len(health.Components) != 5 {
		t.Fatalf("components = %d, want 5: %+v", len(health.Components), health.Components)
	}
}

func TestGatewayHealthEgressRoutePolicyCandidates(t *testing.T) {
	tests := []struct {
		name               string
		status             map[string]any
		wantStatus         string
		wantDetail         []string
		wantSelectedPath   string
		wantPreferredPath  string
		wantFallbackReason string
		wantLastTransition string
	}{
		{
			name: "selected preferred",
			status: map[string]any{
				"phase":              "Healthy",
				"selectedCandidate":  "ds-lite-primary",
				"preferredCandidate": "ds-lite-primary",
				"lastTransitionAt":   "2026-05-26T10:11:12Z",
			},
			wantStatus:         "pass",
			wantDetail:         []string{"selected=ds-lite-primary,preferred=ds-lite-primary", "selectedCandidate=ds-lite-primary", "preferredCandidate=ds-lite-primary"},
			wantSelectedPath:   "ds-lite-primary",
			wantPreferredPath:  "ds-lite-primary",
			wantLastTransition: "2026-05-26T10:11:12Z",
		},
		{
			name: "fallback",
			status: map[string]any{
				"phase":             "Healthy",
				"reason":            "PreferredCandidateUnhealthy",
				"selectedCandidate": "pppoe-fallback",
				"updatedAt":         "2026-05-26T10:12:13Z",
				"candidates": []map[string]any{
					{"name": "ds-lite-primary", "weight": 100, "priority": 10, "ready": false},
					{"name": "pppoe-fallback", "weight": 10, "priority": 20, "ready": true},
				},
			},
			wantStatus:         "warn",
			wantDetail:         []string{"selected=pppoe-fallback,preferred=ds-lite-primary", "selectedCandidate=pppoe-fallback", "preferredCandidate=ds-lite-primary"},
			wantSelectedPath:   "pppoe-fallback",
			wantPreferredPath:  "ds-lite-primary",
			wantFallbackReason: "PreferredCandidateUnhealthy",
			wantLastTransition: "2026-05-26T10:12:13Z",
		},
		{
			name: "error",
			status: map[string]any{
				"phase":              "Error",
				"reason":             "NoReadyCandidates",
				"selectedCandidate":  "pppoe-fallback",
				"preferredCandidate": "ds-lite-primary",
			},
			wantStatus:         "down",
			wantSelectedPath:   "pppoe-fallback",
			wantPreferredPath:  "ds-lite-primary",
			wantFallbackReason: "NoReadyCandidates",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			health := gatewayHealth([]routerstate.ObjectStatus{{
				APIVersion: api.NetAPIVersion,
				Kind:       "EgressRoutePolicy",
				Name:       "ipv4-default",
				Status:     tt.status,
			}})
			if len(health.Components) != 1 {
				t.Fatalf("components = %+v", health.Components)
			}
			component := health.Components[0]
			if component.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q: %+v", component.Status, tt.wantStatus, component)
			}
			for _, want := range tt.wantDetail {
				if !strings.Contains(component.Detail, want) {
					t.Fatalf("detail missing %q: %q", want, component.Detail)
				}
			}
			if component.SelectedPath != tt.wantSelectedPath {
				t.Fatalf("selectedPath = %q, want %q: %+v", component.SelectedPath, tt.wantSelectedPath, component)
			}
			if component.PreferredPath != tt.wantPreferredPath {
				t.Fatalf("preferredPath = %q, want %q: %+v", component.PreferredPath, tt.wantPreferredPath, component)
			}
			if component.FallbackReason != tt.wantFallbackReason {
				t.Fatalf("fallbackReason = %q, want %q: %+v", component.FallbackReason, tt.wantFallbackReason, component)
			}
			if component.LastTransition != tt.wantLastTransition {
				t.Fatalf("lastTransition = %q, want %q: %+v", component.LastTransition, tt.wantLastTransition, component)
			}
		})
	}
}

func TestGatewayHealthHealthCheckFailedProbeEvidence(t *testing.T) {
	health := gatewayHealth([]routerstate.ObjectStatus{
		{APIVersion: api.NetAPIVersion, Kind: "HealthCheck", Name: "internet", Status: map[string]any{
			"phase":            "Unhealthy",
			"failedProbes":     []any{"tcp-443", "dns-aaaa", "tcp-443"},
			"lastTransitionAt": "2026-05-26T10:13:14Z",
		}},
		{APIVersion: api.NetAPIVersion, Kind: "HealthCheck", Name: "plain", Status: map[string]any{
			"phase": "Healthy",
		}},
	})
	components := map[string]GatewayHealthComponent{}
	for _, component := range health.Components {
		components[component.Name] = component
	}
	if got := components["internet"].FailedProbes; len(got) != 2 || got[0] != "tcp-443" || got[1] != "dns-aaaa" {
		t.Fatalf("failedProbes = %#v, want tcp-443/dns-aaaa", got)
	}
	if components["internet"].LastTransition != "2026-05-26T10:13:14Z" {
		t.Fatalf("lastTransition = %q", components["internet"].LastTransition)
	}
	if len(components["plain"].FailedProbes) != 0 {
		t.Fatalf("plain failedProbes = %#v, want omitted/empty", components["plain"].FailedProbes)
	}
}

func TestGatewayHealthNAT44AndHealthCheckStatuses(t *testing.T) {
	health := gatewayHealth([]routerstate.ObjectStatus{
		{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule", Name: "nat-applied", Status: map[string]any{"phase": "Applied"}},
		{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule", Name: "nat-pending", Status: map[string]any{"phase": "Pending"}},
		{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule", Name: "nat-error", Status: map[string]any{"phase": "Error"}},
		{APIVersion: api.NetAPIVersion, Kind: "HealthCheck", Name: "hc-healthy", Status: map[string]any{"phase": "Healthy"}},
		{APIVersion: api.NetAPIVersion, Kind: "HealthCheck", Name: "hc-unhealthy", Status: map[string]any{"phase": "Unhealthy"}},
		{APIVersion: api.NetAPIVersion, Kind: "HealthCheck", Name: "hc-disabled", Status: map[string]any{"phase": "Disabled"}},
	})
	if health.Overall != "down" {
		t.Fatalf("overall = %q, want down: %+v", health.Overall, health)
	}
	statuses := map[string]string{}
	for _, component := range health.Components {
		statuses[component.Kind+"/"+component.Name] = component.Status
	}
	for name, want := range map[string]string{
		"NAT44Rule/nat-applied":    "pass",
		"NAT44Rule/nat-pending":    "degraded",
		"NAT44Rule/nat-error":      "down",
		"HealthCheck/hc-healthy":   "pass",
		"HealthCheck/hc-unhealthy": "down",
		"HealthCheck/hc-disabled":  "skip",
	} {
		if statuses[name] != want {
			t.Fatalf("%s status = %q, want %q: %+v", name, statuses[name], want, health.Components)
		}
	}
}

func TestGatewayHealthSkipsConditionFalseComponents(t *testing.T) {
	health := gatewayHealth([]routerstate.ObjectStatus{
		{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation", Name: "wan-pd", Status: map[string]any{"phase": "Pending", "reason": "WhenFalse"}},
		{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel", Name: "ds-lite", Status: map[string]any{"phase": "Pending", "reason": "WhenFalse"}},
		{APIVersion: api.NetAPIVersion, Kind: "HealthCheck", Name: "internet-via-dslite", Status: map[string]any{"phase": "Pending", "reason": "WhenFalse"}},
		{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule", Name: "lan-to-dslite", Status: map[string]any{"phase": "Pending", "reason": "WhenFalse"}},
		{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule", Name: "lan-to-ix2215", Status: map[string]any{"phase": "Active"}},
	})
	if health.Overall != "ok" {
		t.Fatalf("overall = %q, want ok: %+v", health.Overall, health)
	}
	statuses := map[string]string{}
	for _, component := range health.Components {
		statuses[component.Kind+"/"+component.Name] = component.Status
	}
	for name, want := range map[string]string{
		"DHCPv6PrefixDelegation/wan-pd":   "skip",
		"DSLiteTunnel/ds-lite":            "skip",
		"HealthCheck/internet-via-dslite": "skip",
		"NAT44Rule/lan-to-dslite":         "skip",
		"NAT44Rule/lan-to-ix2215":         "pass",
	} {
		if statuses[name] != want {
			t.Fatalf("%s status = %q, want %q: %+v", name, statuses[name], want, health.Components)
		}
	}
}

func TestHandlerIncludesConfiguredResourcesWithoutObservedStatus(t *testing.T) {
	handler := New(Options{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TailscaleNode"},
				Metadata: api.ObjectMeta{Name: "homert02"},
				Spec:     api.TailscaleNodeSpec{AdvertiseExitNode: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.0.2.1/24"},
			},
		}}},
	})
	resources, err := handler.resourceStatuses()
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 2 {
		t.Fatalf("resources = %+v, want configured resources without observed status", resources)
	}
	var tailscale *routerstate.ObjectStatus
	for i := range resources {
		if resources[i].Kind == "TailscaleNode" && resources[i].Name == "homert02" {
			tailscale = &resources[i]
		}
	}
	if tailscale == nil {
		t.Fatalf("configured TailscaleNode missing from resources: %+v", resources)
	}
	status := tailscale.Status
	if status["phase"] != "NotObserved" || status["reason"] != "NoObservedStatus" || status["configured"] != true || status["observed"] != false {
		t.Fatalf("tailscale synthetic status = %+v", status)
	}
}

func TestHandlerServesEmbeddedAppShell(t *testing.T) {
	handler := New(Options{Title: "homert02"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`window.__ROUTERD_WEB_CONSOLE__`, `basePath: "/"`, `title: "homert02"`, `type="module"`, `id="root"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("index missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func TestHandlerServesGenerationHistoryAndDiff(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	firstConfig := `kind: Router
metadata:
  name: old
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: PPPoESession
      metadata:
        name: wan
      spec:
        interface: wan0
        username: user-old
        password: generation-old-secret
`
	secondConfig := `kind: Router
metadata:
  name: new
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: PPPoESession
      metadata:
        name: wan
      spec:
        interface: wan0
        username: user-new
        password: generation-new-secret
`
	first, err := store.BeginGeneration("hash-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordGenerationConfig(first, firstConfig); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishGeneration(first, "Healthy", nil); err != nil {
		t.Fatal(err)
	}
	second, err := store.BeginGeneration("hash-b")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordGenerationConfig(second, secondConfig); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishGeneration(second, "Healthy", nil); err != nil {
		t.Fatal(err)
	}
	handler := New(Options{Store: store})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/generations", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"generation": 2`, `"configHash": "hash-b"`, `"hasYaml": true`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("generations missing %q:\n%s", want, rec.Body.String())
		}
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/generations/1/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, notWant := range []string{"generation-old-secret"} {
		if strings.Contains(rec.Body.String(), notWant) {
			t.Fatalf("generation config leaked %q:\n%s", notWant, rec.Body.String())
		}
	}
	for _, want := range []string{"password:", api.RedactedSecret, "interface: wan0", "username: user-old"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("generation config missing %q:\n%s", want, rec.Body.String())
		}
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/generations/1/diff/2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, notWant := range []string{"generation-old-secret", "generation-new-secret"} {
		if strings.Contains(rec.Body.String(), notWant) {
			t.Fatalf("generation diff leaked %q:\n%s", notWant, rec.Body.String())
		}
	}
	for _, want := range []string{"--- generation-1.yaml", "+++ generation-2.yaml", "-    name: old", "+    name: new", api.RedactedSecret, "interface: wan0"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("diff missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func reqContext() context.Context { return context.Background() }

func TestHandlerRejectsWriteMethods(t *testing.T) {
	handler := New(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/summary", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandlerServesDNSQueries(t *testing.T) {
	queryLog := t.TempDir() + "/dns-queries.db"
	dnsLog, err := logstore.OpenDNSQueryLog(queryLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := dnsLog.Record(context.Background(), logstore.DNSQuery{Timestamp: time.Now(), ClientAddress: "172.18.0.2", QuestionName: "www.example.com", QuestionType: "AAAA", ResponseCode: "NOERROR"}); err != nil {
		t.Fatal(err)
	}
	_ = dnsLog.Close()
	handler := New(Options{DNSQueryLogPath: queryLog})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dns-queries?since=1h&limit=10", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "www.example.com") {
		t.Fatalf("dns queries missing row:\n%s", rec.Body.String())
	}
}

func TestHandlerServesTrafficFlows(t *testing.T) {
	path := t.TempDir() + "/traffic-flows.db"
	queryLog := t.TempDir() + "/dns-queries.db"
	dnsLog, err := logstore.OpenDNSQueryLog(queryLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := dnsLog.Record(context.Background(), logstore.DNSQuery{Timestamp: time.Now(), ClientAddress: "172.18.0.2", QuestionName: "one.one.one.one", QuestionType: "A", ResponseCode: "NOERROR", Answers: []string{"1.1.1.1"}}); err != nil {
		t.Fatal(err)
	}
	_ = dnsLog.Close()
	flowLog, err := logstore.OpenTrafficFlowLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := flowLog.UpsertActive(context.Background(), logstore.TrafficFlow{StartedAt: time.Now(), ClientAddress: "172.18.0.2", PeerAddress: "1.1.1.1", PeerPort: 443, Protocol: "tcp", ApplicationProtocol: "tls", Category: "web", Confidence: 90, Metadata: map[string]string{"tls.sni": "one.one.one.one"}}); err != nil {
		t.Fatal(err)
	}
	_ = flowLog.Close()
	handler := New(Options{TrafficFlowLogPath: path, DNSQueryLogPath: queryLog})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic-flows?since=1h&limit=10", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "one.one.one.one") {
		t.Fatalf("traffic flows missing row:\n%s", rec.Body.String())
	}
	for _, want := range []string{`"applicationProtocol": "tls"`, `"category": "web"`, `"confidence": 90`, `"tls.sni": "one.one.one.one"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("traffic flows missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func TestTrafficFlowPortFallbackOverridesDNSOnTailscalePort(t *testing.T) {
	flow := logstore.TrafficFlow{
		Protocol:         "udp",
		ClientAddress:    "172.18.0.2",
		ClientPort:       53122,
		PeerAddress:      "198.51.100.10",
		PeerPort:         41641,
		AppName:          "dns",
		AppCategory:      "network",
		AppConfidence:    75,
		ResolvedHostname: "binary-noise",
	}
	applyTrafficFlowPortFallback(&flow)
	if flow.AppName != "tailscale" || flow.AppCategory != "port-fallback" || flow.ResolvedHostname != "" {
		t.Fatalf("flow fallback = %#v", flow)
	}
}

func TestTrafficFlowPortFallbackKeepsProviderSeparateFromProtocol(t *testing.T) {
	flow := logstore.TrafficFlow{
		Protocol:         "tcp",
		ClientAddress:    "172.18.0.2",
		ClientPort:       53122,
		PeerAddress:      "203.0.113.10",
		PeerPort:         443,
		ResolvedHostname: "edge.googleusercontent.com",
	}
	applyTrafficFlowPortFallback(&flow)
	if flow.AppName != "tls" || flow.AppConfidence != 40 {
		t.Fatalf("flow fallback = %#v", flow)
	}
}

func TestTrafficFlowPortFallbackCanonicalizesLegacyProviderHTTPS(t *testing.T) {
	flow := logstore.TrafficFlow{
		Protocol:         "tcp",
		ClientAddress:    "172.18.0.2",
		ClientPort:       53122,
		PeerAddress:      "203.0.113.10",
		PeerPort:         443,
		AppName:          "google-https",
		AppCategory:      "port-fallback",
		AppConfidence:    45,
		ResolvedHostname: "edge.googleusercontent.com",
	}
	applyTrafficFlowPortFallback(&flow)
	if flow.AppName != "tls" {
		t.Fatalf("flow fallback = %#v", flow)
	}
}

func TestConnectionPortFallbackPromotesTailscaleDERPStun(t *testing.T) {
	entry := observe.ConnectionEntry{
		Protocol:      "udp",
		AppName:       "stun",
		AppCategory:   "port-fallback",
		AppConfidence: 40,
		Original: observe.ConntrackTuple{
			Source:              "192.0.2.10",
			SourcePort:          "55123",
			Destination:         "205.147.105.30",
			DestinationPort:     "3478",
			DestinationHostname: "derp20c.tailscale.com",
		},
	}
	applyConnectionPortFallback(&entry)
	if entry.AppName != "tailscale" || entry.AppConfidence < 60 {
		t.Fatalf("connection fallback = %#v", entry)
	}
}

func TestConnectionPortFallbackUsesTailscaleHostnameOnHTTPS(t *testing.T) {
	entry := observe.ConnectionEntry{
		Protocol:      "tcp",
		AppName:       "tls",
		AppCategory:   "port-fallback",
		AppConfidence: 40,
		Original: observe.ConntrackTuple{
			Source:              "192.0.2.10",
			SourcePort:          "55123",
			Destination:         "203.0.113.10",
			DestinationPort:     "443",
			DestinationHostname: "lb.fra.tailscale.com",
		},
	}
	applyConnectionPortFallback(&entry)
	if entry.AppName != "tailscale" || entry.AppConfidence < 60 {
		t.Fatalf("connection fallback = %#v", entry)
	}
}

func TestHandlerServesFirewallLogs(t *testing.T) {
	path := t.TempDir() + "/firewall-logs.db"
	firewallLog, err := logstore.OpenFirewallLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := firewallLog.Record(context.Background(), logstore.FirewallLogEntry{Timestamp: time.Now(), Action: "drop", SrcAddress: "172.18.0.2", DstAddress: "198.51.100.1", Protocol: "tcp", TCPFlags: "SYN", L3Proto: "ipv4"}); err != nil {
		t.Fatal(err)
	}
	_ = firewallLog.Close()
	handler := New(Options{FirewallLogPath: path})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/firewall-logs?since=1h&action=drop", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "198.51.100.1") || !strings.Contains(rec.Body.String(), `"tcpFlags": "SYN"`) {
		t.Fatalf("firewall logs missing row:\n%s", rec.Body.String())
	}
}

func TestHandlerAnnotatesFirewallDestinationSets(t *testing.T) {
	path := t.TempDir() + "/firewall-logs.db"
	firewallLog, err := logstore.OpenFirewallLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := firewallLog.Record(context.Background(), logstore.FirewallLogEntry{
		Timestamp:  time.Now(),
		RuleName:   "block-cloud",
		Action:     "drop",
		SrcAddress: "172.18.0.2",
		DstAddress: "203.0.113.10",
		Protocol:   "tcp",
		L3Proto:    "ipv4",
	}); err != nil {
		t.Fatal(err)
	}
	_ = firewallLog.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"},
			Metadata: api.ObjectMeta{Name: "cloud-service"},
			Spec:     api.IPAddressSetSpec{Addresses: []string{"203.0.113.10"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"},
			Metadata: api.ObjectMeta{Name: "block-cloud"},
			Spec: api.FirewallRuleSpec{
				FromZone:           "lan",
				ToZone:             "wan",
				DestinationSetRefs: []string{"cloud-service"},
				Protocol:           "tcp",
				Action:             "drop",
				Log:                true,
			},
		},
	}}}
	handler := New(Options{FirewallLogPath: path, Router: router})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/firewall-logs?since=1h&action=drop", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"destinationSetMatches"`, `"resourceName": "cloud-service"`, `"source": "firewall-rule"`, `"current": true`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("firewall logs missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func TestHandlerServesFirewallDenyTimeline(t *testing.T) {
	path := t.TempDir() + "/firewall-logs.db"
	firewallLog, err := logstore.OpenFirewallLog(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for i := 0; i < 250; i++ {
		if err := firewallLog.Record(context.Background(), logstore.FirewallLogEntry{
			Timestamp:  now.Add(-23 * time.Hour).Add(time.Duration(i%30) * time.Minute),
			Action:     "drop",
			SrcAddress: "172.18.0.2",
			DstAddress: "198.51.100.1",
			Protocol:   "tcp",
			L3Proto:    "ipv4",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := firewallLog.Record(context.Background(), logstore.FirewallLogEntry{
		Timestamp:  now.Add(-23 * time.Hour),
		Action:     "accept",
		SrcAddress: "172.18.0.2",
		DstAddress: "198.51.100.2",
		Protocol:   "tcp",
		L3Proto:    "ipv4",
	}); err != nil {
		t.Fatal(err)
	}
	_ = firewallLog.Close()
	handler := New(Options{FirewallLogPath: path})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/firewall/deny-timeline?range=24h&bucket=1h", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var timeline []logstore.FirewallDenyTimelineBucket
	if err := json.Unmarshal(rec.Body.Bytes(), &timeline); err != nil {
		t.Fatal(err)
	}
	if len(timeline) != 24 {
		t.Fatalf("timeline buckets = %d, want 24", len(timeline))
	}
	total := 0
	for _, bucket := range timeline {
		total += bucket.Count
	}
	if total != 250 {
		t.Fatalf("timeline total = %d, want 250: %+v", total, timeline)
	}
}

func TestHandlerIncludesDHCPLeases(t *testing.T) {
	leasePath := filepath.Join(t.TempDir(), "dnsmasq.leases")
	expires := time.Now().Add(time.Hour).Unix()
	if err := os.WriteFile(leasePath, []byte(fmt.Sprintf("%d 7c:dd:e9:01:40:15 172.18.1.78 ATOM 01:7c:dd:e9:01:40:15\n", expires)), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := New(Options{DHCPLeasePaths: []string{leasePath}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/summary", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"172.18.1.78", "7c:dd:e9:01:40:15", "ATOM", "ATOM tech Inc."} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("summary missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func TestHandlerOmitsUnsetDHCPLeaseStickyUntil(t *testing.T) {
	leasePath := filepath.Join(t.TempDir(), "dnsmasq.leases")
	expires := time.Now().Add(time.Hour).Unix()
	if err := os.WriteFile(leasePath, []byte(fmt.Sprintf("%d 7c:dd:e9:01:40:15 172.18.1.78 ATOM 01:7c:dd:e9:01:40:15\n", expires)), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := New(Options{DHCPLeasePaths: []string{leasePath}})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/summary?dhcpLeases=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		DHCPLeases []map[string]json.RawMessage `json:"dhcpLeases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.DHCPLeases) != 1 {
		t.Fatalf("leases = %#v", response.DHCPLeases)
	}
	if _, ok := response.DHCPLeases[0]["stickyUntil"]; ok {
		t.Fatalf("unset stickyUntil must be omitted: %s", rec.Body.String())
	}
}

func TestHandlerDefaultDHCPLeasePathsUsePlatformDefaults(t *testing.T) {
	defaults, features := platform.Current()
	handler := New(Options{})
	want := platform.DnsmasqLeaseCandidates(defaults, features)
	if got := handler.opts.DHCPLeasePaths; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("default DHCP lease paths = %#v, want %#v", got, want)
	}
}

func TestHandlerFiltersExpiredDHCPLeasesAndPrefersConfiguredLeaseOrder(t *testing.T) {
	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "primary", "dnsmasq.leases")
	fallbackPath := filepath.Join(dir, "fallback", "dnsmasq.leases")
	if err := os.MkdirAll(filepath.Dir(primaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(fallbackPath), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	primaryLease := fmt.Sprintf("%d 8c:aa:b5:48:e0:9c 172.18.1.109 SwitchBot-HubMini-48E09C *\n", now.Add(2*time.Hour).Unix())
	expiredFallbackLease := fmt.Sprintf("%d bc:24:11:26:7b:ab 172.18.1.109 exitnode ff:ca:53:09:5a\n", now.Add(-time.Hour).Unix())
	if err := os.WriteFile(primaryPath, []byte(primaryLease), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fallbackPath, []byte(expiredFallbackLease), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := New(Options{DHCPLeasePaths: []string{primaryPath, fallbackPath}})
	leases, err := handler.dhcpLeaseList()
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 {
		t.Fatalf("leases = %+v", leases)
	}
	if leases[0].Hostname != "SwitchBot-HubMini-48E09C" || leases[0].MAC != "8c:aa:b5:48:e0:9c" {
		t.Fatalf("lease = %+v, want primary lease", leases[0])
	}
}

func TestHandlerDHCPLeaseOrderIsOnlyTieBreakerAfterExpiry(t *testing.T) {
	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "primary", "dnsmasq.leases")
	fallbackPath := filepath.Join(dir, "fallback", "dnsmasq.leases")
	if err := os.MkdirAll(filepath.Dir(primaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(fallbackPath), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	primaryLease := fmt.Sprintf("%d 8c:aa:b5:48:e0:9c 172.18.1.109 old-primary *\n", now.Add(time.Hour).Unix())
	newerFallbackLease := fmt.Sprintf("%d bc:24:11:26:7b:ab 172.18.1.109 newer-fallback ff:ca:53:09:5a\n", now.Add(2*time.Hour).Unix())
	if err := os.WriteFile(primaryPath, []byte(primaryLease), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fallbackPath, []byte(newerFallbackLease), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := New(Options{DHCPLeasePaths: []string{primaryPath, fallbackPath}})
	leases, err := handler.dhcpLeaseList()
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 {
		t.Fatalf("leases = %+v", leases)
	}
	if leases[0].Hostname != "newer-fallback" || leases[0].MAC != "bc:24:11:26:7b:ab" {
		t.Fatalf("lease = %+v, want newer fallback lease", leases[0])
	}
}

func TestReadDnsmasqLeases(t *testing.T) {
	leasePath := filepath.Join(t.TempDir(), "dnsmasq.leases")
	if err := os.WriteFile(leasePath, []byte("1778014867 18:ec:e7:33:12:6c 172.18.0.150 aiseg2 01:18:ec:e7:33:12:6c\n1778014867 bc:24:11:e0:8e:3a 2409:10:3d60:1271::20 host6 00:03:00:01:bc:24:11:e0:8e:3a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	leases, err := readDnsmasqLeases(leasePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 2 {
		t.Fatalf("leases = %d", len(leases))
	}
	lease := leases[0]
	if lease.IP != "172.18.0.150" || lease.Hostname != "aiseg2" || lease.Vendor != "Panasonic" || lease.Family != "ipv4" {
		t.Fatalf("lease = %+v", lease)
	}
	if leases[1].IP != "2409:10:3d60:1271::20" || leases[1].Family != "ipv6" {
		t.Fatalf("ipv6 lease = %+v", leases[1])
	}
}

func TestParseWireGuardAllDump(t *testing.T) {
	rows, err := parseWireGuardAllDump([]byte("wg0\tprivate-key-must-not-leak\tinterface-public\t51820\toff\nwg0\tpeer-public\tpreshared-key-must-not-leak\t203.0.113.2:51820\t10.0.0.2/32,fd00::2/128\t1710000000\t100\t200\t25\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("interfaces = %d", len(rows))
	}
	if rows[0].Name != "wg0" || rows[0].PublicKey != "interface-public" || rows[0].ListenPort != 51820 {
		t.Fatalf("interface = %+v", rows[0])
	}
	if strings.Contains(fmt.Sprintf("%+v", rows), "private-key") || strings.Contains(fmt.Sprintf("%+v", rows), "preshared-key") {
		t.Fatalf("secret material leaked: %+v", rows)
	}
	if len(rows[0].Peers) != 1 || rows[0].Peers[0].PublicKey != "peer-public" || rows[0].Peers[0].TransferTxBytes != 200 || rows[0].Peers[0].PersistentKeepaliveSec != 25 {
		t.Fatalf("peer = %+v", rows[0].Peers)
	}
	if got := rows[0].Peers[0].AllowedIPs; len(got) != 2 || got[1] != "fd00::2/128" {
		t.Fatalf("allowed ips = %+v", got)
	}
	if rows[0].Peers[0].LatestHandshake.IsZero() {
		t.Fatalf("handshake not parsed: %+v", rows[0].Peers[0])
	}
}

func TestParseTailscaleStatusJSON(t *testing.T) {
	status, err := parseTailscaleStatusJSON([]byte(`{
	  "BackendState": "Running",
	  "CurrentTailnet": {"Name": "example@example.com", "MagicDNSSuffix": "example.ts.net", "MagicDNSEnabled": true},
	  "CertDomains": ["homert02.example.ts.net"],
	  "Self": {
	    "HostName": "homert02",
	    "DNSName": "homert02.example.ts.net.",
	    "TailscaleIPs": ["100.64.87.102", "fd7a:115c:a1e0::1"],
	    "AllowedIPs": ["100.64.87.102/32"],
	    "Online": true,
	    "ExitNodeOption": true
	  },
	  "Peer": {
	    "node-b": {"HostName": "phone", "Online": false, "LastSeen": "2026-05-07T10:00:00Z"},
	    "node-a": {"HostName": "laptop", "Online": true, "Active": true, "TailscaleIPs": ["100.64.0.2"], "Relay": "tok", "LastSeen": "2026-05-07T11:00:00Z", "RxBytes": 10, "TxBytes": 20}
	  }
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if status == nil || status.HostName != "homert02" || status.BackendState != "Running" || !status.Online {
		t.Fatalf("status = %+v", status)
	}
	if status.TailnetName != "example@example.com" || status.MagicDNSSuffix != "example.ts.net" || !status.MagicDNSEnabled || len(status.CertDomains) != 1 {
		t.Fatalf("tailnet fields = %+v", status)
	}
	if len(status.Peers) != 2 || status.Peers[0].HostName != "laptop" || !status.Peers[0].Active {
		t.Fatalf("peers not sorted/parsed: %+v", status.Peers)
	}
}

func TestHostCommandPathKeepsAbsolutePath(t *testing.T) {
	if got := hostCommandPath("/usr/local/bin/tailscale"); got != "/usr/local/bin/tailscale" {
		t.Fatalf("hostCommandPath = %q", got)
	}
}

func TestParseIPNeighborJSON(t *testing.T) {
	rows, err := parseIPNeighborJSON([]byte(`[
	  {"dst":"172.18.1.110","dev":"ens19","lladdr":"4e:20:15:aa:e0:67","state":["REACHABLE"]},
	  {"dst":"2409:10:3d60:1271::abcd","dev":"ens19","lladdr":"4e:20:15:aa:e0:67","state":["STALE"]}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].MAC != "4e:20:15:aa:e0:67" || rows[0].Vendor != "Apple private address" {
		t.Fatalf("row = %+v", rows[0])
	}
}

func TestParseFreeBSDNeighbors(t *testing.T) {
	arp := parseFreeBSDARP([]byte(`? (192.168.160.182) at bc:24:11:df:9e:c2 on vtnet1 expires in 1197 seconds [ethernet]
? (192.168.160.190) at (incomplete) on vtnet1 expired [ethernet]
`))
	if len(arp) != 1 || arp[0].IP != "192.168.160.182" || arp[0].IfName != "vtnet1" {
		t.Fatalf("arp = %+v", arp)
	}
	ndp := parseFreeBSDNDP([]byte(`Neighbor                             Linklayer Address  Netif Expire    S Flags
2409:10:3d60:1250::abcd             bc:24:11:df:9e:c2 vtnet1 23h59m37s S R
fe80::be24:11ff:fedf:9ec2%vtnet1    bc:24:11:df:9e:c2 vtnet1 permanent R
`))
	if len(ndp) != 2 || ndp[0].IP != "2409:10:3d60:1250::abcd" || ndp[1].IP != "fe80::be24:11ff:fedf:9ec2" {
		t.Fatalf("ndp = %+v", ndp)
	}
}

func TestCorrelateClientsMergesDHCPLeaseAndIPv6NeighborByMAC(t *testing.T) {
	rows := correlateClients(
		[]DHCPLease{{
			MAC:      "4e:20:15:aa:e0:67",
			IP:       "172.18.1.110",
			Hostname: "MacBookAir",
			Vendor:   "Apple private address",
		}},
		[]NeighborEntry{
			{MAC: "4e:20:15:aa:e0:67", IP: "172.18.1.110", IfName: "ens19", State: "REACHABLE", Source: "ip-neigh"},
			{MAC: "4e:20:15:aa:e0:67", IP: "2409:10:3d60:1271::abcd", IfName: "ens19", State: "STALE", Source: "ip-neigh"},
			{MAC: "4e:20:15:aa:e0:67", IP: "fe80::14c5:6fd7:b848:a739", IfName: "ens19", State: "STALE", Source: "ip-neigh"},
		},
		[]logstore.TrafficFlow{{ClientAddress: "2409:10:3d60:1271::abcd", PeerAddress: "2001:4860:4860::8888", Accounting: true, BytesOut: 120, BytesIn: 240}},
		[]logstore.DNSQuery{{ClientAddress: "2409:10:3d60:1271::abcd", QuestionName: "www.icloud.com", QuestionType: "AAAA"}},
		nil,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.MAC != "4e:20:15:aa:e0:67" || row.Hostname != "MacBookAir" {
		t.Fatalf("row identity = %+v", row)
	}
	for _, want := range []string{"172.18.1.110", "2409:10:3d60:1271::abcd", "fe80::14c5:6fd7:b848:a739"} {
		if !containsString(row.Addresses, want) {
			t.Fatalf("addresses missing %s: %+v", want, row.Addresses)
		}
	}
	if row.BytesOut != 120 || row.BytesIn != 240 {
		t.Fatalf("traffic was not joined to client: %+v", row)
	}
	if row.InferredOSFamily != "Apple" || row.FingerprintConfidence == 0 {
		t.Fatalf("fingerprint missing: %+v", row)
	}
}

func TestCorrelateClientsAddsDPIActivitySummary(t *testing.T) {
	now := time.Now().UTC()
	rows := correlateClients(
		nil,
		nil,
		[]logstore.TrafficFlow{
			{
				ClientAddress: "172.18.1.120",
				PeerAddress:   "93.184.216.34",
				AppName:       "tls",
				TLSSNI:        "example.com",
				Accounting:    true,
				BytesOut:      1600,
				BytesIn:       6400,
				EndedAt:       now.Add(-2 * time.Minute),
			},
			{
				ClientAddress:    "172.18.1.120",
				PeerAddress:      "1.1.1.1",
				AppName:          "dns",
				ResolvedHostname: "one.one.one.one",
				Accounting:       true,
				BytesOut:         120,
				BytesIn:          240,
				EndedAt:          now,
			},
		},
		nil,
		nil,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.PrimaryActivity != "web-heavy" {
		t.Fatalf("primary activity = %q, row = %+v", row.PrimaryActivity, row)
	}
	if row.LastProtocol != "dns" || row.LastProtocolDetail != "DNS-query=one.one.one.one" {
		t.Fatalf("last protocol = %q detail = %q", row.LastProtocol, row.LastProtocolDetail)
	}
	for _, want := range []string{"tls", "dns"} {
		if !containsString(row.ProtocolMix, want) {
			t.Fatalf("protocol mix missing %q: %+v", want, row.ProtocolMix)
		}
	}
}

func TestCorrelateClientsSkipsFailedNeighbors(t *testing.T) {
	rows := correlateClients(nil, []NeighborEntry{
		{IP: "192.168.178.40", IfName: "ens18", State: "FAILED", Source: "ip-neigh"},
		{IP: "172.18.1.110", IfName: "ens19", MAC: "4e:20:15:aa:e0:67", State: "REACHABLE", Source: "ip-neigh"},
	}, nil, nil, nil)
	if len(rows) != 1 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	if containsString(rows[0].Addresses, "192.168.178.40") {
		t.Fatalf("failed neighbor leaked into clients: %+v", rows[0])
	}
	if !containsString(rows[0].Addresses, "172.18.1.110") {
		t.Fatalf("reachable neighbor missing from clients: %+v", rows[0])
	}
}

func TestCorrelateClientsGroupsPrivacyAddressByFingerprintWhenUnique(t *testing.T) {
	rows := correlateClients(
		[]DHCPLease{{
			MAC:      "4e:20:15:aa:e0:67",
			IP:       "172.18.1.110",
			Hostname: "mcberry-iPhone",
			Vendor:   "Apple private address",
		}},
		nil,
		[]logstore.TrafficFlow{{ClientAddress: "2409:10:3d60:1271:abcd::1234", PeerAddress: "17.253.144.10", ResolvedHostname: "gateway.icloud.com"}},
		[]logstore.DNSQuery{{ClientAddress: "2409:10:3d60:1271:abcd::1234", QuestionName: "gateway.icloud.com", QuestionType: "AAAA"}},
		nil,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	if !containsString(rows[0].Addresses, "2409:10:3d60:1271:abcd::1234") {
		t.Fatalf("privacy address not grouped: %+v", rows[0])
	}
	if rows[0].InferredOSFamily != "Apple" || rows[0].InferredDeviceClass != "phone" {
		t.Fatalf("fingerprint = %+v", rows[0])
	}
}

func TestCorrelateClientsKeepsDeviceClassWithinInferredOSFamily(t *testing.T) {
	rows := correlateClients(
		[]DHCPLease{{
			MAC:      "66:1f:8c:8d:0d:58",
			IP:       "172.18.1.133",
			Hostname: "iPhone",
		}},
		nil,
		nil,
		[]logstore.DNSQuery{
			{ClientAddress: "172.18.1.133", QuestionName: "www.icloud.com", QuestionType: "A"},
			{ClientAddress: "172.18.1.133", QuestionName: "login.microsoft.com", QuestionType: "A"},
			{ClientAddress: "172.18.1.133", QuestionName: "office365.com", QuestionType: "A"},
		},
		nil,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	if rows[0].InferredOSFamily != "Apple" || rows[0].InferredDeviceClass != "phone" {
		t.Fatalf("mixed signals should keep iPhone as Apple phone: %+v", rows[0])
	}
}

func TestCorrelateClientsInfersGamingConsoleDNSFamilies(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		family string
	}{
		{name: "nintendo", query: "api.lp1.av5ja.srv.nintendo.net", family: "nintendo"},
		{name: "npln", query: "matchmaking.npln.jp", family: "nintendo"},
		{name: "playstation", query: "auth.api.playstation.net", family: "playstation"},
		{name: "xbox", query: "user.auth.xboxlive.com", family: "xbox"},
		{name: "steam", query: "cdn.steamcontent.com", family: "steam-os"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := correlateClients(
				nil,
				nil,
				nil,
				[]logstore.DNSQuery{{ClientAddress: "172.18.1.150", QuestionName: tt.query, QuestionType: "A"}},
				nil,
			)
			if len(rows) != 1 {
				t.Fatalf("rows = %d: %+v", len(rows), rows)
			}
			if rows[0].InferredOSFamily != tt.family || rows[0].InferredDeviceClass != "gaming-console" {
				t.Fatalf("fingerprint = %+v, want family=%s class=gaming-console", rows[0], tt.family)
			}
		})
	}
}

func TestCorrelateClientsNintendoDNSBeatsGenericAppleUsage(t *testing.T) {
	rows := correlateClients(
		nil,
		nil,
		[]logstore.TrafficFlow{{ClientAddress: "172.18.1.151", ResolvedHostname: "gateway.icloud.com"}},
		[]logstore.DNSQuery{
			{ClientAddress: "172.18.1.151", QuestionName: "accounts.nintendo.com", QuestionType: "A"},
			{ClientAddress: "172.18.1.151", QuestionName: "gateway.icloud.com", QuestionType: "AAAA"},
		},
		nil,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	if rows[0].InferredOSFamily != "nintendo" || rows[0].InferredDeviceClass != "gaming-console" {
		t.Fatalf("Nintendo signal should win over generic Apple usage: %+v", rows[0])
	}
	if !containsString(rows[0].FingerprintSignals, "dns/nintendo:nintendo.com") {
		t.Fatalf("Nintendo signal missing: %+v", rows[0].FingerprintSignals)
	}
}

func TestHandlerClientSnapshotIgnoresStaleIPBasedFingerprints(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	leasePath := filepath.Join(dir, "dnsmasq.leases")
	if err := os.WriteFile(leasePath, []byte(fmt.Sprintf("%d 3c:a9:ab:0b:40:07 172.18.1.150 * *\n", now.Add(12*time.Hour).Unix())), 0o644); err != nil {
		t.Fatal(err)
	}

	queryLog := filepath.Join(dir, "dns-queries.db")
	dnsLog, err := logstore.OpenDNSQueryLog(queryLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := dnsLog.Record(reqContext(), logstore.DNSQuery{Timestamp: now.Add(-90 * time.Minute), ClientAddress: "172.18.1.150", QuestionName: "app.lp1.five.nintendo.net", QuestionType: "A"}); err != nil {
		t.Fatal(err)
	}
	_ = dnsLog.Close()

	trafficLog := filepath.Join(dir, "traffic-flows.db")
	traffic, err := logstore.OpenTrafficFlowLog(trafficLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := traffic.UpsertActive(context.Background(), logstore.TrafficFlow{StartedAt: now.Add(-90 * time.Minute), ClientAddress: "172.18.1.150", PeerAddress: "203.0.113.10", Protocol: "tcp", TLSSNI: "receive.p01.lp1.dg.srv.nintendo.net"}); err != nil {
		t.Fatal(err)
	}
	_ = traffic.Close()

	firewallLogPath := filepath.Join(dir, "firewall-logs.db")
	firewallLog, err := logstore.OpenFirewallLog(firewallLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := firewallLog.Record(context.Background(), logstore.FirewallLogEntry{Timestamp: now.Add(-90 * time.Minute), Action: "drop", SrcAddress: "172.18.1.150", DstAddress: "203.0.113.10", Protocol: "tcp", L3Proto: "ipv4", DPITLSSNI: "receive.p01.lp1.dg.srv.nintendo.net", DPIApp: "tls", DPICategory: "web", DPIConfidence: 90}); err != nil {
		t.Fatal(err)
	}
	_ = firewallLog.Close()

	fingerprintLogPath := filepath.Join(dir, "dhcp-fingerprints.db")
	fingerprintLog, err := logstore.OpenDHCPFingerprintLog(fingerprintLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := fingerprintLog.Upsert(context.Background(), logstore.DHCPFingerprint{MAC: "3c:a9:ab:0b:40:07", Hostname: "NintendoSwitch", OSFamily: "nintendo", DeviceClass: "gaming-console", Confidence: 95, Signal: "dhcp-fingerprint/nintendo", ObservedAt: now.Add(-90 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	_ = fingerprintLog.Close()

	handler := New(Options{
		DNSQueryLogPath:        queryLog,
		TrafficFlowLogPath:     trafficLog,
		FirewallLogPath:        firewallLogPath,
		DHCPFingerprintLogPath: fingerprintLogPath,
		DHCPLeasePaths:         []string{leasePath},
	})
	snapshot := handler.Snapshot(SnapshotOptions{
		EventLimit:            -1,
		ConnectionsLimit:      -1,
		FirewallLimit:         -1,
		DNSQueryLimit:         -1,
		TrafficFlowLimit:      200,
		FingerprintQueryLimit: 1000,
		DHCPFingerprintLimit:  1000,
		IncludeDPIEnrichment:  true,
		IncludeClients:        true,
		IncludeVPN:            false,
		SkipResources:         true,
	})
	var row *ClientEntry
	for i := range snapshot.Clients {
		if strings.EqualFold(snapshot.Clients[i].MAC, "3c:a9:ab:0b:40:07") {
			row = &snapshot.Clients[i]
			break
		}
	}
	if row == nil {
		t.Fatalf("client not found: %+v", snapshot.Clients)
	}
	if row.InferredOSFamily == "nintendo" || containsString(row.FingerprintSignals, "dns/nintendo:nintendo.net") || containsString(row.FingerprintSignals, "dhcp-fingerprint/nintendo") {
		t.Fatalf("stale IP-based fingerprint leaked into current client: %+v", row)
	}
	if row.Vendor != "Apple" {
		t.Fatalf("current lease vendor = %q, want Apple", row.Vendor)
	}
}

func TestCorrelateClientsStrongHostnameBeatsRepeatedGamingDNS(t *testing.T) {
	rows := correlateClients(
		[]DHCPLease{{
			MAC:      "76:ed:c9:67:89:e5",
			IP:       "172.18.1.177",
			Hostname: "Pixel-10",
		}},
		[]NeighborEntry{{MAC: "76:ed:c9:67:89:e5", IP: "172.18.1.178", State: "REACHABLE", Source: "ip-neigh"}},
		nil,
		[]logstore.DNSQuery{
			{ClientAddress: "172.18.1.177", QuestionName: "api-lp1.znc.srv.nintendo.net", QuestionType: "A"},
			{ClientAddress: "172.18.1.178", QuestionName: "api-lp1.znc.srv.nintendo.net", QuestionType: "A"},
			{ClientAddress: "172.18.1.177", QuestionName: "android.clients.google.com", QuestionType: "A"},
			{ClientAddress: "172.18.1.178", QuestionName: "www.googleapis.com", QuestionType: "A"},
		},
		nil,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	if rows[0].InferredOSFamily != "Android" || rows[0].InferredDeviceClass != "phone" {
		t.Fatalf("strong Android hostname should win over repeated Nintendo usage: %+v", rows[0])
	}
}

func TestCorrelateClientsInfersHomeAndOfficeDeviceSignals(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		family string
		class  string
	}{
		{name: "amazon echo", query: "device-metrics.amazonalexa.com", family: "iot", class: "smart-speaker"},
		{name: "chromecast", query: "clients3.google.com", family: "iot", class: "smart-tv"},
		{name: "roku", query: "api.roku.com", family: "iot", class: "smart-tv"},
		{name: "switchbot", query: "api.switchbot.com", family: "iot", class: "iot"},
		{name: "hue", query: "discovery.meethue.com", family: "iot", class: "lighting"},
		{name: "ring", query: "api.ring.com", family: "iot", class: "camera"},
		{name: "roomba", query: "prod.irobotapi.com", family: "iot", class: "vacuum"},
		{name: "sonos", query: "update.sonos.com", family: "iot", class: "smart-speaker"},
		{name: "synology", query: "global.quickconnect.to", family: "nas", class: "nas"},
		{name: "qnap", query: "update.qnap.com", family: "nas", class: "nas"},
		{name: "hp printer", query: "www.hpconnected.com", family: "printer", class: "printer"},
		{name: "epson printer", query: "printer.epsonconnect.com", family: "printer", class: "printer"},
		{name: "yealink", query: "rps.yealink.com", family: "voip", class: "voip"},
		{name: "samsung", query: "dc.di.atlas.samsungcloud.com", family: "Android", class: "phone"},
		{name: "xiaomi", query: "api.io.mi.com", family: "Android", class: "phone"},
		{name: "tesla", query: "owner-api.teslamotors.com", family: "iot", class: "ev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := correlateClients(
				nil,
				nil,
				nil,
				[]logstore.DNSQuery{{ClientAddress: "172.18.1.160", QuestionName: tt.query, QuestionType: "A"}},
				nil,
			)
			if len(rows) != 1 {
				t.Fatalf("rows = %d: %+v", len(rows), rows)
			}
			if rows[0].InferredOSFamily != tt.family || rows[0].InferredDeviceClass != tt.class {
				t.Fatalf("fingerprint = %+v, want family=%s class=%s", rows[0], tt.family, tt.class)
			}
		})
	}
}

func TestCorrelateClientsInfersHomeDeviceHostnames(t *testing.T) {
	rows := correlateClients(
		[]DHCPLease{{
			MAC:      "00:f6:20:74:00:bc",
			IP:       "172.18.1.70",
			Hostname: "Google-Nest-Mini",
			Vendor:   "Google",
		}},
		nil,
		nil,
		nil,
		nil,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	if rows[0].InferredOSFamily != "iot" || rows[0].InferredDeviceClass != "smart-speaker" {
		t.Fatalf("Google Nest should be an IoT smart speaker: %+v", rows[0])
	}
}

func TestCorrelateClientsWeakGenericCloudSignalDoesNotForceIdentity(t *testing.T) {
	rows := correlateClients(
		nil,
		nil,
		nil,
		[]logstore.DNSQuery{{ClientAddress: "172.18.1.161", QuestionName: "abcd.execute-api.us-east-1.amazonaws.com", QuestionType: "A"}},
		nil,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	if rows[0].InferredOSFamily != "" || rows[0].InferredDeviceClass != "" {
		t.Fatalf("generic cloud DNS should stay unknown: %+v", rows[0])
	}
}

func TestCorrelateClientsDHCPFingerprintBeatsGenericDNS(t *testing.T) {
	rows := correlateClients(
		[]DHCPLease{{
			MAC:      "aa:bb:cc:dd:ee:ff",
			IP:       "172.18.1.180",
			Hostname: "desktop",
		}},
		nil,
		nil,
		[]logstore.DNSQuery{
			{ClientAddress: "172.18.1.180", QuestionName: "gateway.icloud.com", QuestionType: "A"},
			{ClientAddress: "172.18.1.180", QuestionName: "www.googleapis.com", QuestionType: "A"},
		},
		nil,
		[]logstore.DHCPFingerprint{{
			MAC:              "aa:bb:cc:dd:ee:ff",
			Hostname:         "desktop",
			VendorClass:      "MSFT 5.0",
			RequestedOptions: []int{1, 15, 3, 6, 44, 46, 47, 31, 33, 249, 43},
			OSFamily:         "Windows",
			DeviceClass:      "computer",
			Confidence:       90,
			Signal:           "dhcp-fingerprint/windows-vendor",
			ObservedAt:       time.Now().UTC(),
		}},
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	if rows[0].InferredOSFamily != "Windows" || rows[0].InferredDeviceClass != "computer" {
		t.Fatalf("DHCP fingerprint should beat generic DNS: %+v", rows[0])
	}
	if !containsString(rows[0].FingerprintSignals, "dhcp-fingerprint/windows-vendor") {
		t.Fatalf("DHCP signal missing: %+v", rows[0].FingerprintSignals)
	}
}

func TestCorrelateClientsGoogleMediaBeatsGenericAndroidUsage(t *testing.T) {
	rows := correlateClients(
		nil,
		nil,
		nil,
		[]logstore.DNSQuery{
			{ClientAddress: "172.18.1.162", QuestionName: "www.googleapis.com", QuestionType: "A"},
			{ClientAddress: "172.18.1.162", QuestionName: "connectivitycheck.gstatic.com", QuestionType: "A"},
			{ClientAddress: "172.18.1.162", QuestionName: "foo.l.google.com", QuestionType: "A"},
			{ClientAddress: "172.18.1.162", QuestionName: "foo.l.google.com", QuestionType: "AAAA"},
		},
		nil,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	if rows[0].InferredOSFamily != "iot" || rows[0].InferredDeviceClass != "smart-tv" {
		t.Fatalf("specific Google media signal should beat generic Android DNS: %+v", rows[0])
	}
}

func TestCorrelateClientsExpandedOUIAndIOTSignals(t *testing.T) {
	tests := []struct {
		name   string
		mac    string
		host   string
		query  string
		family string
		class  string
	}{
		{name: "nintendo oui", mac: "04:03:d6:12:34:56", query: "gateway.icloud.com", family: "nintendo", class: "gaming-console"},
		{name: "ecoflow oui", mac: "64:e8:33:12:34:56", family: "iot", class: "iot"},
		{name: "atom tech oui", mac: "7c:dd:e9:12:34:56", family: "iot", class: "iot"},
		{name: "tuya dns", mac: "aa:bb:cc:12:34:56", query: "a1.tuyaus.com", family: "iot", class: "iot"},
		{name: "tplink dns", mac: "aa:bb:cc:12:34:57", query: "use1-wap.tplinkcloud.com", family: "iot", class: "iot"},
		{name: "bravia hostname", mac: "00:01:4a:12:34:56", host: "BRAVIA-4K", query: "auth.api.sonyentertainmentnetwork.com", family: "iot", class: "smart-tv"},
		{name: "nest doorbell", mac: "d8:eb:46:12:34:56", host: "Nest-Doorbell-Battery", query: "www.googleapis.com", family: "iot", class: "camera"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var queries []logstore.DNSQuery
			if tt.query != "" {
				queries = append(queries, logstore.DNSQuery{ClientAddress: "172.18.1.190", QuestionName: tt.query, QuestionType: "A"})
			}
			rows := correlateClients(
				[]DHCPLease{{
					MAC:      tt.mac,
					IP:       "172.18.1.190",
					Hostname: tt.host,
					Vendor:   macVendor(tt.mac),
				}},
				nil,
				nil,
				queries,
				nil,
			)
			if len(rows) != 1 {
				t.Fatalf("rows = %d: %+v", len(rows), rows)
			}
			if rows[0].InferredOSFamily != tt.family || rows[0].InferredDeviceClass != tt.class {
				t.Fatalf("fingerprint = %+v, want family=%s class=%s", rows[0], tt.family, tt.class)
			}
		})
	}
}

func TestHandlerServesConfigReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: routerd.net/v1alpha1\nkind: Router\n"), 0644); err != nil {
		t.Fatal(err)
	}
	handler := New(Options{ConfigPath: path})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{path, "apiVersion: routerd.net/v1alpha1", "kind: Router"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("config response missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func TestHandlerRedactsAllKnownConfigSecretsAcrossEndpoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.yaml")
	config := redactionRegressionConfig("redaction-lab", "homert02")
	if err := os.WriteFile(path, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, err := store.BeginGeneration("redaction-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordGenerationConfig(first, config); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishGeneration(first, "Healthy", nil); err != nil {
		t.Fatal(err)
	}
	secondConfig := redactionRegressionConfig("redaction-lab-next", "homert03")
	second, err := store.BeginGeneration("redaction-b")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordGenerationConfig(second, secondConfig); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishGeneration(second, "Healthy", nil); err != nil {
		t.Fatal(err)
	}

	handler := New(Options{ConfigPath: path, Store: store})
	sentinels := redactionRegressionSentinels()
	secretKeys := []string{
		"password",
		"passwordFile",
		"privateKey",
		"privateKeyFile",
		"presharedKey",
		"presharedKeyFile",
		"authKey",
		"authKeyEnv",
		"authKeyFile",
		"preSharedKey",
		"passwordFrom",
		"initialPassword",
	}
	publicValues := []string{
		"hostname: homert02",
		"interface: wan0",
		"interface: lan0",
		"address: 192.0.2.1/24",
		"publicKey: wg-public-key",
		"peerASN: 64501",
		"port: 8443",
	}

	t.Run("current config JSON snapshot", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/config", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
		}
		assertNoSentinels(t, rec.Body.String(), sentinels)
		var snapshot ConfigSnapshot
		if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		assertRedactedConfigText(t, snapshot.Text, sentinels, secretKeys, publicValues)
	})

	t.Run("generation config YAML", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/generations/%d/config", first), nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
		}
		assertRedactedConfigText(t, rec.Body.String(), sentinels, secretKeys, publicValues)
	})

	t.Run("generation diff text", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/generations/%d/diff/%d", first, second), nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		assertRedactedConfigText(t, body, sentinels, secretKeys, publicValues)
		for _, want := range []string{"--- generation-1.yaml", "+++ generation-2.yaml", "-    name: redaction-lab", "+    name: redaction-lab-next", "+    hostname: homert03"} {
			if !strings.Contains(body, want) {
				t.Fatalf("diff missing %q:\n%s", want, body)
			}
		}
	})

	t.Run("generation diff JSON", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/generations/%d/diff/%d", first, second), nil)
		req.Header.Set("Accept", "application/json")
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
		}
		assertNoSentinels(t, rec.Body.String(), sentinels)
		var diff GenerationDiff
		if err := json.Unmarshal(rec.Body.Bytes(), &diff); err != nil {
			t.Fatal(err)
		}
		assertRedactedConfigText(t, diff.Diff, sentinels, secretKeys, publicValues)
	})
}

func redactionRegressionConfig(name, hostname string) string {
	return fmt.Sprintf(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: %s
spec:
  hostname: %s
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: PPPoESession
      metadata:
        name: pppoe-wan
      spec:
        interface: wan0
        username: isp-user
        password: REDACTION_SENTINEL_pppoe_password
        passwordFile: REDACTION_SENTINEL_pppoe_passwordFile
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata:
        name: wg0
      spec:
        privateKey: REDACTION_SENTINEL_wireguard_privateKey
        privateKeyFile: REDACTION_SENTINEL_wireguard_privateKeyFile
        listenPort: 51820
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardPeer
      metadata:
        name: peer-a
      spec:
        interface: wg0
        publicKey: wg-public-key
        presharedKey: REDACTION_SENTINEL_wireguard_peer_presharedKey
        presharedKeyFile: REDACTION_SENTINEL_wireguard_peer_presharedKeyFile
        allowedIPs:
          - 192.0.2.2/32
    - apiVersion: net.routerd.net/v1alpha1
      kind: TailscaleNode
      metadata:
        name: ts
      spec:
        hostname: %s
        authKey: REDACTION_SENTINEL_tailscale_authKey
        authKeyEnv: REDACTION_SENTINEL_tailscale_authKeyEnv
        authKeyFile: REDACTION_SENTINEL_tailscale_authKeyFile
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPsecConnection
      metadata:
        name: site-a
      spec:
        localAddress: 203.0.113.2
        remoteAddress: 198.51.100.2
        preSharedKey: REDACTION_SENTINEL_ipsec_preSharedKey
        leftSubnet: 192.0.2.0/24
        rightSubnet: 198.51.100.0/24
    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPPeer
      metadata:
        name: upstream
      spec:
        routerRef: edge
        peerASN: 64501
        peers:
          - 198.51.100.1
        password: REDACTION_SENTINEL_bgp_password
        passwordFrom:
          file: REDACTION_SENTINEL_bgp_passwordFrom_file
          env: REDACTION_SENTINEL_bgp_passwordFrom_env
    - apiVersion: system.routerd.net/v1alpha1
      kind: WebConsole
      metadata:
        name: console
      spec:
        listenAddress: 127.0.0.1
        port: 8443
        initialPassword: REDACTION_SENTINEL_webconsole_initialPassword
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata:
        name: lan-ip
      spec:
        interface: lan0
        address: 192.0.2.1/24
`, name, hostname, hostname)
}

func redactionRegressionSentinels() []string {
	return []string{
		"REDACTION_SENTINEL_pppoe_password",
		"REDACTION_SENTINEL_pppoe_passwordFile",
		"REDACTION_SENTINEL_wireguard_privateKey",
		"REDACTION_SENTINEL_wireguard_privateKeyFile",
		"REDACTION_SENTINEL_wireguard_peer_presharedKey",
		"REDACTION_SENTINEL_wireguard_peer_presharedKeyFile",
		"REDACTION_SENTINEL_tailscale_authKey",
		"REDACTION_SENTINEL_tailscale_authKeyEnv",
		"REDACTION_SENTINEL_tailscale_authKeyFile",
		"REDACTION_SENTINEL_ipsec_preSharedKey",
		"REDACTION_SENTINEL_bgp_password",
		"REDACTION_SENTINEL_bgp_passwordFrom_file",
		"REDACTION_SENTINEL_bgp_passwordFrom_env",
		"REDACTION_SENTINEL_webconsole_initialPassword",
	}
}

func assertRedactedConfigText(t *testing.T, text string, sentinels, secretKeys, publicValues []string) {
	t.Helper()
	assertNoSentinels(t, text, sentinels)
	for _, key := range secretKeys {
		if !strings.Contains(text, key+":") {
			t.Fatalf("redacted config missing key %q:\n%s", key, text)
		}
	}
	if !strings.Contains(text, api.RedactedSecret) {
		t.Fatalf("redacted config missing %q:\n%s", api.RedactedSecret, text)
	}
	for _, want := range publicValues {
		if !strings.Contains(text, want) {
			t.Fatalf("redacted config missing public value %q:\n%s", want, text)
		}
	}
}

func assertNoSentinels(t *testing.T, text string, sentinels []string) {
	t.Helper()
	for _, sentinel := range sentinels {
		if strings.Contains(text, sentinel) {
			t.Fatalf("response leaked %q:\n%s", sentinel, text)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestHandlerRendersUsableBasePath(t *testing.T) {
	handler := New(Options{BasePath: "/", Title: "homert02"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`window.__ROUTERD_WEB_CONSOLE__`, `basePath: "/"`, `title: "homert02"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("index missing %q:\n%s", want, body)
		}
	}
}

func TestHandlerRejectsLegacyAPIPaths(t *testing.T) {
	handler := New(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/summary", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandlerServesEmbeddedAssets(t *testing.T) {
	assets, err := fs.Glob(staticFiles, "static/assets/*.css")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) == 0 {
		t.Fatal("embedded web console css asset not found")
	}
	handler := New(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/"+strings.TrimPrefix(assets[0], "static/"), nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
