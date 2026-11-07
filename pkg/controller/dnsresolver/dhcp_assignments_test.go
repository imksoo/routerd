// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
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
