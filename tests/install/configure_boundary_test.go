// SPDX-License-Identifier: BSD-3-Clause

package install_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/platform"
)

func configureEnv() []string {
	return []string{
		"ROUTERD_WAN_INTERFACE=wan0", "ROUTERD_LAN_INTERFACE=lan0",
		"ROUTERD_WAN_ADDRESS=192.0.2.2/24", "ROUTERD_WAN_GATEWAY=192.0.2.1",
		"ROUTERD_MGMT_MODE=separate", "ROUTERD_MGMT_INTERFACE=mgmt0", "ROUTERD_MGMT_ADDRESS=198.51.100.2/24",
		"ROUTERD_ENABLE_USB_PERSISTENCE=no",
	}
}

func TestConfigureProducesCurrentOperatorConfiguration(t *testing.T) {
	for _, mode := range []string{"static", "dhcp"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			env := append(configureEnv(), "ROUTERD_WAN_MODE="+mode, "ROUTERD_WAN_DNS=2001:db8::53 tcp://192.0.2.53:5353 tls://[2001:db8::54]:8853?serverName=dns.example https://dns.example:8443/query")
			out, err := runInstallRawWithEnv(t, dir, env, "configure", "--prefix", filepath.Join(dir, "prefix"), "--non-interactive", "--dry-run")
			if err != nil {
				t.Fatalf("configure: %v\n%s", err, out)
			}
			_, data, found := strings.Cut(out, "# yaml-language-server:")
			if !found {
				t.Fatalf("configuration missing: %s", out)
			}
			router, err := config.LoadBytes([]byte("# yaml-language-server:"+data), "wizard.yaml")
			if err != nil {
				t.Fatalf("load generated config: %v\n%s", err, data)
			}
			if err := config.ValidateForOS(router, platform.OSLinux); err != nil {
				t.Fatalf("validate generated config: %v\n%s", err, data)
			}
			upstreamCount := 0
			for _, resource := range router.Spec.Resources {
				if resource.Kind != "DNSUpstream" {
					continue
				}
				upstreamCount++
				spec, err := resource.DNSUpstreamSpec()
				if err != nil {
					t.Fatal(err)
				}
				switch resource.Metadata.Name {
				case "wizard-1":
					if spec.Protocol != "udp" || spec.Address != "2001:db8::53" {
						t.Fatalf("IPv6 upstream: %+v", spec)
					}
				case "wizard-2":
					if spec.Protocol != "tcp" || spec.Address != "192.0.2.53" || spec.Port != 5353 {
						t.Fatalf("TCP upstream: %+v", spec)
					}
				case "wizard-3":
					if spec.Protocol != "dot" || spec.Address != "2001:db8::54" || spec.Port != 8853 || spec.TLSName != "dns.example" {
						t.Fatalf("TLS upstream: %+v", spec)
					}
				case "wizard-4":
					if spec.Protocol != "doh" || spec.Address != "dns.example" || spec.Port != 8443 || spec.Path != "/query" {
						t.Fatalf("HTTPS upstream: %+v", spec)
					}
				}
			}
			if upstreamCount != 4 {
				t.Fatalf("upstream count=%d", upstreamCount)
			}
			if mode == "static" {
				for _, resource := range router.Spec.Resources {
					if resource.Kind != "IPv4Route" {
						continue
					}
					spec, err := resource.IPv4RouteSpec()
					if err != nil || spec.Device != "wan0" || spec.Gateway != "192.0.2.1" {
						t.Fatalf("route uses logical alias instead of actual interface: %+v, %v", spec, err)
					}
				}
			}
		})
	}
}

func TestConfigureValidatesBeforeReplacingOperatorConfig(t *testing.T) {
	for _, validateOK := range []bool{false, true} {
		t.Run(fmt.Sprint(validateOK), func(t *testing.T) {
			dir := t.TempDir()
			pkg, prefix := filepath.Join(dir, "package"), filepath.Join(dir, "prefix")
			old := filepath.Join(prefix, "etc", "routerd", "router.yaml")
			if err := os.MkdirAll(filepath.Dir(old), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(old, []byte("retained operator config\n"), 0600); err != nil {
				t.Fatal(err)
			}
			exitCode := 23
			if validateOK {
				exitCode = 0
			}
			writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), fmt.Sprintf(`#!/bin/sh
set -eu
[ "$1 $2" = 'serve --sandbox' ] || exit 88
[ "$(cat "$CONFIGURE_OLD")" = 'retained operator config' ] || exit 89
[ ! -e "$CONFIGURE_OLD.backup" ] || exit 90
shift 2
config=
while [ "$#" -gt 0 ]; do
  case "$1" in --config) config=$2; shift 2 ;; *) shift ;; esac
done
[ "$config" = "$CONFIGURE_OLD.configure" ] || exit 91
[ -s "$config" ] || exit 92
exit %d
`, exitCode))
			env := append(configureEnv(), "ROUTERD_WAN_MODE=dhcp", "CONFIGURE_OLD="+old)
			out, err := runInstallRawWithEnv(t, pkg, env, "configure", "--prefix", prefix, "--non-interactive", "--no-apply")
			if (err == nil) != validateOK {
				t.Fatalf("configure error=%v success=%v\n%s", err, validateOK, out)
			}
			got, readErr := os.ReadFile(old)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !validateOK {
				if string(got) != "retained operator config\n" {
					t.Fatalf("failed candidate replaced live config: %q", got)
				}
				if _, err := os.Stat(old + ".backup"); !os.IsNotExist(err) {
					t.Fatalf("failed validation changed backup: %v", err)
				}
			} else {
				if !strings.Contains(string(got), "kind: DHCPv4Client") {
					t.Fatalf("validated candidate not installed: %q", got)
				}
				previous, err := os.ReadFile(old + ".backup")
				if err != nil || string(previous) != "retained operator config\n" {
					t.Fatalf("backup: %q %v", previous, err)
				}
			}
		})
	}
}
