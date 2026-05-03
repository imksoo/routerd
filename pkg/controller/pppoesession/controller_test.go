package pppoesession

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/pppoeclient"
)

type mapStore map[string]map[string]any

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	return s[apiVersion+"/"+kind+"/"+name]
}

func TestControllerReconcilesDaemonStatus(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "pppoe.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := daemonapi.NewStatus(daemonapi.DaemonRef{Name: "routerd-pppoe-client-wan", Kind: pppoeclient.DaemonKind, Instance: "wan"})
		status.Phase = daemonapi.PhaseRunning
		status.Health = daemonapi.HealthOK
		status.Resources = []daemonapi.ResourceStatus{{
			Resource:   daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "PPPoESession", Name: "wan"},
			Phase:      pppoeclient.PhaseConnected,
			Health:     daemonapi.HealthOK,
			Conditions: []daemonapi.Condition{},
			Observed: map[string]string{
				"interface":      "ens18",
				"ifname":         "ppp-wan",
				"currentAddress": "198.51.100.10",
				"peerAddress":    "198.51.100.1",
				"dnsServers":     `["203.0.113.53"]`,
				"connectedAt":    time.Unix(100, 0).UTC().Format(time.RFC3339),
			},
		}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	store := mapStore{}
	controller := Controller{Router: &api.Router{}, Bus: bus.New(), Store: store, DaemonSockets: map[string]string{"wan": socket}, DryRun: true}
	if err := controller.Reconcile(context.Background(), "wan"); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "PPPoESession", "wan")
	if status["phase"] != pppoeclient.PhaseConnected || status["gateway"] != "198.51.100.1" {
		t.Fatalf("unexpected status: %#v", status)
	}
}
