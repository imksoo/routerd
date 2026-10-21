// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"flag"
	"io"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
)

// An unrelated apply must not change the service definition used by the
// running Linux DHCP/RA controller. This checks the actual CLI defaults and
// rendered unit without starting services or touching the host network.
func TestApplyDefaultsPreserveRunningDnsmasqServiceDefinition(t *testing.T) {
	if platformDefaults.OS != platform.OSLinux {
		t.Skip("Linux runtime service contract")
	}
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	flags := registerApplyFlags(fs, true, true)
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	opts := applyChainControllerOptions(flags.applyOptions("router.yaml"))
	pid := filepath.Join(platformDefaults.RuntimeDir, "dnsmasq.pid")
	runningConfig := filepath.Join(platformDefaults.RuntimeDir, "dnsmasq.conf")
	want := render.DnsmasqServiceUnitWithPID(runningConfig, pid, "/usr/sbin/dnsmasq")
	got := render.DnsmasqServiceUnitWithPID(opts.DnsmasqConfig, opts.DnsmasqPID, "/usr/sbin/dnsmasq")
	if !bytes.Equal(got, want) {
		t.Fatalf("apply would replace the running dnsmasq service definition:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestApplyPreservesExplicitDnsmasqConfigPath(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom-dhcp.conf")
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	flags := registerApplyFlags(fs, true, true)
	if err := fs.Parse([]string{"--dnsmasq-file", custom}); err != nil {
		t.Fatal(err)
	}
	opts := applyChainControllerOptions(flags.applyOptions("router.yaml"))
	if opts.DnsmasqConfig != custom {
		t.Fatalf("explicit dnsmasq config = %q, want %q", opts.DnsmasqConfig, custom)
	}
}
