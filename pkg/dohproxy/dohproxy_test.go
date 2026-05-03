package dohproxy

import (
	"reflect"
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestCloudflaredCommand(t *testing.T) {
	command, args, err := Command(api.DoHProxySpec{
		Backend:       BackendCloudflared,
		ListenAddress: "127.0.0.1",
		ListenPort:    5053,
		Upstreams:     []string{"https://1.1.1.1/dns-query", "https://dns.google/dns-query"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if command != "cloudflared" {
		t.Fatalf("command = %q", command)
	}
	want := []string{"proxy-dns", "--address", "127.0.0.1", "--port", "5053", "--upstream", "https://1.1.1.1/dns-query", "--upstream", "https://dns.google/dns-query"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v", args)
	}
}

func TestNativeCommandUsesInternalServer(t *testing.T) {
	command, args, err := Command(api.DoHProxySpec{
		Backend:   BackendNative,
		Upstreams: []string{"https://1.1.1.1/dns-query"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if command != "" || args != nil {
		t.Fatalf("native command = %q %#v", command, args)
	}
}

func TestValidateAcceptsNativeDNSUpstreamSchemes(t *testing.T) {
	err := Validate(api.DoHProxySpec{
		Backend: BackendNative,
		Upstreams: []string{
			"https://1.1.1.1/dns-query",
			"tls://dns.example",
			"quic://dns.example:853",
			"udp://[2001:db8::53]:53",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsUnsupportedDNSURL(t *testing.T) {
	err := Validate(api.DoHProxySpec{Backend: BackendNative, Upstreams: []string{"http://1.1.1.1/dns-query"}})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestExternalBackendsStillRequireHTTPS(t *testing.T) {
	err := Validate(api.DoHProxySpec{Backend: BackendCloudflared, Upstreams: []string{"tls://dns.example"}})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDNSCryptConfig(t *testing.T) {
	config, err := DNSCryptConfig(api.DoHProxySpec{Backend: BackendDNSCrypt, Upstreams: []string{"https://dns.google/dns-query"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(config, "listen_addresses = ['127.0.0.1:5053']") || !strings.Contains(config, "https://dns.google/dns-query") {
		t.Fatalf("unexpected config:\n%s", config)
	}
}
