// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4client

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

type mapStore map[string]map[string]any

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func TestControllerAppliesLeaseDNS(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "wan.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := daemonapi.NewStatus(daemonapi.DaemonRef{Name: "routerd-dhcpv4-client-wan", Kind: "routerd-dhcpv4-client", Instance: "wan"})
		status.Resources = []daemonapi.ResourceStatus{{
			Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client", Name: "wan"},
			Phase:    daemonapi.ResourcePhaseBound,
			Observed: map[string]string{
				"interface":      "ens18",
				"currentAddress": "192.0.2.10",
				"prefixLength":   "24",
				"dnsServers":     `["192.0.2.53","192.0.2.54"]`,
				"ntpServers":     `["192.0.2.123"]`,
			},
		}}
		_ = json.NewEncoder(w).Encode(status)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	resolvPath := filepath.Join(t.TempDir(), "resolv.conf")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.DHCPv4ClientSpec{Interface: "wan", UseRoutes: boolPtr(false)}},
	}}}
	store := mapStore{}
	controller := Controller{
		Router:         router,
		Bus:            bus.New(),
		Store:          store,
		DaemonSockets:  map[string]string{"wan": socket},
		ResolvConfPath: resolvPath,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background(), "wan"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(resolvPath)
	if err != nil {
		t.Fatalf("read resolv.conf: %v", err)
	}
	for _, want := range []string{"# Source: DHCPv4Client/wan", "nameserver 192.0.2.53", "nameserver 192.0.2.54"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("resolv.conf missing %q:\n%s", want, data)
		}
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
	if status["appliedDNSServers"] != "192.0.2.53,192.0.2.54" {
		t.Fatalf("status = %#v", status)
	}
	ntpServers, _ := status["ntpServers"].([]string)
	if strings.Join(ntpServers, ",") != "192.0.2.123" {
		t.Fatalf("ntpServers status = %#v", status["ntpServers"])
	}
}

func TestControllerAppliesLeaseDNSWithSystemdResolved(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "wan.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := daemonapi.NewStatus(daemonapi.DaemonRef{Name: "routerd-dhcpv4-client-wan", Kind: "routerd-dhcpv4-client", Instance: "wan"})
		status.Resources = []daemonapi.ResourceStatus{{
			Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client", Name: "wan"},
			Phase:    daemonapi.ResourcePhaseBound,
			Observed: map[string]string{
				"interface":      "ens18",
				"currentAddress": "192.0.2.10",
				"prefixLength":   "24",
				"dnsServers":     `["192.0.2.53","192.0.2.54"]`,
			},
		}}
		_ = json.NewEncoder(w).Encode(status)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	resolvPath := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.Symlink("/run/systemd/resolve/stub-resolv.conf", resolvPath); err != nil {
		t.Fatal(err)
	}
	var commands []string
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.DHCPv4ClientSpec{Interface: "wan", UseRoutes: boolPtr(false)}},
	}}}
	store := mapStore{}
	controller := Controller{
		Router:         router,
		Bus:            bus.New(),
		Store:          store,
		DaemonSockets:  map[string]string{"wan": socket},
		ResolvConfPath: resolvPath,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background(), "wan"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ip -4 addr show dev ens18",
		"ip -4 addr replace 192.0.2.10/24 dev ens18",
		"resolvectl dns ens18 192.0.2.53 192.0.2.54",
		"resolvectl domain ens18 ~.",
	}
	if strings.Join(commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n%s", strings.Join(commands, "\n"))
	}
}

func boolPtr(value bool) *bool {
	return &value
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
			Resource:   daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client", Name: "wan"},
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
	status := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
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
			Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client", Name: "wan"},
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
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.DHCPv4ClientSpec{Interface: "wan", RouteMetric: 100}},
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
		"ip -4 addr show dev ens18",
		"ip -4 addr replace 192.0.2.10/24 dev ens18",
		"ip -4 route show default",
		"ip -4 route replace default via 192.0.2.1 dev ens18 metric 100",
	}
	if strings.Join(commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n%s", strings.Join(commands, "\n"))
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
	if status["appliedAddress"] != "192.0.2.10/24" {
		t.Fatalf("status = %#v", status)
	}
}

func TestControllerSkipsUnchangedDefaultRoute(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "wan.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := daemonapi.NewStatus(daemonapi.DaemonRef{Name: "routerd-dhcpv4-client-wan", Kind: "routerd-dhcpv4-client", Instance: "wan"})
		status.Resources = []daemonapi.ResourceStatus{{
			Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client", Name: "wan"},
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
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.DHCPv4ClientSpec{Interface: "wan", RouteMetric: 100}},
	}}}
	store := mapStore{
		api.NetAPIVersion + "/DHCPv4Client/wan": {
			"phase":          daemonapi.ResourcePhaseBound,
			"currentAddress": "192.0.2.10",
			"prefixLength":   "24",
			"appliedAddress": "192.0.2.10/24",
		},
	}
	controller := Controller{
		Router:        router,
		Bus:           bus.New(),
		Store:         store,
		DaemonSockets: map[string]string{"wan": socket},
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			commands = append(commands, line)
			switch line {
			case "ip -4 addr show dev ens18":
				return []byte("2: ens18 inet 192.0.2.10/24 brd 192.0.2.255 scope global ens18\n"), nil
			case "ip -4 route show default":
				return []byte("default via 192.0.2.1 dev ens18 metric 100\n"), nil
			default:
				return nil, nil
			}
		},
	}
	if err := controller.Reconcile(context.Background(), "wan"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ip -4 addr show dev ens18",
		"ip -4 route show default",
	}
	if strings.Join(commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n%s", strings.Join(commands, "\n"))
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
	if status["defaultRoutePresent"] != true {
		t.Fatalf("status = %#v", status)
	}
}

func TestLeaseEventChangedIgnoresLeaseTimestamps(t *testing.T) {
	current := map[string]any{
		"phase":          daemonapi.ResourcePhaseBound,
		"currentAddress": "192.0.2.10",
		"prefixLength":   "24",
		"defaultGateway": "192.0.2.1",
		"domain":         "example.test",
		"leaseTime":      "3600",
		"appliedAddress": "192.0.2.10/24",
		"dnsServers":     []string{"192.0.2.53"},
		"ntpServers":     []string{"192.0.2.123"},
		"lastLeaseAt":    "2026-06-19T10:00:00Z",
		"lastRenewAt":    "2026-06-19T10:00:00Z",
		"lastAppliedAt":  "2026-06-19T10:00:00Z",
	}
	next := map[string]any{}
	for key, value := range current {
		next[key] = value
	}
	next["lastLeaseAt"] = "2026-06-19T10:00:30Z"
	next["lastRenewAt"] = "2026-06-19T10:00:30Z"
	next["lastAppliedAt"] = "2026-06-19T10:00:30Z"
	if leaseEventChanged(current, next) {
		t.Fatal("timestamp-only lease refresh must not emit DHCPv4 client applied event")
	}

	next["defaultGateway"] = "192.0.2.254"
	if !leaseEventChanged(current, next) {
		t.Fatal("default gateway change must emit DHCPv4 client applied event")
	}
}

func TestControllerRepairsMissingLeaseAddressWithStaleStatus(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "wan.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := daemonapi.NewStatus(daemonapi.DaemonRef{Name: "routerd-dhcpv4-client-wan", Kind: "routerd-dhcpv4-client", Instance: "wan"})
		status.Resources = []daemonapi.ResourceStatus{{
			Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client", Name: "wan"},
			Phase:    daemonapi.ResourcePhaseBound,
			Observed: map[string]string{
				"interface":      "ens18",
				"currentAddress": "192.0.2.10",
				"prefixLength":   "24",
			},
		}}
		_ = json.NewEncoder(w).Encode(status)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	var commands []string
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.DHCPv4ClientSpec{Interface: "wan", UseRoutes: boolPtr(false)}},
	}}}
	store := mapStore{
		api.NetAPIVersion + "/DHCPv4Client/wan": {
			"phase":          daemonapi.ResourcePhaseBound,
			"currentAddress": "192.0.2.10",
			"prefixLength":   "24",
			"appliedAddress": "192.0.2.10/24",
		},
	}
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
		"ip -4 addr show dev ens18",
		"ip -4 addr replace 192.0.2.10/24 dev ens18",
	}
	if strings.Join(commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n%s", strings.Join(commands, "\n"))
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
	if status["addressPresent"] != true {
		t.Fatalf("status = %#v", status)
	}
}
