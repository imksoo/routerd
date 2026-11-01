// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
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
