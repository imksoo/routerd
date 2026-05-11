// SPDX-License-Identifier: BSD-3-Clause

package dpi

import "testing"

func TestClassifyTLSSNIFromPayload(t *testing.T) {
	payload := MinimalTLSClientHello("routerd.example")
	got := Classify(ClassifyRequest{L4Payload: payload, TransportProtocol: "tcp", DstPort: 443})
	if got.TLSSNI != "routerd.example" || got.AppName != "tls" || got.AppConfidence < 80 {
		t.Fatalf("classification = %+v", got)
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
