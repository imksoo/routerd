// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestPolicyIPv6NativeKernelSourceLossRecovery(t *testing.T) {
	requireSystemPolicyNamespaces(t)
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(t.Context(), "ip", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("ip %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	run("link", "add", "wan0", "type", "veth", "peer", "name", "peer0")
	run("link", "set", "peer0", "up")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "ip", "link", "del", "wan0").CombinedOutput(); err != nil {
			t.Errorf("remove private link: %v %s", err, out)
		}
	})
	run("link", "set", "wan0", "up")
	run("-6", "addr", "add", "2001:db8::2/64", "dev", "wan0", "nodad")
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c := IPv4PolicyRouteController{Router: hostPolicyRouter(true), Store: store, OperatingSystem: platform.OSLinux}
	reconcile := func() {
		t.Helper()
		if err := c.reconcileIPv6HostPolicies(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	reconcile()
	if !strings.Contains(run("-6", "route", "show", "table", "120"), "src 2001:db8::2") {
		t.Fatal("source not installed")
	}
	run("-6", "addr", "del", "2001:db8::2/64", "dev", "wan0")
	cleared := run("-6", "route", "show", "table", "120")
	if !strings.Contains(cleared, "default") || strings.Contains(cleared, "src ") {
		t.Fatalf("kernel premise not reproduced: %q", cleared)
	}
	reconcile()
	if strings.Contains(run("-6", "route", "show", "table", "all"), "table 120 ") || strings.TrimSpace(run("-6", "rule", "show", "priority", "10120")) != "" {
		t.Fatal("source loss leaked route/rule")
	}
	run("-6", "addr", "add", "2001:db8::2/64", "dev", "wan0", "nodad")
	reconcile()
	if !strings.Contains(run("-6", "route", "show", "table", "120"), "src 2001:db8::2") {
		t.Fatal("source return not restored")
	}
	c.Router = hostPolicyRouter(false)
	reconcile()
	if strings.Contains(run("-6", "route", "show", "table", "all"), "table 120 ") {
		t.Fatal("withdrawal leaked route")
	}
}
