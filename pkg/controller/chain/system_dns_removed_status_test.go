// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

type dnsRemovalStore struct {
	mapStore
	failDelete bool
}

func (s *dnsRemovalStore) DeleteObject(version, kind, name string) error {
	if s.failDelete && kind == "DNSResolver" {
		return errors.New("status deletion unavailable")
	}
	return s.mapStore.DeleteObject(version, kind, name)
}

func TestSystemdDNSRemovalRetiresStatusOnlyAfterStopAndRetriesDeletion(t *testing.T) {
	dir := t.TempDir()
	unit := dnsResolverUnitName("old")
	path := filepath.Join(dir, unit)
	if err := os.WriteFile(path, []byte("# Managed by routerd. Do not edit by hand.\n[Service]\nExecStart=/usr/local/sbin/routerd-dns-resolver\n"), 0644); err != nil {
		t.Fatal(err)
	}
	store := &dnsRemovalStore{mapStore: mapStore{api.NetAPIVersion + "/DNSResolver/old": {"phase": "Pending"}}}
	c := SystemdUnitController{Router: &api.Router{}, Store: store, SystemdSystemDir: dir}
	failStop := true
	command := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "disable" && failStop {
			return nil, errors.New("unit stop failed")
		}
		return nil, nil
	}
	if err := c.cleanupStaleDNSResolverUnits(t.Context(), command); err == nil {
		t.Fatal("unit stop failure hidden")
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "old"); got["phase"] != "Pending" {
		t.Fatalf("unconfirmed stop deleted resolver status: %v", got)
	}
	failStop = false
	store.failDelete = true
	if err := c.cleanupStaleDNSResolverUnits(t.Context(), command); err == nil {
		t.Fatal("status deletion failure hidden")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unit removal not complete before status deletion: %v", err)
	}
	store.failDelete = false
	if err := c.cleanupStaleDNSResolverUnits(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "old"); got != nil {
		t.Fatalf("removed resolver retains status: %v", got)
	}
}

func TestSystemdDNSRemovalKeepsDisabledDeclarationStatus(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/DNSResolver/lan":                              {"phase": "Pending", "reason": "WhenFalse"},
		api.SystemAPIVersion + "/ServiceUnit/" + dnsResolverUnitName("lan"): {"phase": "Removed"},
	}
	declared := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "lan"}},
	}}}
	c := SystemdUnitController{Router: &api.Router{}, DeclaredRouter: declared, Store: store, SystemdSystemDir: t.TempDir()}
	if err := c.cleanupStaleDNSResolverUnits(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "lan"); got["reason"] != "WhenFalse" {
		t.Fatalf("disabled resolver status lost: %v", got)
	}
}
