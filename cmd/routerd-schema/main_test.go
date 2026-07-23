// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestConfigSchemaDescribesTunnelInterfacePeerAddress(t *testing.T) {
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Error(err)
		}
	})
	encoded, err := json.Marshal(configSchema())
	if err != nil {
		t.Fatal(err)
	}
	got := string(encoded)
	for _, want := range []string{
		`"peerAddress":`,
		`PeerAddress is the explicit IPv4 inner destination`,
		`FreeBSD and rejected on Linux; routerd never derives it from the CIDR.`,
		`"kind":{"const":"TunnelInterface"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated config schema is missing %q", want)
		}
	}
}
