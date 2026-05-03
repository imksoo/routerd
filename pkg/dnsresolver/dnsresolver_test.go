package dnsresolver

import (
	"testing"

	"routerd/pkg/api"
)

func TestValidateAcceptsDNSResolverSources(t *testing.T) {
	err := Validate(api.DNSResolverSpec{
		Listen: []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}, Port: 5053}},
		Sources: []api.DNSResolverSourceSpec{
			{Kind: "zone", Match: []string{"lab.example"}, ZoneRef: []string{"DNSZone/lan"}},
			{Kind: "forward", Match: []string{"transix.jp"}, Upstreams: []string{"udp://[2001:db8::53]:53"}},
			{Kind: "upstream", Match: []string{"."}, Upstreams: []string{"https://1.1.1.1/dns-query", "tls://dns.google", "quic://dns.google", "udp://8.8.8.8:53"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsUnsupportedUpstreamScheme(t *testing.T) {
	err := Validate(api.DNSResolverSpec{
		Sources: []api.DNSResolverSourceSpec{{Kind: "upstream", Match: []string{"."}, Upstreams: []string{"http://dns.example/query"}}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestNormalizeUpstreamAcceptsBareAddresses(t *testing.T) {
	tests := map[string]string{
		"2001:db8::53":   "udp://[2001:db8::53]:53",
		"192.0.2.53":     "udp://192.0.2.53:53",
		"dns.example":    "udp://dns.example:53",
		"dns.example:54": "udp://dns.example:54",
	}
	for raw, want := range tests {
		if got := NormalizeUpstream(raw); got != want {
			t.Fatalf("NormalizeUpstream(%q) = %q, want %q", raw, got, want)
		}
		if err := ValidateUpstreamURL(raw); err != nil {
			t.Fatalf("ValidateUpstreamURL(%q): %v", raw, err)
		}
	}
}
