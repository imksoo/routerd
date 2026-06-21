// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestInventoryReportsConfiguredClients(t *testing.T) {
	t.Setenv("ROUTERD_NETNS_SITE", "aws-leaf")
	t.Setenv("ROUTERD_NETNS_SELF_IP", "10.77.60.4/24")
	t.Setenv("ROUTERD_NETNS_CLIENT_IPS", "aws-client-a=10.77.60.11/24,aws-client-b=10.77.60.16/24")
	req := `{"spec":{"provider":"netns","providerRef":"netns-lab","selfNode":"aws-leaf-a","selfNicRef":"eth1"}}`
	var out bytes.Buffer
	if err := run(strings.NewReader(req), &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	var res observeResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Status.Status != statusSucceeded || len(res.Status.ObservedCandidates) != 2 {
		t.Fatalf("status = %#v", res.Status)
	}
	if res.Status.Self == nil || res.Status.Self.PrivateIPs[0] != "10.77.60.4/32" {
		t.Fatalf("self = %#v", res.Status.Self)
	}
	if res.Status.ObservedCandidates[0].ProviderRef != "netns-lab" || !res.Status.ObservedCandidates[0].Primary {
		t.Fatalf("candidate = %#v", res.Status.ObservedCandidates[0])
	}
}
