package dhcpv4lease

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
	socket := filepath.Join(t.TempDir(), "wan.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := daemonapi.NewStatus(daemonapi.DaemonRef{Name: "routerd-dhcpv4-client-wan", Kind: "routerd-dhcpv4-client", Instance: "wan"})
		status.Phase = daemonapi.PhaseRunning
		status.Health = daemonapi.HealthOK
		status.Resources = []daemonapi.ResourceStatus{{
			Resource:   daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Lease", Name: "wan"},
			Phase:      daemonapi.ResourcePhaseBound,
			Health:     daemonapi.HealthOK,
			Conditions: []daemonapi.Condition{},
			Observed: map[string]string{
				"interface":      "ens18",
				"currentAddress": "192.0.2.10",
				"defaultGateway": "192.0.2.1",
				"dnsServers":     `["192.0.2.53"]`,
				"leaseTime":      "7200",
				"renewAt":        time.Unix(100, 0).UTC().Format(time.RFC3339),
			},
		}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	store := mapStore{}
	controller := Controller{
		Router:        &api.Router{},
		Bus:           bus.New(),
		Store:         store,
		DaemonSockets: map[string]string{"wan": socket},
		DryRun:        true,
	}
	if err := controller.Reconcile(context.Background(), "wan"); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Lease", "wan")
	if status["phase"] != daemonapi.ResourcePhaseBound {
		t.Fatalf("phase = %v", status["phase"])
	}
	if status["currentAddress"] != "192.0.2.10" || status["defaultGateway"] != "192.0.2.1" {
		t.Fatalf("unexpected lease status: %#v", status)
	}
	servers, ok := status["dnsServers"].([]string)
	if !ok || len(servers) != 1 || servers[0] != "192.0.2.53" {
		t.Fatalf("dnsServers = %#v", status["dnsServers"])
	}
}
