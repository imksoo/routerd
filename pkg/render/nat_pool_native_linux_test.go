// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package render

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestNATPoolNativeSyntax(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires explicit isolated Linux network namespace")
	}
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	host := os.Getenv("ROUTERD_HOST_NETNS")
	if !strings.HasPrefix(host, "net:[") || !strings.HasSuffix(host, "]") || current == host {
		t.Fatal("outer network namespace isolation is unverified")
	}
	for _, tc := range []struct {
		name      string
		addresses []string
	}{
		{"numeric-order", []string{"198.51.100.2", "198.51.100.10"}},
		{"three-addresses", []string{"198.51.100.2", "198.51.100.10", "198.51.100.20"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, mapping := range []api.IPv4NATPortMappingSpec{{Type: "auto"}, {Type: "preserve"}, {Type: "range", Start: 40000, End: 40010}} {
				data, err := NftablesNAT44Rules([]NAT44RenderRule{{Name: "pool", EgressInterface: "wan0", SourceRanges: []string{"192.0.2.0/24"}, Translation: api.IPv4NATTranslationSpec{Type: "pool", Addresses: tc.addresses, PortMapping: mapping}}})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				cmd := exec.CommandContext(ctx, "nft", "-c", "-f", "-")
				cmd.Stdin = strings.NewReader(string(data))
				out, err := cmd.CombinedOutput()
				cancel()
				if err != nil {
					t.Errorf("%s pool rejected: %v\n%s\n%s", mapping.Type, err, out, data)
				}
			}
		})
	}
}
