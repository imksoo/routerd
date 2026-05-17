// SPDX-License-Identifier: BSD-3-Clause

package dpi

import (
	"strings"
	"testing"
)

func TestClassifyTLSSNIFromPayload(t *testing.T) {
	payload := MinimalTLSClientHello("routerd.example")
	got := Classify(ClassifyRequest{L4Payload: payload, TransportProtocol: "tcp", DstPort: 443})
	if got.TLSSNI != "routerd.example" || got.AppName != "tls" || got.AppConfidence < 80 {
		t.Fatalf("classification = %+v", got)
	}
	if got.ApplicationProtocol != "tls" || got.DetectedProtocol != "tls" || got.Category != "web" || got.Confidence != got.AppConfidence || got.Metadata["tls.sni"] != "routerd.example" {
		t.Fatalf("typed classification = %+v metadata=%+v", got, got.Metadata)
	}
}

func TestClassifyTLSSNIFromIPv4Packet(t *testing.T) {
	payload := MinimalTLSClientHello("routerd.example")
	packet := append([]byte{
		0x45, 0x00, 0x00, 0x00, 0, 0, 0, 0, 64, 6, 0, 0,
		172, 18, 0, 101,
		198, 51, 100, 10,
		0xcf, 0xb0, 0x01, 0xbb,
		0, 0, 0, 0,
		0, 0, 0, 0,
		0x50, 0x18, 0, 0, 0, 0, 0, 0,
	}, payload...)
	got := Classify(ClassifyRequest{Packet: packet})
	if got.TLSSNI != "routerd.example" || got.SrcAddress != "172.18.0.101" || got.DstPort != 443 {
		t.Fatalf("classification = %+v", got)
	}
}

func TestClassifyHTTPHost(t *testing.T) {
	got := Classify(ClassifyRequest{L4Payload: []byte("GET / HTTP/1.1\r\nHost: www.example.com\r\n\r\n")})
	if got.HTTPHost != "www.example.com" || got.AppName != "http" {
		t.Fatalf("classification = %+v", got)
	}
}

func TestClassifyDNSQuery(t *testing.T) {
	payload := []byte{
		0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0,
		0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e',
		0x03, 'c', 'o', 'm', 0x00,
		0x00, 0x01, 0x00, 0x01,
	}
	got := Classify(ClassifyRequest{L4Payload: payload, TransportProtocol: "udp", DstPort: 53})
	if got.DNSQuery != "example.com" || got.AppName != "dns" {
		t.Fatalf("classification = %+v", got)
	}
}

func TestClassifyRejectsBinaryDNSLikeUDP(t *testing.T) {
	payload := []byte{
		0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0,
		0x08, 0x9a, 0xfe, 0x00, 0x10, 'n', 'o', 'i', 's', 'e',
		0x00, 0x99, 0x99, 0x00, 0x01,
	}
	got := Classify(ClassifyRequest{L4Payload: payload, TransportProtocol: "udp", DstPort: 3479})
	if got.AppName == "dns" || got.DNSQuery != "" {
		t.Fatalf("binary payload misclassified as DNS: %+v", got)
	}
}

func TestClassifySTUNBeforeDNS(t *testing.T) {
	payload := []byte{
		0x00, 0x01, 0x00, 0x00,
		0x21, 0x12, 0xa4, 0x42,
		0x63, 0x21, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd,
	}
	got := Classify(ClassifyRequest{L4Payload: payload, TransportProtocol: "udp", DstPort: 41641})
	if got.AppName != "tailscale" || got.DNSQuery != "" || got.Reason != "tailscale_stun_magic_cookie" {
		t.Fatalf("classification = %+v", got)
	}

	got = Classify(ClassifyRequest{L4Payload: payload, TransportProtocol: "udp", DstPort: 49152})
	if got.AppName != "stun" || got.DNSQuery != "" || got.Reason != "stun_magic_cookie" {
		t.Fatalf("classification on ephemeral port = %+v", got)
	}
}

func TestClassifyWireGuardAndTailscalePorts(t *testing.T) {
	wireguard := make([]byte, 148)
	wireguard[0] = 0x01
	got := Classify(ClassifyRequest{L4Payload: wireguard, TransportProtocol: "udp", DstPort: 41641})
	if got.AppName != "tailscale" || got.AppCategory != "vpn" || got.Reason != "tailscale_wireguard_message" {
		t.Fatalf("tailscale classification = %+v", got)
	}

	got = Classify(ClassifyRequest{L4Payload: wireguard, TransportProtocol: "udp", DstPort: 51820})
	if got.AppName != "wireguard" || got.AppCategory != "vpn" || got.Reason != "wireguard_message_type" {
		t.Fatalf("wireguard classification = %+v", got)
	}
}

func TestClassifyTailscaleDNSQuery(t *testing.T) {
	payload := dnsQueryPayload("derp10.tailscale.com")
	got := Classify(ClassifyRequest{L4Payload: payload, TransportProtocol: "udp", DstPort: 53})
	if got.AppName != "tailscale" || got.DNSQuery != "derp10.tailscale.com" || got.Reason != "tailscale_dns_query" {
		t.Fatalf("classification = %+v", got)
	}
}

func TestClassifyNBNSQuery(t *testing.T) {
	payload := []byte{
		0x12, 0x34, 0x01, 0x10, 0x00, 0x01, 0, 0, 0, 0, 0, 0,
		0x20,
		'E', 'M', 'E', 'B', 'E', 'J', 'E', 'O',
		'C', 'A', 'C', 'A', 'C', 'A', 'C', 'A',
		'C', 'A', 'C', 'A', 'C', 'A', 'C', 'A',
		'C', 'A', 'C', 'A', 'C', 'A', 'A', 'B',
		0x00,
		0x00, 0x20, 0x00, 0x01,
	}
	got := Classify(ClassifyRequest{L4Payload: payload, TransportProtocol: "udp", DstPort: 137})
	if got.AppName != "netbios" || got.DNSQuery != "LAIN<0x01>" || got.Reason != "nbns_query" {
		t.Fatalf("classification = %+v", got)
	}
}

func dnsQueryPayload(name string) []byte {
	payload := []byte{0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	for _, label := range strings.Split(name, ".") {
		payload = append(payload, byte(len(label)))
		payload = append(payload, []byte(label)...)
	}
	payload = append(payload, 0x00, 0x00, 0x01, 0x00, 0x01)
	return payload
}
