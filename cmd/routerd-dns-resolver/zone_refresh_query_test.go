// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/dnsresolver"
	"github.com/miekg/dns"
)

func TestZoneRefreshPreservesUnchangedForwardQuery(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	upstream := failoverDoHServer(t, func(w http.ResponseWriter, r *http.Request, query *dns.Msg) {
		if query.Question[0].Name == "in-flight.example." {
			once.Do(func() { close(started) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		failoverWriteDNS(t, w, failoverSuccessfulReply(query))
	})
	config := testForwardConfig("forward", upstream.URL+"/dns-query")
	config.Spec = dnsresolver.NormalizeSpec(config.Spec)
	config.Zones = testResolverConfig(nil).Zones
	path := filepath.Join(t.TempDir(), "resolver.json")
	d := newTestDaemon(t, path, config, true)
	d.sources[0].Pool = failoverDoHPool(t, upstreamPoolConfig{QueryTimeout: time.Second}, upstream)
	address := failoverDualDNSServer(t, dns.HandlerFunc(d.handleDNS))
	query := new(dns.Msg)
	query.SetQuestion("in-flight.example.", dns.TypeA)
	done := make(chan error, 1)
	go func() {
		answer, _, err := (&dns.Client{Net: "tcp", Timeout: 3 * time.Second}).ExchangeContext(t.Context(), query, address)
		if err == nil && (answer == nil || len(answer.Answer) != 1) {
			err = fmt.Errorf("missing forwarded answer: %v", answer)
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("forwarded query did not reach independent upstream")
	}
	// Local zone data changes; listener and forwarding intent are unchanged.
	config.Zones[0].Spec.Records[0].IPv4 = "192.0.2.2"
	writeRuntimeConfig(t, path, config)
	reloaded := make(chan error, 1)
	go func() { _, err := d.reload(t.Context()); reloaded <- err }()
	select {
	case err := <-done:
		t.Fatalf("zone refresh interrupted an unrelated query: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("forwarded query did not finish")
	}
	select {
	case err := <-reloaded:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("zone refresh did not finish")
	}
}

func TestZoneRefreshCanceledDrainKeepsCurrentOwner(t *testing.T) {
	config := testResolverConfig(nil)
	config.Spec = dnsresolver.NormalizeSpec(config.Spec)
	path := filepath.Join(t.TempDir(), "resolver.json")
	d := newTestDaemon(t, path, config, true)
	owner := d.sourceCtx
	config.Zones[0].Spec.Records[0].IPv4 = "192.0.2.2"
	writeRuntimeConfig(t, path, config)
	d.queryMu.RLock()
	defer d.queryMu.RUnlock()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := d.reload(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("reload ignored canceled drain: %v", err)
	}
	if owner.Err() != nil || d.sourceCtx != owner {
		t.Fatal("canceled staging retired the serving owner")
	}
	assertZoneARecord(t, d.zones, "router.lab.example.", "192.0.2.1")
}
