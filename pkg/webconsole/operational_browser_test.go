// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestOperationalBrowserKeepsNewerStatusAndUnknownObservations(t *testing.T) {
	if os.Getenv("ROUTERD_WEB_CONSOLE_CHROMIUM_PATH") == "" {
		t.Skip("set ROUTERD_WEB_CONSOLE_CHROMIUM_PATH to run the actual operational-page browser check")
	}
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	status := map[string]any{
		"phase":    "OlderObservation",
		"backends": []map[string]any{{"name": "backend-not-probed", "address": "192.0.2.2", "port": 443, "healthyCount": 0, "unhealthyCount": 0}},
	}
	if err := store.SaveObjectStatus(api.FirewallAPIVersion, "IngressService", "web", status); err != nil {
		t.Fatal(err)
	}
	handler := New(Options{Store: store})
	releaseOld := make(chan struct{})
	var releaseOnce sync.Once
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/release-old" {
			releaseOnce.Do(func() { close(releaseOld) })
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path == "/api/v1/ingress" {
			switch requests.Add(1) {
			case 1:
				old := httptest.NewRecorder()
				handler.ServeHTTP(old, r)
				<-releaseOld
				for name, values := range old.Header() {
					w.Header()[name] = values
				}
				w.WriteHeader(old.Code)
				_, _ = w.Write(old.Body.Bytes())
				return
			case 2:
				status["phase"] = "NewerObservation"
				if err := store.SaveObjectStatus(api.FirewallAPIVersion, "IngressService", "web", status); err != nil {
					t.Error(err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	defer releaseOnce.Do(func() { close(releaseOld) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "webconsole", "scripts", "check-operational.mjs"), server.URL)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("operational browser: %v\n%s", err, output)
	}
}
