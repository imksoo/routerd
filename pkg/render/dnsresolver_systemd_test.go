package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestDNSResolverSystemdUnit(t *testing.T) {
	unit := string(DNSResolverSystemdUnit("cloudflare", api.DNSResolverSpec{
		Listen:  []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}, Port: 5053}},
		Sources: []api.DNSResolverSourceSpec{{Kind: "upstream", Match: []string{"."}, Upstreams: []string{"https://1.1.1.1/dns-query"}}},
	}, "/usr/local/sbin/routerd-dns-resolver", "/var/lib/routerd/dns-resolver/cloudflare/config.json"))
	for _, want := range []string{
		"Description=routerd DNS resolver cloudflare",
		"ExecStart=/usr/local/sbin/routerd-dns-resolver daemon",
		"--config-file \"/var/lib/routerd/dns-resolver/cloudflare/config.json\"",
		"Restart=always",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
}
