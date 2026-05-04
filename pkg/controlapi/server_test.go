package controlapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"routerd/pkg/apply"
	"routerd/pkg/logstore"
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

func TestApplyHandler(t *testing.T) {
	handler := Handler{
		Apply: func(r *http.Request, req ApplyRequest) (*ApplyResult, error) {
			if !req.DryRun {
				t.Fatal("DryRun = false, want true")
			}
			result := NewApplyResult(&apply.Result{Phase: "Healthy", Generation: 7})
			return &result, nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, Prefix+"/apply", strings.NewReader(`{"apiVersion":"control.routerd.net/v1alpha1","kind":"ApplyRequest","dryRun":true}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"kind": "ApplyResult"`) {
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

func TestNAPTHandler(t *testing.T) {
	handler := Handler{
		NAPT: func(r *http.Request, req NAPTRequest) (*NAPTTable, error) {
			if req.Limit != 10 {
				t.Fatalf("limit = %d, want 10", req.Limit)
			}
			table := NewNAPTTable(nil)
			table.Status.Count = 3
			return &table, nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, Prefix+"/napt?limit=10", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"kind": "NAPTTable"`) {
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
