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
