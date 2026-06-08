// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestSuperviseClientDaemonsStartsDNSResolverWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	logPath := filepath.Join(dir, "dns.args")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(logPath) + "\nsleep 30\n"
	if err := os.WriteFile(filepath.Join(binDir, "routerd-dns-resolver"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
		Metadata: api.ObjectMeta{Name: "lan-resolver"},
		Spec: api.DNSResolverSpec{Listen: []api.DNSResolverListenSpec{{
			Name: "lan", Addresses: []string{"127.0.0.1"}, Port: 53,
		}}},
	}}}}
	runner := &Runner{
		Router: router,
		Opts:   Options{SuperviseDNSResolvers: true},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.superviseClientDaemons(ctx, nil)

	var data []byte
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		if got, err := os.ReadFile(logPath); err == nil {
			data = got
			break
		}
	}
	cancel()
	if len(data) == 0 {
		t.Fatalf("routerd-dns-resolver was not started")
	}
	got := strings.Fields(string(data))
	for _, want := range []string{
		"daemon",
		"--resource", "lan-resolver",
		"--config-file", "/var/lib/routerd/dns-resolver/lan-resolver/config.json",
		"--socket", "/run/routerd/dns-resolver/lan-resolver.sock",
		"--state-file", "/var/lib/routerd/dns-resolver/lan-resolver/state.json",
		"--event-file", "/var/lib/routerd/dns-resolver/lan-resolver/events.jsonl",
	} {
		if !stringSliceContains(got, want) {
			t.Fatalf("routerd-dns-resolver args missing %q: %v", want, got)
		}
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
