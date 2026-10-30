// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrivateLeaseMACDoesNotInventVendorOrOS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dnsmasq.leases")
	lease := fmt.Sprintf("%d 4e:20:15:aa:e0:67 192.0.2.2 * *\n", time.Now().Add(time.Hour).Unix())
	if err := os.WriteFile(path, []byte(lease), 0o600); err != nil {
		t.Fatal(err)
	}
	leases, err := readDnsmasqLeases(path)
	if err != nil {
		t.Fatal(err)
	}
	clients := correlateClients(leases, nil, nil, nil, nil)
	if len(clients) != 1 || clients[0].MAC != "4e:20:15:aa:e0:67" || clients[0].Vendor != "" || clients[0].InferredOSFamily != "" {
		t.Fatalf("private MAC became a vendor/OS identity: %+v", clients)
	}
}
