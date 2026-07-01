// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestFirewallLoggerSystemdSpecPassesNFLogGroup(t *testing.T) {
	spec := FirewallLoggerSystemdSpec(api.FirewallLogSpec{
		Enabled:    true,
		Path:       "/var/lib/routerd/firewall-logs.db",
		NFLogGroup: 42,
	}, "/run/routerd/dpi-classifier/default.sock")

	want := []string{
		"/usr/local/sbin/routerd-firewall-logger",
		"daemon",
		"--path",
		"/var/lib/routerd/firewall-logs.db",
		"--nflog-group",
		"42",
		"--dpi-socket",
		"/run/routerd/dpi-classifier/default.sock",
	}
	if !reflect.DeepEqual(spec.ExecStart, want) {
		t.Fatalf("ExecStart = %#v, want %#v", spec.ExecStart, want)
	}
}

func TestFirewallLoggerSystemdSpecDefaultsNFLogGroup(t *testing.T) {
	spec := FirewallLoggerSystemdSpec(api.FirewallLogSpec{
		Enabled: true,
		Path:    "/var/lib/routerd/firewall-logs.db",
	}, "")

	want := []string{
		"/usr/local/sbin/routerd-firewall-logger",
		"daemon",
		"--path",
		"/var/lib/routerd/firewall-logs.db",
		"--nflog-group",
		"1",
	}
	if !reflect.DeepEqual(spec.ExecStart, want) {
		t.Fatalf("ExecStart = %#v, want %#v", spec.ExecStart, want)
	}
}
