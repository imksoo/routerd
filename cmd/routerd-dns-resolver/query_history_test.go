// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"database/sql"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/miekg/dns"
)

func awaitQueryHistory(t *testing.T, d *daemon, count int) (*logstore.DNSQueryLog, []logstore.DNSQuery) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		d.queryLogMu.Lock()
		active := d.queryLog
		d.queryLogMu.Unlock()
		if active != nil {
			rows, err := active.List(context.Background(), logstore.DNSQueryFilter{Limit: count + 1})
			if err == nil && len(rows) >= count {
				return active, rows
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("query history writer did not record expected rows")
	return nil, nil
}

func TestReloadWaitsForRetiredHistoryOperation(t *testing.T) {
	d, config, configPath := queryLogReloadFixture(t)
	oldPath := filepath.Join(filepath.Dir(configPath), "old.db")
	config.Spec.QueryLog = api.DNSResolverQueryLogSpec{Enabled: true, Path: oldPath}
	writeRuntimeConfig(t, configPath, config)
	if _, err := d.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.queryLogMu.Lock()
	oldGeneration := d.queryHistory.generation
	d.queryLogMu.Unlock()
	blocker, err := sql.Open("sqlite", oldPath)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	connection, err := blocker.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer connection.ExecContext(context.Background(), "ROLLBACK")
	d.enqueueQueryHistory(logstore.DNSQuery{Timestamp: time.Now(), QuestionName: "blocked.example"})
	awaitHistoryCondition(t, func() bool {
		if d.queryHistory.operation.TryLock() {
			d.queryHistory.operation.Unlock()
			return false
		}
		return true
	})
	newPath := filepath.Join(filepath.Dir(configPath), "new.db")
	config.Spec.QueryLog.Path = newPath
	writeRuntimeConfig(t, configPath, config)
	reloaded := make(chan error, 1)
	go func() {
		_, err := d.reload(context.Background())
		reloaded <- err
	}()
	// Opening the replacement precedes publication. The old worker is holding
	// a real SQLite write lock wait, so publication must still wait for it.
	awaitHistoryCondition(t, func() bool { _, err := os.Stat(newPath); return err == nil })
	select {
	case err := <-reloaded:
		t.Fatalf("reload retired an in-flight history operation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	awaitHistoryCondition(t, func() bool { return d.queryHistory.writeErrors.Load() > 0 })
	if _, err := connection.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-reloaded:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reload did not finish after bounded history operation")
	}
	if err := os.Remove(oldPath); err != nil {
		t.Fatal(err)
	}
	before := d.queryHistory.dropped.Load()
	d.queryHistory.queue <- queuedQuery{generation: oldGeneration, query: logstore.DNSQuery{Timestamp: time.Now()}}
	awaitHistoryCondition(t, func() bool { return d.queryHistory.dropped.Load() > before })
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("stale queued query recreated the retired database: %v", err)
	}
}

func awaitHistoryCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("history operation did not reach expected state")
}

func TestTCPQueriesAndShutdownDoNotWaitForLockedHistory(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	d := newTestDaemon(t, "", testResolverConfig([]int{port}), false)
	path := filepath.Join(t.TempDir(), "queries.db")
	d.queryLog, err = logstore.OpenDNSQueryLog(path)
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	connection, err := blocker.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer connection.ExecContext(context.Background(), "ROLLBACK")
	server := &dns.Server{Listener: listener, Net: "tcp", Handler: dns.HandlerFunc(d.handleDNS)}
	go server.ActivateAndServe()
	defer server.Shutdown()
	client := &dns.Client{Net: "tcp", Timeout: time.Second}
	transport, err := client.Dial(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	request := new(dns.Msg)
	request.SetQuestion("router.lab.example.", dns.TypeA)
	started := time.Now()
	for range 4 {
		response, _, err := client.ExchangeWithConn(request, transport)
		if err != nil || response.Rcode != dns.RcodeSuccess {
			t.Fatalf("history lock interrupted repeated TCP resolution: %v, %v", response, err)
		}
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("TCP DNS waited for SQLite history: %s", elapsed)
	}
	// Fill beyond the fixed capacity while the one writer is blocked. Every
	// enqueue must finish and the loss must be observable, not silently buffered.
	for range queryHistoryCapacity + 32 {
		d.recordQuery("192.0.2.1:53000", request, resolveResult{ResponseCode: "NOERROR"}, time.Millisecond)
	}
	if d.queryHistory.dropped.Load() == 0 {
		t.Fatal("saturated history queue did not report dropped queries")
	}
	started = time.Now()
	d.closeQueryLog()
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("shutdown waited for locked query history: %s", elapsed)
	}
}
