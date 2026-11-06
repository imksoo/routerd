// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

// Exercise upstream CLI argument validation and its actual preference patch,
// without connecting a host daemon or a tailnet. The LocalAPI is a fixture.
func TestTailscaleCLIWithdrawsAdvertisedRoutes(t *testing.T) {
	binary := os.Getenv("ROUTERD_TEST_TAILSCALE")
	if binary == "" {
		t.Skip("set ROUTERD_TEST_TAILSCALE to an upstream tailscale executable")
	}
	socket := filepath.Join(t.TempDir(), "tailscaled.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	patches := make(chan struct {
		AdvertiseRoutes    []string
		AdvertiseRoutesSet bool
	}, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/localapi/v0/status":
			_, _ = w.Write([]byte(`{"BackendState":"Running","Self":{},"Health":[]}`))
		case r.URL.Path == "/localapi/v0/prefs" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"ControlURL":"https://controlplane.tailscale.com","WantRunning":true,"CorpDNS":true,"NetfilterMode":2,"Hostname":"test-router","AdvertiseRoutes":["0.0.0.0/0","::/0","192.0.2.0/24"]}`))
		case r.URL.Path == "/localapi/v0/prefs" && r.Method == http.MethodPatch:
			var patch struct {
				AdvertiseRoutes    []string
				AdvertiseRoutesSet bool
			}
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Errorf("invalid upstream patch: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			patches <- patch
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected LocalAPI request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	// The declaration no longer advertises either exit-node or subnet routes.
	args := TailscaleUpArgs(api.TailscaleNodeSpec{BinaryPath: binary, Hostname: "test-router"})
	command := exec.CommandContext(ctx, binary, append([]string{"--socket=" + socket}, args[1:]...)...)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("upstream CLI rejected declared withdrawal: %v\n%s", err, out)
	}
	select {
	case patch := <-patches:
		if !patch.AdvertiseRoutesSet || len(patch.AdvertiseRoutes) != 0 {
			t.Fatalf("withdrawal did not explicitly clear advertised routes: %+v", patch)
		}
	default:
		t.Fatal("upstream CLI did not send a preference change")
	}
}
