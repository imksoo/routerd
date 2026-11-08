// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type mapStore map[string]map[string]any

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if status := s[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func (s mapStore) Get(name string) routerstate.Value {
	now := s.Now()
	return routerstate.Value{Status: routerstate.StatusUnknown, Since: now, UpdatedAt: now}
}

func (s mapStore) Age(name string) time.Duration {
	return 0
}

func (s mapStore) Now() time.Time {
	return time.Now().UTC()
}

func TestPruneDHCPv4ProfileMismatchedLeases(t *testing.T) {
	dir := t.TempDir()
	leaseFile := filepath.Join(dir, "dnsmasq.leases")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{
			LeaseFile: leaseFile,
			Profiles:  []api.DHCPv4ServerProfileSpec{{Name: "legacy-iot", AddressPool: api.DHCPAddressPoolSpec{Start: "172.17.1.100", End: "172.17.1.199"}}},
			Scopes:    []api.DHCPv4ServerScopeSpec{{Name: "iot", ProfileRef: "legacy-iot", Match: api.DHCPv4ServerScopeMatchSpec{OUIPrefixes: []string{"7c:dd:e9"}}}},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"}, Metadata: api.ObjectMeta{Name: "reserved-iot"}, Spec: api.DHCPv4ReservationSpec{Server: "lan-v4", MACAddress: "7c:dd:e9:00:00:04", IPAddress: "172.18.1.104"}},
	}}}
	leases := "0 7c:dd:e9:00:00:01 172.18.1.101 old-iot *\n" +
		"0 7c:dd:e9:00:00:02 172.17.1.102 profile-iot *\n" +
		"0 02:00:00:00:00:03 172.18.1.103 normal *\n" +
		"0 7c:dd:e9:00:00:04 172.18.1.104 reserved-iot *\n"
	if err := os.WriteFile(leaseFile, []byte(leases), 0o640); err != nil {
		t.Fatal(err)
	}
	changed, err := pruneDHCPv4ProfileMismatchedLeases(router, filepath.Join(dir, "dnsmasq.conf"), filepath.Join(dir, "dnsmasq.pid"))
	if err != nil {
		t.Fatalf("prune leases: %v", err)
	}
	if !changed {
		t.Fatal("expected old profile lease to be removed")
	}
	got, err := os.ReadFile(leaseFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "old-iot") {
		t.Fatalf("old profile lease remains: %s", got)
	}
	for _, hostname := range []string{"profile-iot", "normal", "reserved-iot"} {
		if !strings.Contains(string(got), hostname) {
			t.Fatalf("expected %s to remain: %s", hostname, got)
		}
	}
	if changed, err := pruneDHCPv4ProfileMismatchedLeases(router, filepath.Join(dir, "dnsmasq.conf"), filepath.Join(dir, "dnsmasq.pid")); err != nil || changed {
		t.Fatalf("second prune = changed:%t err:%v, want false nil", changed, err)
	}
}
