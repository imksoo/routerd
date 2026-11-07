// SPDX-License-Identifier: BSD-3-Clause

package chain

import "testing"

func TestSAMSysctlPreservesDottedNativeInterface(t *testing.T) {
	for _, setting := range []string{"proxy_arp", "accept_local"} {
		got, err := samSysctlPath("net.ipv4.conf.eth0.100." + setting)
		want := "/proc/sys/net/ipv4/conf/eth0.100/" + setting
		if err != nil || got != want {
			t.Fatalf("kernel path = %q, %v; want %q", got, err, want)
		}
	}
	if _, err := samSysctlPath("net.ipv4.conf.eth0/100.proxy_arp"); err == nil {
		t.Fatal("interface token escaped its native sysctl component")
	}
}
