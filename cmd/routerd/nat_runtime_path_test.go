// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"flag"
	"github.com/imksoo/routerd/pkg/platform"
	"io"
	"path/filepath"
	"testing"
)

func TestApplyNATUsesRunningControllerConfigPath(t *testing.T) {
	if platformDefaults.OS != platform.OSLinux {
		t.Skip("Linux nftables")
	}
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	flags := registerApplyFlags(fs, true, true)
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	opts := applyChainControllerOptions(flags.applyOptions("router.yaml"))
	want := filepath.Join(platformDefaults.RuntimeDir, "nat44.nft")
	if opts.NftablesPath != want {
		t.Fatalf("apply NAT path=%q, running controller uses %q", opts.NftablesPath, want)
	}
}
