// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4lease

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
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
				"prefixLength":   "24",
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

func TestControllerAppliesLeaseAddressAndRoute(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "wan.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := daemonapi.NewStatus(daemonapi.DaemonRef{Name: "routerd-dhcpv4-client-wan", Kind: "routerd-dhcpv4-client", Instance: "wan"})
		status.Resources = []daemonapi.ResourceStatus{{
			Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Lease", Name: "wan"},
			Phase:    daemonapi.ResourcePhaseBound,
			Observed: map[string]string{
				"interface":      "ens18",
				"currentAddress": "192.0.2.10",
				"prefixLength":   "24",
				"defaultGateway": "192.0.2.1",
			},
		}}
		_ = json.NewEncoder(w).Encode(status)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	var commands []string
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Lease"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.DHCPv4LeaseSpec{Interface: "wan", RouteMetric: 100}},
	}}}
	store := mapStore{}
	controller := Controller{
		Router:        router,
		Bus:           bus.New(),
		Store:         store,
		DaemonSockets: map[string]string{"wan": socket},
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background(), "wan"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ip -4 addr replace 192.0.2.10/24 dev ens18",
		"ip -4 route replace default via 192.0.2.1 dev ens18 metric 100",
	}
	if strings.Join(commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n%s", strings.Join(commands, "\n"))
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Lease", "wan")
	if status["appliedAddress"] != "192.0.2.10/24" {
		t.Fatalf("status = %#v", status)
	}
}
