// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestLegacyV1NativeHandoffAllowsOnlyDigestBoundConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	config := []byte("apiVersion: routerd.net/v1alpha1\nkind: Router\n")
	if err := os.WriteFile(configPath, config, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(config)
	handoffPath := filepath.Join(dir, "handoff.json")
	record := fmt.Sprintf("{\"version\":\"%s\",\"sourceCommit\":\"%s\",\"configSHA256\":\"%x\"}\n", legacyV1NativeHandoffVersion, legacyV1NativeHandoffCommit, digest)
	if err := os.WriteFile(handoffPath, []byte(record), 0600); err != nil {
		t.Fatal(err)
	}
	allowed, err := legacyV1NativeHandoffAllows(handoffPath, configPath)
	if err != nil || !allowed {
		t.Fatalf("allowed=%v err=%v, want true nil", allowed, err)
	}
	if err := os.WriteFile(configPath, append(config, []byte("metadata: {}\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	allowed, err = legacyV1NativeHandoffAllows(handoffPath, configPath)
	if err != nil || allowed {
		t.Fatalf("changed configuration allowed=%v err=%v, want false nil", allowed, err)
	}
	if err := os.WriteFile(handoffPath, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := legacyV1NativeHandoffAllows(handoffPath, configPath); err == nil {
		t.Fatal("invalid handoff record was accepted")
	}
}

func TestLegacyV1NativeHandoffPersistsStaticAddressReceipt(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	config := []byte("apiVersion: routerd.net/v1alpha1\nkind: Router\n")
	if err := os.WriteFile(configPath, config, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(config)
	handoffPath := filepath.Join(dir, "handoff.json")
	record := fmt.Sprintf("{\"version\":\"%s\",\"sourceCommit\":\"%s\",\"configSHA256\":\"%x\"}\n", legacyV1NativeHandoffVersion, legacyV1NativeHandoffCommit, digest)
	if err := os.WriteFile(handoffPath, []byte(record), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	controller := IPv4StaticAddressController{
		Router:                    staticOwnerRouter(),
		Store:                     db,
		ConfigPath:                configPath,
		LegacyV1NativeHandoffPath: handoffPath,
		ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
			return staticAddressSnapshot{Index: 10, Scope: "fixture-kernel", Addresses: []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}}, nil
		},
		Command: func(context.Context, string, ...string) error {
			t.Fatal("digest-bound existing address was modified instead of adopted")
			return nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err := db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := staticReceipt(status)
	if err != nil || receipt == nil || receipt.Pending || !status["legacyV1NativeAdopted"].(bool) {
		t.Fatalf("handoff receipt was not durably adopted: status=%#v receipt=%#v err=%v", status, receipt, err)
	}
}
