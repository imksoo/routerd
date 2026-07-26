// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestConntrackdConfigUsesKernelReplicationAndFailoverTolerantTCPState(t *testing.T) {
	data, err := ConntrackdConfig(api.ConntrackdSyncSpec{
		Interface: "ens19", LocalAddress: "172.18.0.2", PeerAddress: "172.18.0.3",
		IgnoreIPv4: []string{"192.168.123.129", "172.18.0.2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"Mode FTFW", "CommitTimeout 180", "DisableExternalCache no", "StartupResync yes", "TCPWindowTracking no",
		"IPv4_address 172.18.0.2", "IPv4_Destination_Address 172.18.0.3",
		"Interface ens19", "Port 3780", "IPv4_address 172.18.0.2",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("config missing %q:\n%s", want, got)
		}
	}
}

func TestConntrackdConfigRejectsInvalidPeer(t *testing.T) {
	if _, err := ConntrackdConfig(api.ConntrackdSyncSpec{Interface: "ens19", LocalAddress: "172.18.0.2", PeerAddress: "bad"}); err == nil {
		t.Fatal("expected invalid peer error")
	}
}
