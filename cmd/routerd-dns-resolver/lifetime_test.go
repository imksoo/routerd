// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestUpstreamStopJoinsActualProbePublication(t *testing.T) {
	address := failoverDualDNSServer(t, dns.HandlerFunc(func(w dns.ResponseWriter, query *dns.Msg) {
		_ = w.WriteMsg(failoverSuccessfulReply(query))
	}))
	pool, err := newUpstreamPool([]string{"udp://" + address}, upstreamPoolConfig{PassThreshold: 1, ProbeInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	pool.Start(t.Context(), func(string, string, string, string, map[string]string) {
		once.Do(func() { close(entered) })
		<-release
	})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("actual probe did not publish")
	}
	done := make(chan struct{})
	go func() { pool.Stop(); close(done) }()
	select {
	case <-done:
		t.Fatal("Stop acknowledged a still-running probe callback")
	case <-time.After(30 * time.Millisecond):
	}
	unblock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("probe owner did not join")
	}
	if _, err := pool.Exchange(t.Context(), dnsProbeQuery(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("retired pool accepted query: %v", err)
	}
}

func TestReloadCancelsAndJoinsOldActualDoHQueries(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	upstream := failoverDoHServer(t, func(_ http.ResponseWriter, r *http.Request, query *dns.Msg) {
		if query.Question[0].Name == "old.example." {
			once.Do(func() { close(started) })
		}
		<-r.Context().Done()
	})
	config := testForwardConfig("old", upstream.URL+"/dns-query")
	config.Spec.Sources[0].QueryTimeout = "1m"
	path := filepath.Join(t.TempDir(), "resolver.json")
	d := newTestDaemon(t, path, config, true)
	d.sources[0].Pool = failoverDoHPool(t, upstreamPoolConfig{QueryTimeout: time.Minute}, upstream)
	oldPool := d.sources[0].Pool
	address := failoverDualDNSServer(t, dns.HandlerFunc(d.handleDNS))
	query := new(dns.Msg)
	query.SetQuestion("old.example.", dns.TypeA)
	queryDone := make(chan error, 1)
	go func() {
		_, _, err := (&dns.Client{Net: "tcp", Timeout: 3 * time.Second}).ExchangeContext(t.Context(), query, address)
		queryDone <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("actual DoH query did not start")
	}
	writeRuntimeConfig(t, path, testResolverConfig(nil))
	if _, err := d.reload(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(d.querySlots) != 0 {
		t.Fatal("reload acknowledged an old admitted query")
	}
	select {
	case err := <-queryDone:
		if err == nil {
			t.Fatal("retired generation sent an answer")
		}
	case <-time.After(time.Second):
		t.Fatal("old TCP query survived acknowledgement")
	}
	if _, err := oldPool.Exchange(t.Context(), dnsProbeQuery(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("old pool revived after reload: %v", err)
	}
	if answer := namespaceQuery(t, address, "router.lab.example."); len(answer.Answer) != 1 {
		t.Fatalf("new generation did not serve after handover: %v", answer)
	}
}

func TestActualAcceptedTCPWriteHasFiniteDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialTCP("tcp", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.SetReadBuffer(1024); err != nil {
		t.Fatal(err)
	}
	accepted, err := (dnsDeadlineListener{Listener: listener}).Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer accepted.Close()
	raw := accepted.(dnsWriteConn).Conn.(*net.TCPConn)
	if err := raw.SetWriteBuffer(1024); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		// Fill only this owned loopback connection while its peer never reads.
		payload := make([]byte, 65535)
		for i := 0; i < 128; i++ {
			if _, err := accepted.Write(payload); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		var timeout net.Error
		if !errors.As(err, &timeout) || !timeout.Timeout() {
			t.Fatalf("unread TCP peer did not hit the actual write deadline: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("actual TCP response write had no finite deadline")
	}
}
