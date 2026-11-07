// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/nftstate"
)

func TestPathMTUApplyRetriesFailedDesiredStateAndWithdrawal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mss.nft")
	old := []byte("table inet routerd_mss { # old\n}\n")
	desired := []byte("table inet routerd_mss { # new\n}\n")
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	if err := nftstate.MarkVerified(path, time.Now()); err != nil {
		t.Fatal(err)
	}
	fail := true
	var applies, deletes int
	controller := PathMTUController{RunNFT: func(_ context.Context, args ...string) ([]byte, error) {
		switch args[0] {
		case "-f":
			applies++
			if fail {
				return []byte("apply failed"), errors.New("nft failed")
			}
		case "delete":
			deletes++
			if fail {
				return []byte("permission denied"), errors.New("nft failed")
			}
		}
		return nil, nil
	}}
	for i := 0; i < 2; i++ {
		if _, err := controller.applyTable(context.Background(), "nft", path, "inet", "routerd_mss", desired); err == nil {
			t.Fatal("failed new rules acknowledged")
		}
		if nftstate.RecentlyVerified(path, time.Now()) {
			t.Fatal("failed rules retained applied receipt")
		}
	}
	fail = false
	if _, err := controller.applyTable(context.Background(), "nft", path, "inet", "routerd_mss", desired); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.applyTable(context.Background(), "nft", path, "inet", "routerd_mss", desired); err != nil {
		t.Fatal(err)
	}
	if applies != 3 {
		t.Fatalf("apply attempts = %d, want 3", applies)
	}
	fail = true
	for i := 0; i < 2; i++ {
		if _, err := controller.applyTable(context.Background(), "nft", path, "inet", "routerd_mss", nil); err == nil {
			t.Fatal("failed withdrawal acknowledged")
		}
	}
	fail = false
	if _, err := controller.applyTable(context.Background(), "nft", path, "inet", "routerd_mss", nil); err != nil {
		t.Fatal(err)
	}
	if deletes != 3 || !nftstate.RecentlyVerified(path, time.Now()) {
		t.Fatalf("withdrawal attempts = %d or missing successful receipt", deletes)
	}
}

func TestPolicyNftFailedReplacementCannotReuseOldReceipt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.nft")
	failPath := filepath.Join(dir, "fail")
	logPath := filepath.Join(dir, "calls")
	nft := filepath.Join(dir, "nft")
	script := "#!/bin/sh\necho \"$@\" >> " + testShellQuote(logPath) + "\nif [ \"$1\" = -f ] && [ -f " + testShellQuote(failPath) + " ]; then exit 1; fi\nexit 0\n"
	for name, content := range map[string][]byte{path: []byte("old"), failPath: nil, nft: []byte(script)} {
		if err := os.WriteFile(name, content, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := nftstate.MarkVerified(path, time.Now()); err != nil {
		t.Fatal(err)
	}
	controller := IPv4PolicyRouteController{}
	apply := func() error {
		return controller.applyNftTable(context.Background(), nft, path, "ip", "routerd_policy", []byte("new"))
	}
	for i := 0; i < 2; i++ {
		if err := apply(); err == nil {
			t.Fatal("failed replacement accepted")
		}
	}
	if err := os.Remove(failPath); err != nil {
		t.Fatal(err)
	}
	if err := apply(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path + ".verified")
	if err != nil {
		t.Fatal(err)
	}
	if err := apply(); err != nil {
		t.Fatal(err)
	}
	current, err := os.Stat(path + ".verified")
	if err != nil {
		t.Fatal(err)
	}
	if !current.ModTime().Equal(info.ModTime()) {
		t.Fatal("skipping apply extended the verification lifetime")
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(log), "\n-f "); count != 3 {
		t.Fatalf("apply attempts = %d, want 3: %s", count, log)
	}
}
