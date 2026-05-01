package controlapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"routerd/pkg/apply"
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
			if !req.Prune {
				t.Fatal("Prune = false, want true")
			}
			result := NewApplyResult(&apply.Result{Phase: "Healthy", Generation: 7})
			return &result, nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, Prefix+"/apply", strings.NewReader(`{"apiVersion":"control.routerd.net/v1alpha1","kind":"ApplyRequest","dryRun":true,"prune":true}`))
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
				Deleted:  []string{"net.routerd.net/v1alpha1/IPv6PrefixDelegation/wan-pd"},
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

func TestDHCP6EventHandler(t *testing.T) {
	handler := Handler{
		DHCP6Event: func(r *http.Request, req DHCP6EventRequest) (*DHCP6EventResult, error) {
			if req.Resource != "wan-pd" {
				t.Fatalf("resource = %q, want wan-pd", req.Resource)
			}
			if req.Env["reason"] != "BOUND6" {
				t.Fatalf("env reason = %q, want BOUND6", req.Env["reason"])
			}
			result := NewDHCP6EventResult(req.Resource)
			return &result, nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, Prefix+"/dhcp6-event", strings.NewReader(`{"apiVersion":"control.routerd.net/v1alpha1","kind":"DHCP6Event","resource":"wan-pd","env":{"reason":"BOUND6"}}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"kind": "DHCP6EventResult"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
