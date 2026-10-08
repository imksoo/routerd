// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Keep the HTTP surface stable as implementations move between responsibility files.
func TestHandlerRoutingContract(t *testing.T) {
	h := New(Options{BasePath: "/console", Title: "Router <lab>", Store: fakeStore{}})
	for _, path := range []string{
		"api/v1/resources", "api/v1/controllers", "api/v1/events",
		"api/v1/dns-queries", "api/v1/dns-queries/aggregate",
		"api/v1/traffic-flows", "api/v1/traffic-flows/aggregate",
		"api/v1/firewall-logs", "api/v1/firewall/deny-timeline",
		"api/v1/sam", "api/v1/bgp", "api/v1/vrrp", "api/v1/ingress",
	} {
		t.Run(path, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest(method, "/console/"+path, nil))
				if w.Code != http.StatusOK || !json.Valid(w.Body.Bytes()) {
					t.Fatalf("%s %s: status=%d body=%s", method, path, w.Code, w.Body.String())
				}
				if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
					t.Fatalf("content type = %q", got)
				}
			}
		})
	}
	for _, path := range []string{"", "index.html", "bgp", "vrrp", "ingress"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/console/"+path, nil))
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s: status=%d", path, w.Code)
		}
		if strings.Contains(w.Body.String(), "__ROUTERD_TITLE_TEXT__") || strings.Contains(w.Body.String(), "__ROUTERD_BASE_PATH__") {
			t.Fatalf("%s: unexpanded page placeholder", path)
		}
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodPost, "api/v1/resources", http.StatusMethodNotAllowed},
		{http.MethodDelete, "", http.StatusMethodNotAllowed},
		{http.MethodGet, "api/v1/not-found", http.StatusNotFound},
		{http.MethodGet, "missing.js", http.StatusNotFound},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, "/console/"+tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s %s: status=%d, want %d", tc.method, tc.path, w.Code, tc.status)
		}
	}
}
