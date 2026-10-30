// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

func TestCanceledRetirementCannotAcknowledgeStaleAppliedDigest(t *testing.T) {
	address := failoverDualDNSServer(t, dns.HandlerFunc(func(w dns.ResponseWriter, query *dns.Msg) {
		_ = w.WriteMsg(failoverSuccessfulReply(query))
	}))
	config := testForwardConfig("primary", "udp://"+address)
	config.Spec.Sources[0].Healthcheck.PassThreshold = 1
	path := filepath.Join(t.TempDir(), "resolver.json")
	writeRuntimeConfig(t, path, config)
	d := newTestDaemon(t, path, config, true)
	t.Cleanup(d.shutdownDNSServers)
	if _, err := d.reload(t.Context()); err != nil {
		t.Fatal(err)
	}
	digest := d.configDigest
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	d.sources[0].Pool.Start(t.Context(), func(string, string, string, string, map[string]string) {
		once.Do(func() { close(entered) })
		<-release
	})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("old probe did not enter publication")
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := d.reload(ctx); done <- err }()
	select {
	case <-d.sourceCtx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("reload did not begin actual old-owner retirement")
	}
	cancel()
	unblock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted reload = %v", err)
	}
	if d.configDigest != "" {
		t.Fatal("interrupted retirement retained an apparently applied digest")
	}
	result, err := d.reloadConfig(t.Context(), digest)
	if err != nil || !result.Reloaded || result.ConfigDigest != digest {
		t.Fatalf("same-digest retry did not restore a live owner: %+v, %v", result, err)
	}
	if d.sourceCtx.Err() != nil {
		t.Fatal("retry acknowledged a canceled owner")
	}
}

func TestCompleteRequestTimeoutBoundsActualDoHLookup(t *testing.T) {
	upstream := failoverDoHServer(t, func(_ http.ResponseWriter, r *http.Request, _ *dns.Msg) {
		<-r.Context().Done()
	})
	config := testForwardConfig("slow", upstream.URL+"/dns-query")
	config.Spec.RequestTimeout = "80ms"
	config.Spec.Sources[0].QueryTimeout = "1m"
	path := filepath.Join(t.TempDir(), "resolver.json")
	d := newTestDaemon(t, path, config, true)
	d.sources[0].Pool = failoverDoHPool(t, upstreamPoolConfig{QueryTimeout: time.Minute}, upstream)
	t.Cleanup(d.shutdownDNSServers)
	address := failoverDualDNSServer(t, dns.HandlerFunc(d.handleDNS))
	query := new(dns.Msg)
	query.SetQuestion("deadline.example.", dns.TypeA)
	started := time.Now()
	answer, _, err := (&dns.Client{Net: "tcp", Timeout: 2 * time.Second}).ExchangeContext(t.Context(), query, address)
	if err != nil || answer.Rcode != dns.RcodeServerFailure {
		t.Fatalf("lookup deadline did not produce SERVFAIL: %v, %v", answer, err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("whole lookup ignored requestTimeout: %v", elapsed)
	}
}

func TestStoppedLeaseOwnerRejectsLaterCallbacks(t *testing.T) {
	config := testResolverConfig(nil)
	d := newTestDaemon(t, filepath.Join(t.TempDir(), "resolver.json"), config, true)
	d.shutdownDNSServers()
	lease := observedLeaseForTest(dhcpLeaseEvent{Action: "added", IP: "192.0.2.55", Hostname: "late"})
	body, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	d.leaseHandler(response, httptest.NewRequest(http.MethodPost, "/v1/leases", bytes.NewReader(body)))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("stopped lease owner acknowledged callback: %d %s", response.Code, response.Body.String())
	}
	query := new(dns.Msg)
	query.SetQuestion("late.lab.example.", dns.TypeA)
	answer, _ := d.zones.Answer(query, nil)
	if answer != nil && len(answer.Answer) != 0 {
		t.Fatal("callback mutated stopped owner")
	}
}

func TestLeaseIPCRejectsTrailingOrUnknownPayload(t *testing.T) {
	config := testResolverConfig(nil)
	d := newTestDaemon(t, filepath.Join(t.TempDir(), "resolver.json"), config, true)
	lease := observedLeaseForTest(dhcpLeaseEvent{Action: "added", IP: "192.0.2.55", Hostname: "invalid"})
	body, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{string(body) + ` {}`, strings.TrimSuffix(string(body), "}") + `,"unexpected":true}`} {
		response := httptest.NewRecorder()
		d.leaseHandler(response, httptest.NewRequest(http.MethodPost, "/v1/leases", strings.NewReader(payload)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("ambiguous lease payload accepted: %d %s", response.Code, response.Body.String())
		}
	}
}
