// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	runtime "github.com/imksoo/routerd/pkg/dnsresolver"
	"github.com/imksoo/routerd/pkg/logstore"
)

func TestSidecarRequiresCurrentAssignmentEvidenceAndNamedSource(t *testing.T) {
	controller := Controller{Router: dnsResolverRouterWithZone(api.DNSZoneRecordSpec{Hostname: "router", IPv4: "172.18.0.1"}), StateDir: t.TempDir(), RuntimeDir: t.TempDir()}
	parsed := runtime.ParseDNSMasqHostRecords([]byte("fc:b2:14:8c:49:1a,172.18.1.127,client,12h\n"))
	scopes := []runtime.DHCPAddressScope{{Source: "DHCPv4Server/lan-dhcpv4", Start: "172.18.0.0", End: "172.18.255.255"}}
	records, err := controller.namedDHCPAssignments(t.Context(), parsed, scopes)
	if err != nil || len(records) != 0 {
		t.Fatalf("bare sidecar became a lease: %+v %v", records, err)
	}
	prepareHeldAssignment(t, &controller, "fc:b2:14:8c:49:1a", "172.18.1.127", "client")
	records, err = controller.namedDHCPAssignments(t.Context(), parsed, scopes)
	if err != nil || len(records) != 1 || records[0].Origin != runtime.DHCPAssignmentHeld || records[0].Source != "DHCPv4Server/lan-dhcpv4" || !records[0].ExpiresAt.After(time.Now()) {
		t.Fatalf("actual held deadline/source lost: %+v %v", records, err)
	}
	if selected := assignmentsForSources(records, []string{"DHCPv4Server/foreign"}); len(selected) != 0 {
		t.Fatal("held assignment crossed named source")
	}
	scopes = append(scopes, runtime.DHCPAddressScope{Source: "DHCPv4Server/foreign", Start: "172.18.0.0", End: "172.18.255.255"})
	records, err = controller.namedDHCPAssignments(t.Context(), parsed, scopes)
	if err != nil || len(records) != 0 {
		t.Fatalf("ambiguous intended assignment was attributed: %+v %v", records, err)
	}
}

func TestExpiredStickyAssignmentCannotBeRecoveredAsInfinite(t *testing.T) {
	controller := Controller{Router: &api.Router{}, StateDir: t.TempDir()}
	store, err := logstore.OpenDHCPStickyLog(filepath.Join(controller.StateDir, "dhcp-sticky.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordLeaseEvent(t.Context(), "removed", "client", "192.0.2.55", "expired", 1, time.Now().Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	parsed := []runtime.RuntimeDHCPHostRecord{{MAC: "client", IP: "192.0.2.55", Hostname: "expired"}}
	records, err := controller.namedDHCPAssignments(t.Context(), parsed, []runtime.DHCPAddressScope{{Source: "DHCPv4Server/lan", Start: "192.0.2.0", End: "192.0.2.255"}})
	if err != nil || len(records) != 0 {
		t.Fatalf("expired hold revived: %+v %v", records, err)
	}
	path := filepath.Join(t.TempDir(), "oversize")
	if err := os.WriteFile(path, make([]byte, (8<<20)+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readDHCPHosts(path); err == nil {
		t.Fatal("unbounded sidecar accepted")
	}
}

func TestConfiguredReservationDoesNotRequireDNSMasqSidecar(t *testing.T) {
	router := dnsResolverRouterWithZone(api.DNSZoneRecordSpec{Hostname: "router", IPv4: "192.0.2.1"})
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.DHCPv4ServerSpec{AddressPool: api.DHCPAddressPoolSpec{Start: "192.0.2.100", End: "192.0.2.110"}}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"}, Metadata: api.ObjectMeta{Name: "printer"}, Spec: api.DHCPv4ReservationSpec{Server: "lan", MACAddress: "02:00:00:00:00:01", Hostname: "printer", IPAddress: "192.0.2.150"}},
	)
	controller := Controller{Router: router, StateDir: t.TempDir()}
	records, err := controller.namedDHCPAssignments(t.Context(), nil, runtime.DHCPScopes(router))
	if err != nil || len(records) != 1 {
		t.Fatalf("configured reservation missing without sidecar: %+v %v", records, err)
	}
	record := records[0]
	if record.Origin != runtime.DHCPAssignmentReservation || record.Source != "DHCPv4Server/lan" || record.Hostname != "printer" || record.IP != "192.0.2.150" || record.MAC != "02:00:00:00:00:01" {
		t.Fatalf("configured reservation = %+v", record)
	}
}

func TestRuntimeConfigIncludesConfiguredReservationWithoutDNSMasqSidecar(t *testing.T) {
	router := dnsResolverRouterWithZone(api.DNSZoneRecordSpec{Hostname: "router", IPv4: "192.0.2.1"})
	zone, err := router.Spec.Resources[0].DNSZoneSpec()
	if err != nil {
		t.Fatal(err)
	}
	zone.DHCPDerived.Sources = []string{"DHCPv4Server/lan"}
	router.Spec.Resources[0].Spec = zone
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.DHCPv4ServerSpec{AddressPool: api.DHCPAddressPoolSpec{Start: "192.0.2.100", End: "192.0.2.110"}}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"}, Metadata: api.ObjectMeta{Name: "printer"}, Spec: api.DHCPv4ReservationSpec{Server: "lan", MACAddress: "02:00:00:00:00:01", Hostname: "printer", IPAddress: "192.0.2.150"}},
	)
	resolverSpec, err := router.Spec.Resources[1].DNSResolverSpec()
	if err != nil {
		t.Fatal(err)
	}
	controller := Controller{Router: router, Store: mapStore{}, RuntimeDir: t.TempDir(), StateDir: t.TempDir()}
	config, _, err := controller.runtimeConfig(t.Context(), "lan-resolver", runtime.NormalizeSpec(resolverSpec))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Zones) != 1 || len(config.Zones[0].DHCPHostRecords) != 1 {
		t.Fatalf("runtime DHCP host records = %+v", config.Zones)
	}
	record := config.Zones[0].DHCPHostRecords[0]
	if record.Origin != runtime.DHCPAssignmentReservation || record.Source != "DHCPv4Server/lan" || record.Hostname != "printer" || record.IP != "192.0.2.150" || record.MAC != "02:00:00:00:00:01" {
		t.Fatalf("runtime DHCP host record = %+v", record)
	}
}

func TestRuntimeConfigKeepsUnconditionalDeclaredReservationWhenEffectiveViewOmitsIt(t *testing.T) {
	declared := dnsResolverRouterWithZone(api.DNSZoneRecordSpec{Hostname: "router", IPv4: "192.0.2.1"})
	zone, err := declared.Spec.Resources[0].DNSZoneSpec()
	if err != nil {
		t.Fatal(err)
	}
	zone.DHCPDerived.Sources = []string{"DHCPv4Server/lan"}
	declared.Spec.Resources[0].Spec = zone
	declared.Spec.Resources = append(declared.Spec.Resources,
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.DHCPv4ServerSpec{AddressPool: api.DHCPAddressPoolSpec{Start: "192.0.2.100", End: "192.0.2.110"}}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"}, Metadata: api.ObjectMeta{Name: "printer"}, Spec: api.DHCPv4ReservationSpec{Server: "lan", MACAddress: "02:00:00:00:00:01", Hostname: "printer", IPAddress: "192.0.2.150"}},
	)
	effective := declared.Clone()
	effective.Spec.Resources = effective.Spec.Resources[:len(effective.Spec.Resources)-1]
	resolverSpec, err := effective.Spec.Resources[1].DNSResolverSpec()
	if err != nil {
		t.Fatal(err)
	}
	controller := Controller{Router: &effective, DeclaredRouter: declared, Store: mapStore{}, RuntimeDir: t.TempDir(), StateDir: t.TempDir()}
	config, _, err := controller.runtimeConfig(t.Context(), "lan-resolver", runtime.NormalizeSpec(resolverSpec))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Zones) != 1 || len(config.Zones[0].DHCPHostRecords) != 1 {
		t.Fatalf("runtime DHCP host records = %+v", config.Zones)
	}
	record := config.Zones[0].DHCPHostRecords[0]
	if record.Origin != runtime.DHCPAssignmentReservation || record.Source != "DHCPv4Server/lan" || record.Hostname != "printer" || record.IP != "192.0.2.150" || record.MAC != "02:00:00:00:00:01" {
		t.Fatalf("runtime DHCP host record = %+v", record)
	}
}

func TestRuntimeConfigExportsFixtureReservationAfterConfigDecode(t *testing.T) {
	router, err := config.Load("../../../tests/fixtures/linux-dhcp-firewall-lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}
	var resolverSpec api.DNSResolverSpec
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DNSResolver" || resource.Metadata.Name != "lan" {
			continue
		}
		resolverSpec, err = resource.DNSResolverSpec()
		if err != nil {
			t.Fatal(err)
		}
	}
	controller := Controller{Router: router, DeclaredRouter: router, Store: mapStore{}, RuntimeDir: t.TempDir(), StateDir: t.TempDir()}
	config, _, err := controller.runtimeConfig(t.Context(), "lan", runtime.NormalizeSpec(resolverSpec))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Zones) != 1 || len(config.Zones[0].DHCPHostRecords) != 1 {
		t.Fatalf("fixture runtime DHCP host records = %+v", config.Zones)
	}
	record := config.Zones[0].DHCPHostRecords[0]
	if record.Origin != runtime.DHCPAssignmentReservation || record.Source != "DHCPv4Server/lan" || record.Hostname != "printer" || record.IP != "198.18.15.150" || record.MAC != "02:00:00:15:00:01" {
		t.Fatalf("fixture runtime DHCP host record = %+v", record)
	}
}
