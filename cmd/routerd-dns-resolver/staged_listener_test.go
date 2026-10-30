// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"github.com/miekg/dns"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestNewListenerCannotServeOldConfigurationBeforeHandover(t *testing.T) {
	old := testResolverConfig(nil)
	path := filepath.Join(t.TempDir(), "resolver.json")
	d := newTestDaemon(t, path, old, false)
	port := freeTCPPort(t)
	updated := testResolverConfig([]int{port})
	updated.Zones[0].Spec.Records[0].IPv4 = "192.0.2.2"
	writeRuntimeConfig(t, path, updated)
	d.queryHistory.operation.Lock()
	done := make(chan error, 1)
	go func() { _, err := d.reload(context.Background()); done <- err }()
	released := false
	release := func() {
		if !released {
			d.queryHistory.operation.Unlock()
			released = true
		}
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("reload did not finish after release")
		}
	}
	defer func() {
		if !released {
			release()
		}
	}()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	// Establish that the new socket is actually staged, rather than passing
	// because the reload goroutine has not bound it yet.
	staged := false
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			staged = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !staged {
		t.Fatal("reload did not stage its new listener")
	}
	query := new(dns.Msg)
	query.SetQuestion("router.lab.example.", dns.TypeA)
	answer, _, err := (&dns.Client{Net: "tcp", Timeout: 100 * time.Millisecond}).Exchange(query, address)
	if err == nil {
		t.Fatalf("uncommitted new listener served a response before handover: %v", answer)
	}
	if len(d.querySlots) != 0 {
		t.Fatal("staged requests queued for query admission")
	}
	release()
	assertDNSAnswer(t, address, "router.lab.example.", "192.0.2.2")
}

func TestRetiredListenerCannotEnterNewGenerationAfterQueryWait(t *testing.T) {
	d := newTestDaemon(t, filepath.Join(t.TempDir(), "resolver.json"), testResolverConfig(nil), true)
	listener := &boundListener{Addr: "127.0.0.1:5053"}
	d.stateMu.Lock()
	d.listeners[listener.Addr] = listener
	d.stateMu.Unlock()
	defer func() { d.stateMu.Lock(); clear(d.listeners); d.stateMu.Unlock() }()
	d.queryMu.Lock()
	locked := true
	defer func() {
		if locked {
			d.queryMu.Unlock()
		}
	}()
	writer := new(failureResponseWriter)
	request := new(dns.Msg)
	request.SetQuestion("router.lab.example.", dns.TypeA)
	done := make(chan struct{})
	go func() { d.handleDNSOnListener(writer, request, listener); close(done) }()
	deadline := time.Now().Add(time.Second)
	for len(d.querySlots) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(d.querySlots) == 0 {
		t.Fatal("request did not reach query admission wait")
	}
	d.stateMu.Lock()
	delete(d.listeners, listener.Addr)
	d.generation++
	d.stateMu.Unlock()
	d.queryMu.Unlock()
	locked = false
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retired request did not exit")
	}
	if writer.response != nil {
		t.Fatalf("retired listener used new generation sources: %v", writer.response)
	}
}
