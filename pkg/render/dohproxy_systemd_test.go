package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestDoHProxySystemdUnit(t *testing.T) {
	unit := string(DoHProxySystemdUnit("cloudflare", api.DoHProxySpec{
		Backend:       "cloudflared",
		ListenAddress: "127.0.0.1",
		ListenPort:    5053,
		Upstreams:     []string{"https://1.1.1.1/dns-query"},
	}, "/usr/local/sbin/routerd-doh-proxy"))
	for _, want := range []string{
		"Description=routerd DoH proxy cloudflare",
		"ExecStart=/usr/local/sbin/routerd-doh-proxy daemon",
		"--upstream \"https://1.1.1.1/dns-query\"",
		"Restart=always",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
}
