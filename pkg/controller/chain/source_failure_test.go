// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

type unavailableSourceStore struct {
	mapStore
	failure       error
	healthFailure bool
}

func (s unavailableSourceStore) ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error) {
	if name == "unavailable" {
		return nil, s.failure
	}
	return s.mapStore.ReadObjectStatus(apiVersion, kind, name)
}

func (s unavailableSourceStore) HealthObservation(name string) (api.HealthObservation, error) {
	if s.healthFailure {
		return api.HealthObservation{}, s.failure
	}
	return s.mapStore.HealthObservation(name)
}

func TestNTPReadFailurePreservesAppliedConfigAndDoesNotChooseFallback(t *testing.T) {
	failure := errors.New("NTP source read failed")
	source := api.StatusValueSourceSpec{Resource: "DHCPv6Information/unavailable", Field: "sntpServers", Optional: true}
	for _, field := range []string{"client", "server", "listen", "allow"} {
		t.Run(field, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "chrony.conf")
			if err := os.WriteFile(path, []byte("previous NTP config"), 0o600); err != nil {
				t.Fatal(err)
			}
			previous := map[string]any{"phase": "Applied", "servers": []string{"192.0.2.123"}}
			store := unavailableSourceStore{mapStore: mapStore{}, failure: failure}
			calls := 0
			command := func(context.Context, string, ...string) ([]byte, error) { calls++; return nil, nil }
			var err error
			kind := "NTPServer"
			if field == "client" {
				kind = "NTPClient"
				store.mapStore[api.SystemAPIVersion+"/"+kind+"/time"] = previous
				router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: kind}, Metadata: api.ObjectMeta{Name: "time"}, Spec: api.NTPClientSpec{Managed: true, Provider: "chrony", Source: "auto", ServerFrom: []api.StatusValueSourceSpec{source}, FallbackServers: []string{"198.51.100.123"}}}}}}
				err = (NTPClientController{Router: router, Store: store, ConfigPath: path, Command: command}).Reconcile(context.Background())
			} else {
				store.mapStore[api.SystemAPIVersion+"/"+kind+"/time"] = previous
				spec := api.NTPServerSpec{Managed: true, Provider: "chrony", Servers: []string{"192.0.2.123"}}
				switch field {
				case "server":
					spec.Source, spec.ServerFrom, spec.FallbackServers = "auto", []api.StatusValueSourceSpec{source}, []string{"198.51.100.123"}
				case "listen":
					spec.ListenAddressFrom = []api.StatusValueSourceSpec{source}
				case "allow":
					spec.AllowCIDRFrom = []api.StatusValueSourceSpec{source}
				}
				router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: kind}, Metadata: api.ObjectMeta{Name: "time"}, Spec: spec}}}}
				err = (NTPServerController{Router: router, Store: store, ConfigPath: path, Command: command}).Reconcile(context.Background())
			}
			if !errors.Is(err, failure) || calls != 0 {
				t.Fatalf("failed source selected new NTP effects: calls=%d, %v", calls, err)
			}
			data, _ := os.ReadFile(path)
			if string(data) != "previous NTP config" || !reflect.DeepEqual(store.ObjectStatus(api.SystemAPIVersion, kind, "time"), previous) {
				t.Fatal("failed source changed applied NTP state")
			}
		})
	}
}

func TestPolicySourceReadFailureCannotChooseFallbackOrCleanUp(t *testing.T) {
	failure := errors.New("policy observation unavailable")
	source := api.StatusValueSourceSpec{Resource: "Interface/unavailable", Field: "ifname", Optional: true}
	for _, field := range []string{"device", "gateway", "dependency", "health"} {
		t.Run(field, func(t *testing.T) {
			candidate := api.EgressRoutePolicyCandidate{Interface: "wan", Priority: 100, Table: 100, Mark: 100, Gateway: "192.0.2.1", GatewaySource: "static"}
			switch field {
			case "device":
				candidate.DeviceFrom = source
			case "gateway":
				candidate.GatewayFrom = source
			case "dependency":
				candidate.DependsOn = []api.ResourceDependencySpec{{Resource: source.Resource, Optional: true}}
			case "health":
				candidate.HealthCheck = "health"
			}
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "policy"}, Spec: api.EgressRoutePolicySpec{Mode: "mark", Candidates: []api.EgressRoutePolicyCandidate{candidate}}},
			}}}
			store := unavailableSourceStore{mapStore: mapStore{}, failure: failure, healthFailure: field == "health"}
			var commands []string
			controller := IPv4PolicyRouteController{Router: router, Store: store, OperatingSystem: platform.OSLinux, PolicyPath: filepath.Join(t.TempDir(), "policy.nft"), DefaultRoutePath: filepath.Join(t.TempDir(), "default.nft"), CommandOutput: func(_ context.Context, command string, args ...string) ([]byte, error) {
				commands = append(commands, command+" "+strings.Join(args, " "))
				return nil, nil
			}}
			if err := controller.Reconcile(context.Background()); !errors.Is(err, failure) || len(commands) != 0 {
				t.Fatalf("observation failure mutated/fell back: commands=%v, %v", commands, err)
			}
		})
	}
}

func TestHealthCheckFailedSourcePreservesUnit(t *testing.T) {
	failure := errors.New("health source unavailable")
	path := filepath.Join(t.TempDir(), "health.service")
	if err := os.WriteFile(path, []byte("previous health unit"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	controller := SystemdUnitController{Router: &api.Router{}, Store: unavailableSourceStore{mapStore: mapStore{}, failure: failure}}
	spec := api.HealthCheckSpec{Target: "192.0.2.1", Protocol: "icmp", SourceAddressFrom: api.StatusValueSourceSpec{Resource: "Interface/unavailable", Field: "primaryIPv4", Optional: true}}
	_, _, err := controller.applyHealthCheckSystemdUnit(context.Background(), path, "health.service", "health", spec, nil, func(context.Context, string, ...string) ([]byte, error) {
		calls++
		return nil, nil
	})
	if !errors.Is(err, failure) || calls != 0 {
		t.Fatalf("failed health source applied an unbound probe: calls=%d, %v", calls, err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "previous health unit" {
		t.Fatal("failed health source replaced the health unit")
	}
}

func TestDnsmasqFailedSourcePreservesConfigAndHostSidecar(t *testing.T) {
	failure := errors.New("DNS server source unavailable")
	dir := t.TempDir()
	path := filepath.Join(dir, "dnsmasq.conf")
	hosts := dnsmasqHostsFile(path)
	for name, content := range map[string]string{path: "previous config", hosts: "previous hosts"} {
		if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}}, {
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan"},
			Spec: api.DHCPv4ServerSpec{Interface: "lan", AddressPool: api.DHCPAddressPoolSpec{Start: "192.0.2.10", End: "192.0.2.20"}, DNSServerFrom: []api.StatusValueSourceSpec{{Resource: "Interface/unavailable", Field: "primaryIPv4", Optional: true}}},
		}}}}
	_, _, err := writeDnsmasqConfig(router, unavailableSourceStore{mapStore: mapStore{}, failure: failure}, path, filepath.Join(dir, "dnsmasq.pid"), 0, nil)
	if !errors.Is(err, failure) {
		t.Fatalf("failed source became an empty DNS option: %v", err)
	}
	for name, content := range map[string]string{path: "previous config", hosts: "previous hosts"} {
		data, _ := os.ReadFile(name)
		if string(data) != content {
			t.Fatalf("failed source changed %s", name)
		}
	}
}
