package main

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestParseDNSUpstreamDefaults(t *testing.T) {
	tests := []struct {
		raw     string
		scheme  string
		address string
	}{
		{"https://dns.example/dns-query", "https", ""},
		{"tls://dns.example", "tls", "dns.example:853"},
		{"quic://dns.example:8853", "quic", "dns.example:8853"},
		{"udp://[2001:db8::53]", "udp", "[2001:db8::53]:53"},
	}
	for i, tt := range tests {
		upstream, err := parseDNSUpstream(i, tt.raw)
		if err != nil {
			t.Fatalf("parse %q: %v", tt.raw, err)
		}
		if upstream.Scheme != tt.scheme || upstream.Address != tt.address {
			t.Fatalf("parse %q = scheme %q address %q", tt.raw, upstream.Scheme, upstream.Address)
		}
	}
}

func TestUpstreamPoolFallsBackToSecondUpstream(t *testing.T) {
	failed, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer failed.Close()
	ok, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ok.Close()
	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := ok.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = ok.WriteTo(buf[:n], addr)
		}
	}()

	pool, err := newUpstreamPool([]string{"udp://" + failed.LocalAddr().String(), "udp://" + ok.LocalAddr().String()}, upstreamPoolConfig{
		ProbeInterval: time.Hour,
		ProbeTimeout:  50 * time.Millisecond,
		FailThreshold: 1,
		PassThreshold: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	query := dnsProbeQuery()
	resp, err := pool.Exchange(context.Background(), query, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp) != string(query) {
		t.Fatalf("response = %x, want %x", resp, query)
	}
	snapshot := pool.Snapshot()
	if snapshot[0].Phase != upstreamDown || snapshot[1].Phase != upstreamHealthy {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestLengthPrefixedExchange(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go func() {
		var length uint16
		if err := binary.Read(server, binary.BigEndian, &length); err != nil {
			t.Error(err)
			return
		}
		query := make([]byte, length)
		if _, err := io.ReadFull(server, query); err != nil {
			t.Error(err)
			return
		}
		_ = binary.Write(server, binary.BigEndian, uint16(len(query)))
		_, _ = server.Write(query)
	}()
	query := dnsProbeQuery()
	resp, err := exchangeLengthPrefixed(context.Background(), client, query)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp) != string(query) {
		t.Fatalf("response = %x, want %x", resp, query)
	}
}
