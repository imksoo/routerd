// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/dhcpeventqueue"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestGeneratedHookEntryPersistsWithoutDaemon(t *testing.T) {
	if root := os.Getenv("ROUTERD_TEST_HOOK_ROOT"); root != "" {
		defaults, _ := platform.Current()
		defaults.StateDir = filepath.Join(root, "var", "lib", "routerd")
		defaults.RuntimeDir = filepath.Join(root, "run", "routerd")
		for i, a := range os.Args {
			if a == "--" {
				os.Exit(exitCodeWithHardTimeout(func() error { return runWithDefaults(os.Args[i+1:], os.Environ(), defaults) }, hardTimeout, os.Stderr))
			}
		}
		os.Exit(98)
	}
	// Reproduce the deployed entry exactly: dhcp-script names a symlink to the
	// installed executable, with no --socket added. The helper process changes
	// only platform directories to a fixture; callback arguments are unchanged.
	root := t.TempDir()
	bin := filepath.Join(root, "usr", "local", "sbin", "routerd-dhcp-event-relay")
	hook := filepath.Join(root, "usr", "local", "libexec", "routerd", "dhcp-event-relay")
	for _, dir := range []string{filepath.Dir(bin), filepath.Dir(hook)} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, bin); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bin, hook); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC()
	for _, a := range []string{"old", "del", "add"} {
		cmd := exec.Command(hook, "-test.run=^TestGeneratedHookEntryPersistsWithoutDaemon$", "--", a, "02:00:00:00:00:42", "192.0.2.42", "fixture")
		cmd.Env = append(os.Environ(), "ROUTERD_TEST_HOOK_ROOT="+root, "DNSMASQ_INTERFACE=test0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("callback failed: %v %s", err, out)
		}
	}
	defaults, _ := platform.Current()
	defaults.StateDir = filepath.Join(root, "var", "lib", "routerd")
	q, err := dhcpeventqueue.Open(context.Background(), defaults.DHCPEventQueueFile())
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	for _, a := range []string{"renewed", "removed", "added"} {
		ok, err := q.ProcessOne(context.Background(), func(_ context.Context, e dhcpeventqueue.Event) error {
			if e.Lease.Action != a || e.Lease.Interface != "test0" || e.ObservedAt.Before(before) || e.ObservedAt.After(time.Now()) {
				t.Fatal(e)
			}
			return nil
		})
		if !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	if ok, err := q.ProcessOne(context.Background(), func(context.Context, dhcpeventqueue.Event) error { t.Fatal("duplicate callback"); return nil }); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestQueueSaveFailureReturnsNonzero(t *testing.T) {
	defaults, _ := platform.Current()
	defaults.StateDir = t.TempDir()
	if err := os.MkdirAll(filepath.Dir(defaults.DHCPEventQueueFile()), 0755); err != nil {
		t.Fatal(err)
	}
	err := runWithDefaults([]string{"old", "02:00:00:00:00:42", "192.0.2.42", "fixture"}, nil, defaults)
	if err == nil {
		t.Fatal("expected queue save failure")
	}
}
