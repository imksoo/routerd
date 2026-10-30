// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/miekg/dns"
)

type lifetimeTransport struct {
	queryStarted chan struct{}
	idleClosed   chan struct{}
}

func (t *lifetimeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	wire, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	var msg dns.Msg
	if err := msg.Unpack(wire); err != nil {
		return nil, err
	}
	if len(msg.Question) == 1 && msg.Question[0].Name == "in-flight.example." {
		close(t.queryStarted)
	}
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func (t *lifetimeTransport) CloseIdleConnections() {
	select {
	case t.idleClosed <- struct{}{}:
	default:
	}
}

func TestRetiredUpstreamCancelsQueriesAndClosesIdleConnections(t *testing.T) {
	pool, err := newUpstreamPool([]string{"https://resolver.example/dns-query", "https://backup.example/dns-query"}, upstreamPoolConfig{QueryTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	transport := &lifetimeTransport{queryStarted: make(chan struct{}), idleClosed: make(chan struct{}, 1)}
	backup := &lifetimeTransport{queryStarted: make(chan struct{}), idleClosed: make(chan struct{}, 1)}
	pool.upstreams[0].client.Transport = transport
	pool.upstreams[1].client.Transport = backup
	owner, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(owner, nil)
	query := new(dns.Msg)
	query.SetQuestion("in-flight.example.", dns.TypeA)
	wire, err := query.Pack()
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := pool.Exchange(context.Background(), wire, nil)
		result <- err
	}()
	select {
	case <-transport.queryStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("query did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("retired pool query = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("retired pool query survived its owner")
	}
	for _, retired := range []*lifetimeTransport{transport, backup} {
		select {
		case <-retired.idleClosed:
		case <-time.After(3 * time.Second):
			t.Fatal("retired HTTP transport retained idle connections")
		}
	}
	if _, err := pool.Exchange(context.Background(), wire, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("new query entered retired pool: %v", err)
	}
}
