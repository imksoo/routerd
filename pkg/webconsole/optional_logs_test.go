package webconsole

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestOptionalLogDatabases(t *testing.T) {
	endpoints := []string{"dns-queries", "dns-queries/aggregate", "traffic-flows", "traffic-flows/aggregate", "firewall-logs"}
	for _, endpoint := range endpoints {
		t.Run(endpoint, func(t *testing.T) {
			request := func(opts Options) *httptest.ResponseRecorder {
				t.Helper()
				rec := httptest.NewRecorder()
				New(opts).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/"+endpoint+"?from=2026-01-01T00:00:00Z&until=2026-01-02T00:00:00Z", nil))
				return rec
			}
			empty := request(Options{})
			if empty.Code != http.StatusOK {
				t.Fatalf("unset path: %d %s", empty.Code, empty.Body.String())
			}
			for _, nested := range []bool{false, true} {
				dir := t.TempDir()
				path := filepath.Join(dir, "log.db")
				if nested {
					path = filepath.Join(dir, "missing", "log.db")
				}
				rec := request(Options{DNSQueryLogPath: path, TrafficFlowLogPath: path, FirewallLogPath: path})
				if rec.Code != http.StatusOK || rec.Body.String() != empty.Body.String() {
					t.Errorf("missing database (nested=%v): %d %s; want %s", nested, rec.Code, rec.Body.String(), empty.Body.String())
				}
				entries, err := os.ReadDir(dir)
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) != 0 {
					t.Errorf("read created files: %v", entries)
				}
			}
			path := filepath.Join(t.TempDir(), "corrupt.db")
			if err := os.WriteFile(path, []byte("not a SQLite database"), 0600); err != nil {
				t.Fatal(err)
			}
			rec := request(Options{DNSQueryLogPath: path, TrafficFlowLogPath: path, FirewallLogPath: path})
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("corrupt database: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}
