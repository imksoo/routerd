// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApplyRejectsUnknownIntentAndTrailingRequestBeforeMutation(t *testing.T) {
	for _, body := range []string{`{"dry-run":true}`, `{} {"dryRun":true}`} {
		t.Run(body, func(t *testing.T) {
			called := false
			handler := Handler{Apply: func(*http.Request, ApplyRequest) (*ApplyResult, error) { called = true; return &ApplyResult{}, nil }}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, Prefix+"/apply", strings.NewReader(body)))
			if called || response.Code != http.StatusBadRequest {
				t.Fatalf("ambiguous intent reached mutation=%v code=%d", called, response.Code)
			}
		})
	}
}

func TestClientRejectsTrailingSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{} {"error":"effect outcome unconfirmed"}`))
	}))
	defer server.Close()
	if _, err := NewHTTPClient(server.URL).Apply(context.Background(), ApplyRequest{}); err == nil {
		t.Fatal("trailing response accepted as confirmed success")
	}
}

func TestClientChecksEncodedRequestBudgetBeforeMutation(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; _, _ = w.Write([]byte(`{}`)) }))
	defer server.Close()
	// The YAML string fits the raw ceiling, but JSON escaping exceeds the
	// actual server limit. The client must enforce the encoded budget.
	candidate := strings.Repeat("\n", MaxRequestBodyBytes/2)
	if _, err := NewHTTPClient(server.URL).Apply(context.Background(), ApplyRequest{CandidateYAML: candidate}); err == nil {
		t.Fatal("oversized encoded request accepted")
	}
	if called {
		t.Fatal("oversized request reached mutation endpoint")
	}
}
