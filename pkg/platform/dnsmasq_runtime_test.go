// SPDX-License-Identifier: BSD-3-Clause

package platform

import "testing"

func TestManagedDnsmasqConfigFile(t *testing.T) {
	for _, tt := range []struct {
		name     string
		defaults Defaults
		want     string
	}{
		{"linux", Defaults{OS: OSLinux, RuntimeDir: "/run/routerd", DnsmasqConfigFile: "/usr/local/etc/routerd/dnsmasq.conf"}, "/run/routerd/dnsmasq.conf"},
		{"linux-custom-runtime", Defaults{OS: OSLinux, RuntimeDir: "/tmp/routerd-test/", DnsmasqConfigFile: "/etc/routerd/dnsmasq.conf"}, "/tmp/routerd-test/dnsmasq.conf"},
		{"freebsd", Defaults{OS: OSFreeBSD, RuntimeDir: "/var/run/routerd", DnsmasqConfigFile: "/usr/local/etc/routerd/dnsmasq.conf"}, "/usr/local/etc/routerd/dnsmasq.conf"},
		{"other", Defaults{OS: OSOther, RuntimeDir: "/tmp/routerd", DnsmasqConfigFile: "/etc/routerd/dnsmasq.conf"}, "/etc/routerd/dnsmasq.conf"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.defaults.ManagedDnsmasqConfigFile(); got != tt.want {
				t.Fatalf("managed dnsmasq config = %q, want %q", got, tt.want)
			}
		})
	}
}
