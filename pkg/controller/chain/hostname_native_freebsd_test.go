//go:build freebsd

// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestHostnameNativeFreeBSDPrivateJailLifecycle(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_HOSTNAME_JAIL") != "1" {
		t.Skip("explicit private FreeBSD jail gate")
	}
	if os.Geteuid() != 0 {
		t.Fatal("private jail fixture requires root")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	paths := map[string]string{}
	for _, name := range []string{"hostname", "sysrc", "jail", "jls", "jexec", "sysctl"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		paths[name] = path
	}
	run := func(c context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(c, paths[name], args...).CombinedOutput()
	}
	jailed, err := run(ctx, "sysctl", "-n", "security.jail.jailed")
	if err != nil || strings.TrimSpace(string(jailed)) != "0" {
		t.Fatalf("outer scope must be unjailed: %q %v", jailed, err)
	}
	outer, err := run(ctx, "hostname")
	if err != nil {
		t.Fatal(err)
	}
	name := "routerd-hostname-" + strconv.Itoa(os.Getpid())
	before, err := run(ctx, "jls", "-n", "jid", "name")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range strings.Fields(string(before)) {
		if field == "name="+name {
			t.Fatal("private jail name already exists")
		}
	}
	if out, err := run(ctx, "jail", "-c", "name="+name, "path=/", "persist", "vnet", "host.hostname=initial.invalid"); err != nil {
		t.Fatalf("create private jail: %q %v", out, err)
	}
	jid := ""
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		current, err := run(cleanup, "jls", "-n", "-j", name, "jid", "name")
		if err != nil || jid == "" || !strings.Contains(string(current), "jid="+jid+" ") || !strings.Contains(string(current), "name="+name) {
			t.Errorf("unconfirmed jail identity at cleanup: %q %v", current, err)
			return
		}
		if out, err := run(cleanup, "jail", "-r", jid); err != nil {
			t.Errorf("remove owned jail: %q %v", out, err)
			return
		}
		after, err := run(cleanup, "jls", "-n", "jid", "name")
		if err != nil || string(after) != string(before) {
			t.Errorf("jail census changed after cleanup: %q %v", after, err)
		}
		afterHost, err := run(cleanup, "hostname")
		if err != nil || string(afterHost) != string(outer) {
			t.Errorf("outer hostname changed: %q -> %q %v", outer, afterHost, err)
		}
	})
	got, err := run(ctx, "jls", "-n", "-j", name, "jid")
	if err != nil {
		t.Fatal(err)
	}
	jid = strings.TrimPrefix(strings.TrimSpace(string(got)), "jid=")
	if value, err := strconv.ParseUint(jid, 10, 32); err != nil || value == 0 {
		t.Fatalf("invalid created jail ID %q", got)
	}
	c, _ := hostnameFixture(t)
	statePath := filepath.Join(t.TempDir(), "native-state.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c.Store = store
	c.OS = platform.OSFreeBSD
	c.ConfigPath = filepath.Join(t.TempDir(), "rc.conf")
	const unrelated = "# private fixture\nfixture_other=keep\n"
	if err := os.WriteFile(c.ConfigPath, []byte(unrelated), 0644); err != nil {
		t.Fatal(err)
	}
	commands, setters := 0, 0
	c.Command = func(operation context.Context, command string, args ...string) ([]byte, error) {
		commands++
		switch command {
		case "hostname":
			if len(args) > 1 {
				return nil, fmt.Errorf("unexpected hostname args %v", args)
			}
			if len(args) == 1 {
				setters++
			}
			return run(operation, "jexec", append([]string{name, paths["hostname"]}, args...)...)
		case "sysrc":
			if len(args) < 3 || args[0] != "-f" || args[1] != c.ConfigPath {
				return nil, fmt.Errorf("refuse shared rc.conf command %v", args)
			}
			return run(operation, "sysrc", args...)
		default:
			return nil, fmt.Errorf("unexpected native command %s", command)
		}
	}
	assertName := func(want string) {
		t.Helper()
		out, err := run(ctx, "jexec", name, paths["hostname"])
		if err != nil || strings.TrimSpace(string(out)) != want {
			t.Fatalf("jailed hostname=%q want=%q err=%v", out, want, err)
		}
	}
	if err := c.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	assertName("routerd-fixture.invalid")
	if status := store.ObjectStatus(api.NetAPIVersion, "Hostname", "appliance"); status["phase"] != "Applied" {
		t.Fatalf("native status=%+v", status)
	}
	contents, err := os.ReadFile(c.ConfigPath)
	if err != nil || !strings.Contains(string(contents), "fixture_other=keep") {
		t.Fatalf("unrelated private config lost: %q %v", contents, err)
	}
	if err := c.Reconcile(ctx); err != nil || setters != 1 {
		t.Fatalf("no-op repeated setter: %d %v", setters, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	c.Store = next
	c.Router.Spec.Resources[0].Spec = api.HostnameSpec{Hostname: "routerd-reopened.invalid", Managed: true}
	if err := c.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	assertName("routerd-reopened.invalid")
	if out, err := run(ctx, "jexec", name, paths["hostname"], "external.invalid"); err != nil {
		t.Fatalf("fixture external writer: %q %v", out, err)
	}
	previousCommands := commands
	c.Router = &api.Router{}
	if err := c.Reconcile(ctx); err != nil || commands != previousCommands {
		t.Fatalf("removed declaration executed commands: %d -> %d %v", previousCommands, commands, err)
	}
	assertName("external.invalid")
	after, err := run(ctx, "hostname")
	if err != nil || string(after) != string(outer) {
		t.Fatalf("outer hostname changed: %q -> %q %v", outer, after, err)
	}
	t.Logf("native hostname/setters=%d; real private sysrc preserved unrelated config; outer hostname unchanged", setters)
}
