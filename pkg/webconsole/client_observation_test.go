// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/logstore"
)

func TestClientAccountingDistinguishesObservedZeroFromUnavailable(t *testing.T) {
	clients := correlateClients(nil, nil, []logstore.TrafficFlow{
		{ClientAddress: "192.0.2.1", Accounting: true},
		{ClientAddress: "192.0.2.2", Accounting: false},
	}, nil, nil)
	data, err := json.Marshal(clients)
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		ID         string `json:"id"`
		Accounting bool   `json:"accounting"`
		BytesOut   *int64 `json:"bytesOut"`
		BytesIn    *int64 `json:"bytesIn"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !rows[0].Accounting || rows[1].Accounting || rows[0].BytesOut == nil || *rows[0].BytesOut != 0 || rows[0].BytesIn == nil || *rows[0].BytesIn != 0 {
		t.Fatalf("accounting availability or observed zero lost: %s", data)
	}
}

func TestDnsmasqIPv6IAIDDoesNotMergeUnrelatedClients(t *testing.T) {
	// dnsmasq lease_update_file stores IAID in the second IPv6 field;
	// it is scoped to a client DUID and does not identify a MAC/device.
	path := filepath.Join(t.TempDir(), "dnsmasq.leases")
	data := "duid 00:03:00:01:00:11:22:33:44:55\n" +
		"0 1 2001:db8::1 first 00:03:00:01:00:11:22:33:44:55\n" +
		"0 1 2001:db8::2 second 00:03:00:01:00:11:22:33:44:66\n" +
		"0 00:11:22:33:44:55 192.0.2.1 first 01:00:11:22:33:44:55\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	leases, err := readDnsmasqLeases(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 3 || leases[0].MAC != "" || leases[1].MAC != "" || leases[0].IAID != "1" || leases[0].ClientID == leases[1].ClientID {
		t.Fatalf("IAID became a MAC identity: %+v", leases)
	}
	clients := correlateClients(leases, nil, nil, nil, nil)
	if len(clients) != 3 {
		t.Fatalf("unrelated DUIDs sharing an IAID were merged: %+v", clients)
	}
	neighbors, err := parseIPNeighborJSON([]byte(`[{"dst":"2001:db8::1","dev":"lan0","lladdr":"00:11:22:33:44:55","state":["REACHABLE"]}]`))
	if err != nil {
		t.Fatal(err)
	}
	clients = correlateClients(leases, neighbors, nil, nil, nil)
	if len(clients) != 2 {
		t.Fatalf("actual neighbor MAC did not join the observed IPv6 and IPv4 device: %+v", clients)
	}
	for _, client := range clients {
		if client.ID == "00:11:22:33:44:55" && len(client.Addresses) == 2 && strings.Contains(strings.Join(client.Sources, ","), "dhcpv6") {
			return
		}
	}
	t.Fatalf("actual MAC evidence or IPv6 lease source lost: %+v", clients)
}

func TestClientNeighborEvidenceSurvivesAnotherAddressBecomingStale(t *testing.T) {
	neighbors, err := parseIPNeighborJSON([]byte(`[
		{"dst":"192.0.2.8","dev":"lan0","lladdr":"00:11:22:33:44:55","state":["STALE"]},
		{"dst":"2001:db8::8","dev":"lan0","lladdr":"00:11:22:33:44:55","state":["REACHABLE"]}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	clients := correlateClients(nil, neighbors, nil, nil, nil)
	if len(clients) != 1 || len(clients[0].Addresses) != 2 || !strings.Contains(clients[0].State, "STALE") || !strings.Contains(clients[0].State, "REACHABLE") {
		t.Fatalf("one address hid another address's neighbor evidence: %+v", clients)
	}
	arp := parseFreeBSDARP([]byte("? (192.0.2.8) at 00:11:22:33:44:55 on em0 expires in 100 seconds [ethernet]\n"))
	if len(arp) != 1 || arp[0].State != "" {
		t.Fatalf("ARP cache lifetime does not establish current reachability: %+v", arp)
	}
}
