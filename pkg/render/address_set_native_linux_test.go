//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

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

func TestNativeNATAddressSetReplacement(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires explicit isolated Linux network namespace")
	}
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	host := os.Getenv("ROUTERD_HOST_NETNS")
	if !strings.HasPrefix(host, "net:[") || !strings.HasSuffix(host, "]") || host == current {
		t.Fatal("outer network namespace isolation is unverified")
	}
	nft := func(t *testing.T, input string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "nft", args...)
		cmd.Stdin = strings.NewReader(input)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("nft %v: %v\n%s\n%s", args, err, out, input)
		}
		return string(out)
	}
	for _, mode := range []string{"runtime", "full-render"} {
		t.Run(mode, func(t *testing.T) {
			nft(t, "add table ip routerd_nat\ndelete table ip routerd_nat\nadd table ip6 routerd_nat\ndelete table ip6 routerd_nat\n", "-f", "-")
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan0"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "allowed"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "LocalServiceRedirect"}, Metadata: api.ObjectMeta{Name: "dns"}, Spec: api.LocalServiceRedirectSpec{Interface: "lan", Rules: []api.LocalServiceRedirectRuleSpec{{Protocols: []string{"udp"}, DestinationSetRef: "IPAddressSet/allowed", DestinationPort: 53, RedirectPort: 15353}}}},
			}}}
			for _, addresses := range [][]string{{"192.0.2.1", "2001:db8::1"}, {"192.0.2.2", "2001:db8::2"}, {"192.0.2.2", "2001:db8::2"}} {
				router.Spec.Resources[1].Spec = api.IPAddressSetSpec{Addresses: addresses}
				var data []byte
				var err error
				if mode == "runtime" {
					data, err = NftablesNAT44RulesForRouter(router, nil)
				} else {
					data, err = NftablesNAT44Rule(router)
				}
				if err != nil {
					t.Fatal(err)
				}
				nft(t, string(data), "-f", "-")
				for i, family := range []string{"ip", "ip6"} {
					out := nft(t, "", "list", "set", family, "routerd_nat", "ip_address_set_allowed")
					if !strings.Contains(out, "elements = { "+addresses[i]+" }") {
						t.Errorf("%s did not replace set with exactly %s:\n%s", family, addresses[i], out)
					}
				}
			}
			// DNS-resolved members are owned by the address-set controller;
			// an unrelated NAT render must not discard them.
			router.Spec.Resources[1].Spec = api.IPAddressSetSpec{Addresses: []string{"192.0.2.2", "2001:db8::2"}, Names: []string{"peer.example"}}
			nft(t, "add element ip routerd_nat ip_address_set_allowed { 192.0.2.99 }\nadd element ip6 routerd_nat ip_address_set_allowed { 2001:db8::99 }\n", "-f", "-")
			var data []byte
			var err error
			if mode == "runtime" {
				data, err = NftablesNAT44RulesForRouter(router, nil)
			} else {
				data, err = NftablesNAT44Rule(router)
			}
			if err != nil {
				t.Fatal(err)
			}
			nft(t, string(data), "-f", "-")
			for i, family := range []string{"ip", "ip6"} {
				out := nft(t, "", "list", "set", family, "routerd_nat", "ip_address_set_allowed")
				if !strings.Contains(out, []string{"192.0.2.99", "2001:db8::99"}[i]) {
					t.Errorf("%s discarded DNS-owned member: %s", family, out)
				}
			}
			nft(t, "", "delete", "table", "ip", "routerd_nat")
			nft(t, "", "delete", "table", "ip6", "routerd_nat")
		})
	}
}
