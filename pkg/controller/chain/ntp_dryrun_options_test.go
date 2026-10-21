// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"os"
	"path/filepath"
	"testing"
)

func TestNTPChainDryRunPreservesFilesAndSkipsServiceCommands(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	for _, server := range []bool{false, true} {
		t.Run(map[bool]string{false: "client", true: "server"}[server], func(t *testing.T) {
			router := ntpRouter(api.NTPClientSpec{Provider: "chrony", Managed: true, Servers: []string{"ntp.example.net"}})
			if server {
				router = ntpServerRouter(api.NTPServerSpec{Provider: "chrony", Managed: true, Servers: []string{"ntp.example.net"}, AllowCIDRs: []string{"192.0.2.0/24"}})
			}
			path := filepath.Join(t.TempDir(), "chrony.conf")
			if err := os.WriteFile(path, []byte("existing configuration\n"), 0600); err != nil {
				t.Fatal(err)
			}
			commands := 0
			command := func(context.Context, string, ...string) ([]byte, error) { commands++; return nil, nil }
			client, service := ntpControllersForOptions(router, bus.New(), mapStore{}, Options{DryRunNTP: true})
			client.ConfigPath = path
			client.Command = command
			service.ConfigPath = path
			service.Command = command
			var err error
			if server {
				err = service.Reconcile(context.Background())
			} else {
				err = client.Reconcile(context.Background())
			}
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != "existing configuration\n" || commands != 0 {
				t.Fatalf("dry-run modified NTP: config=%q commands=%d", after, commands)
			}
		})
	}
}
