// SPDX-License-Identifier: BSD-3-Clause

package nftstate

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerificationRequiresAcknowledgedContentAndFreshness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.nft")
	now := time.Now()
	if err := os.WriteFile(path, []byte("applied"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := MarkVerified(path, now); err != nil {
		t.Fatal(err)
	}
	if !RecentlyVerified(path, now) || RecentlyVerified(path, now.Add(DefaultVerifyInterval)) {
		t.Fatal("verification freshness is incorrect")
	}
	if err := os.WriteFile(path, []byte("desired but not applied"), 0644); err != nil {
		t.Fatal(err)
	}
	if RecentlyVerified(path, now) {
		t.Fatal("old acknowledgement accepted new desired rules")
	}
	if err := Invalidate(path); err != nil {
		t.Fatal(err)
	}
	if err := Invalidate(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath(path), []byte(now.UTC().Format(time.RFC3339Nano)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if RecentlyVerified(path, now) {
		t.Fatal("timestamp without content acknowledgement accepted")
	}
}

func TestMarkVerifiedRequiresReadableDesiredRules(t *testing.T) {
	if err := MarkVerified(filepath.Join(t.TempDir(), "absent.nft"), time.Now()); err == nil {
		t.Fatal("missing desired rules were acknowledged")
	}
}
