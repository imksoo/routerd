// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/logstore"
)

func TestStatusHandler(t *testing.T) {
	handler := Handler{
		Status: func(r *http.Request) (*Status, error) {
			status := NewStatus(&apply.Result{Phase: "Healthy", Generation: 42})
			return &status, nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, Prefix+"/status", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"apiVersion": "control.routerd.net/v1alpha1"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"phase": "Healthy"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestInspectionReadHandlers(t *testing.T) {
	handler := Handler{
		Get: func(r *http.Request, req GetRequest) (*GetResult, error) {
			if req.Subject != "events" || req.Limit != 7 || req.SinceID != 12 || req.Topic != "routerd.test" || req.Resource != "Interface/wan" || req.KindFilter != "Interface" || req.NameFilter != "wan" {
				t.Fatalf("get request = %+v", req)
			}
			result := NewGetResult(req.Subject)
			return &result, nil
		},
		Describe: func(r *http.Request, req DescribeRequest) (*DescribeResult, error) {
			if req.Target != "Interface/wan" || req.EventsLimit != 3 {
				t.Fatalf("describe request = %+v", req)
			}
			result := NewDescribeResult(req.Target, ResourceView{APIVersion: "net.routerd.net/v1alpha1", Kind: "Interface", Name: "wan"})
			return &result, nil
		},
		Probe: func(r *http.Request, req ProbeRequest) (*ProbeResult, error) {
			if req.Subject != "egress" || req.Target != "ipv4-default" {
				t.Fatalf("probe request = %+v", req)
			}
			result := NewProbeResult(req.Subject, req.Target, []ProbeCheck{{Name: "EgressRoutePolicy/ipv4-default", Status: "pass"}})
			return &result, nil
		},
	}
	for _, path := range []string{
		Prefix + "/get?subject=events&limit=7&since-id=12&topic=routerd.test&resource=Interface/wan&kind=Interface&name=wan",
		Prefix + "/describe?target=Interface/wan&events-limit=3",
		Prefix + "/probe?subject=egress&target=ipv4-default",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status code = %d, body = %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestApplyHandler(t *testing.T) {
	handler := Handler{
		Apply: func(r *http.Request, req ApplyRequest) (*ApplyResult, error) {
			if strings.TrimSpace(req.CandidateYAML) == "" {
				t.Fatal("CandidateYAML is empty")
			}
			if !req.Replace || !req.NoReconcile {
				t.Fatalf("replace/noReconcile = %t/%t, want true/true", req.Replace, req.NoReconcile)
			}
			result := NewApplyResult(&apply.Result{Phase: "Healthy", Generation: 7})
			return &result, nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, Prefix+"/apply", strings.NewReader(`{"apiVersion":"control.routerd.net/v1alpha1","kind":"ApplyRequest","candidateYaml":"kind: Router\n","replace":true,"noReconcile":true}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"kind": "ApplyResult"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestPlanHandler(t *testing.T) {
	handler := Handler{
		Plan: func(r *http.Request, req PlanRequest) (*PlanResult, error) {
			if !req.Replace || !strings.Contains(req.CandidateYAML, "Router") {
				t.Fatalf("request = %+v", req)
			}
			result := NewPlanResult(&apply.Result{Phase: "Healthy", Generation: 0})
			return &result, nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, Prefix+"/plan", strings.NewReader(`{"apiVersion":"control.routerd.net/v1alpha1","kind":"PlanRequest","candidateYaml":"kind: Router\n","replace":true}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"kind": "PlanResult"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestDeleteHandler(t *testing.T) {
	handler := Handler{
		Delete: func(r *http.Request, req DeleteRequest) (*DeleteResult, error) {
			if req.Target != "pd/wan-pd" {
				t.Fatalf("target = %q", req.Target)
			}
			if !req.DryRun {
				t.Fatal("DryRun = false, want true")
			}
			return &DeleteResult{
				TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "DeleteResult"},
				Deleted:  []string{"net.routerd.net/v1alpha1/DHCPv6PrefixDelegation/wan-pd"},
				DryRun:   true,
			}, nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, Prefix+"/delete", strings.NewReader(`{"apiVersion":"control.routerd.net/v1alpha1","kind":"DeleteRequest","target":"pd/wan-pd","dryRun":true}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"kind": "DeleteResult"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestValidateHandler(t *testing.T) {
	handler := Handler{
		Validate: func(r *http.Request, req ValidateRequest) (*ValidateResult, error) {
			if !strings.Contains(req.CandidateYAML, "Router") {
				t.Fatalf("candidateYaml = %q", req.CandidateYAML)
			}
			result := NewValidateResult(true, []string{"warn"}, "")
			return &result, nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, Prefix+"/validate", strings.NewReader(`{"apiVersion":"control.routerd.net/v1alpha1","kind":"ValidateRequest","candidateYaml":"kind: Router\n"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"kind": "ValidateResult"`) || !strings.Contains(rec.Body.String(), `"valid": true`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestSubmitSAMEnrollmentClaimHandler(t *testing.T) {
	handler := Handler{
		SubmitSAMEnrollmentClaim: func(r *http.Request, req SAMEnrollmentClaimSubmitRequest) (*SAMEnrollmentClaimSubmitResult, error) {
			if req.Claim.APIVersion != api.MobilityAPIVersion || req.Claim.Kind != "SAMEnrollmentClaim" || req.Claim.Metadata.Name != "leaf-a" {
				t.Fatalf("claim = %#v", req.Claim)
			}
			now := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
			result := NewSAMEnrollmentClaimSubmitResult("SAMEnrollmentClaim/leaf-a", "SAMEnrollmentClaim/leaf-a", 1, now, now.Add(time.Minute))
			return &result, nil
		},
	}
	body := `{"apiVersion":"control.routerd.net/v1alpha1","kind":"SAMEnrollmentClaimSubmitRequest","claim":{"apiVersion":"mobility.routerd.net/v1alpha1","kind":"SAMEnrollmentClaim","metadata":{"name":"leaf-a"},"spec":{"policyRef":"SAMEnrollmentPolicy/leaves","leafID":"leaf-a","tunnelAddress":"10.255.0.21/32"}}}`
	req := httptest.NewRequest(http.MethodPost, Prefix+"/sam-enrollment-claims", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"kind": "SAMEnrollmentClaimSubmitResult"`) || !strings.Contains(rec.Body.String(), `"accepted": true`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestSetLogLevelHandler(t *testing.T) {
	handler := Handler{
		SetLogLevel: func(r *http.Request, req LogLevelRequest) (*LogLevelResult, error) {
			if req.Level != "debug" {
				t.Fatalf("level = %q, want debug", req.Level)
			}
			result := NewLogLevelResult(req.Level)
			return &result, nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, Prefix+"/log-level", strings.NewReader(`{"apiVersion":"control.routerd.net/v1alpha1","kind":"LogLevelRequest","level":"debug"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"kind": "LogLevelResult"`) || !strings.Contains(rec.Body.String(), `"level": "debug"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestSetLogLevelHandlerRejectsAPIVersion(t *testing.T) {
	called := false
	handler := Handler{
		SetLogLevel: func(r *http.Request, req LogLevelRequest) (*LogLevelResult, error) {
			called = true
			result := NewLogLevelResult(req.Level)
			return &result, nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, Prefix+"/log-level", strings.NewReader(`{"apiVersion":"example.invalid/v1","kind":"LogLevelRequest","level":"debug"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("handler was called for invalid apiVersion")
	}
}

func TestRuntimeHandler(t *testing.T) {
	handler := Handler{
		Runtime: func(r *http.Request) (*RuntimeStats, error) {
			stats := NewRuntimeStats()
			stats.HeapAllocBytes = 9 * 1024 * 1024
			stats.NumGoroutine = 33
			stats.OpenFDs = 12
			stats.MaxFDs = 1024
			return &stats, nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, Prefix+"/runtime", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"kind": "RuntimeStats"`) {
		t.Fatalf("body missing RuntimeStats kind: %s", body)
	}
	if !strings.Contains(body, `"numGoroutine": 33`) || !strings.Contains(body, `"openFds": 12`) {
		t.Fatalf("body missing runtime fields: %s", body)
	}
}

func TestRuntimeHandlerNotConfigured(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, Prefix+"/runtime", nil)
	rec := httptest.NewRecorder()

	Handler{}.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestSetLogLevelHandlerNotConfigured(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, Prefix+"/log-level", strings.NewReader(`{"apiVersion":"control.routerd.net/v1alpha1","kind":"LogLevelRequest","level":"debug"}`))
	rec := httptest.NewRecorder()

	Handler{}.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestConnectionsHandler(t *testing.T) {
	handler := Handler{
		Connections: func(r *http.Request, req ConnectionsRequest) (*ConnectionTable, error) {
			if req.Limit != 10 {
				t.Fatalf("limit = %d, want 10", req.Limit)
			}
			table := NewConnectionTable(nil)
			table.Status.Count = 3
			return &table, nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, Prefix+"/connections?limit=10", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"kind": "ConnectionTable"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestLogHandlers(t *testing.T) {
	handler := Handler{
		DNSQueries: func(r *http.Request, req DNSQueriesRequest) (*DNSQueries, error) {
			if req.Since != "2h" || req.Client != "172.18.0.10" || req.QName != "example%" || req.Limit != 7 {
				t.Fatalf("dns query request = %+v", req)
			}
			result := NewDNSQueries([]logstore.DNSQuery{{ClientAddress: req.Client, QuestionName: "example.com", QuestionType: "A"}})
			return &result, nil
		},
		TrafficFlows: func(r *http.Request, req TrafficFlowsRequest) (*TrafficFlows, error) {
			if req.Peer != "1.1.1.1" || req.Limit != 100 {
				t.Fatalf("traffic flow request = %+v", req)
			}
			result := NewTrafficFlows([]logstore.TrafficFlow{{ClientAddress: "172.18.0.10", PeerAddress: req.Peer, Protocol: "tcp"}})
			return &result, nil
		},
		FirewallLogs: func(r *http.Request, req FirewallLogsRequest) (*FirewallLogs, error) {
			if req.Action != "drop" || req.Src != "172.18.0.10" {
				t.Fatalf("firewall log request = %+v", req)
			}
			result := NewFirewallLogs([]logstore.FirewallLogEntry{{Action: "drop", SrcAddress: req.Src, DstAddress: "198.51.100.1", Protocol: "tcp"}})
			return &result, nil
		},
	}
	for _, tt := range []struct {
		path string
		want string
	}{
		{Prefix + "/dns-queries?since=2h&client=172.18.0.10&qname=example%25&limit=7", "DNSQueries"},
		{Prefix + "/traffic-flows?peer=1.1.1.1", "TrafficFlows"},
		{Prefix + "/firewall-logs?action=drop&src=172.18.0.10", "FirewallLogs"},
	} {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status code = %d, body = %s", tt.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"kind": "`+tt.want+`"`) {
			t.Fatalf("%s body = %s", tt.path, rec.Body.String())
		}
	}
}

func TestDHCPv6EventHandler(t *testing.T) {
	handler := Handler{
		DHCPv6Event: func(r *http.Request, req DHCPv6EventRequest) (*DHCPv6EventResult, error) {
			if req.Resource != "wan-pd" {
				t.Fatalf("resource = %q, want wan-pd", req.Resource)
			}
			if req.Env["reason"] != "BOUND6" {
				t.Fatalf("env reason = %q, want BOUND6", req.Env["reason"])
			}
			result := NewDHCPv6EventResult(req.Resource)
			return &result, nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, Prefix+"/dhcpv6-event", strings.NewReader(`{"apiVersion":"control.routerd.net/v1alpha1","kind":"DHCPv6Event","resource":"wan-pd","env":{"reason":"BOUND6"}}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"kind": "DHCPv6EventResult"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
