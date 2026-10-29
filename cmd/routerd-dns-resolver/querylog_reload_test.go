// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/imksoo/routerd/pkg/api"
	resolvercfg "github.com/imksoo/routerd/pkg/dnsresolver"
	"github.com/imksoo/routerd/pkg/logstore"
)

func queryLogReloadFixture(t *testing.T) (*daemon, resolvercfg.RuntimeConfig, string) {
	t.Helper()
	dir := t.TempDir()
	config := testResolverConfig(nil)
	// A resolver waiting for a referenced address has no bound listeners. This
	// exercises the live reload and SQLite paths without modifying host networking.
	config.Spec.Listen = []api.DNSResolverListenSpec{{
		Name: "waiting", Port: 1053,
		AddressFrom: []api.StatusValueSourceSpec{{Resource: "Interface/lan", Field: "status.addresses[0]", Optional: true}},
	}}
	path := filepath.Join(dir, "resolver.json")
	writeRuntimeConfig(t, path, config)
	d := newTestDaemon(t, path, config, false)
	t.Cleanup(d.closeQueryLog)
	return d, config, path
}

func TestReloadEnablesReplacesAndDisablesQueryLog(t *testing.T) {
	d, config, path := queryLogReloadFixture(t)
	request := new(dns.Msg)
	request.SetQuestion("router.lab.example.", dns.TypeA)
	var previous *logstore.DNSQueryLog
	for _, name := range []string{"first.db", "second.db", ""} {
		config.Spec.QueryLog = api.DNSResolverQueryLogSpec{Enabled: name != "", Path: filepath.Join(filepath.Dir(path), name)}
		writeRuntimeConfig(t, path, config)
		if _, err := d.reload(context.Background()); err != nil {
			t.Fatal(err)
		}
		if previous != nil {
			if err := previous.Record(context.Background(), logstore.DNSQuery{Timestamp: time.Now()}); err == nil {
				t.Fatal("replaced query log is still open")
			}
		}
		if name == "" {
			if d.queryLog != nil {
				t.Fatal("disabled query log is still active")
			}
			continue
		}
		active := d.queryLog
		if active == nil || active.Stats().Path != config.Spec.QueryLog.Path {
			t.Fatalf("query log did not switch to %s", config.Spec.QueryLog.Path)
		}
		d.recordQuery("192.0.2.1:53000", request, resolveResult{ResponseCode: "NOERROR"}, time.Millisecond)
		rows, err := active.List(context.Background(), logstore.DNSQueryFilter{Limit: 10})
		if err != nil || len(rows) != 1 || rows[0].QuestionName != "router.lab.example" {
			t.Fatalf("query was not recorded in replacement log: rows=%+v error=%v", rows, err)
		}
		if _, err := d.reload(context.Background()); err != nil {
			t.Fatal(err)
		}
		if d.queryLog != active {
			t.Fatal("unchanged query log was unnecessarily reopened")
		}
		previous = active
	}
}

func TestReloadQueryLogOpenFailureKeepsAppliedRuntime(t *testing.T) {
	d, config, path := queryLogReloadFixture(t)
	config.Spec.QueryLog = api.DNSResolverQueryLogSpec{Enabled: true, Path: filepath.Join(filepath.Dir(path), "active.db")}
	writeRuntimeConfig(t, path, config)
	if _, err := d.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	oldLog, oldDigest, oldGeneration := d.queryLog, d.configDigest, d.generation
	blocker := filepath.Join(filepath.Dir(path), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	config.Spec.QueryLog.Path = filepath.Join(blocker, "new.db")
	config.Zones[0].Spec.Records[0].IPv4 = "192.0.2.99"
	writeRuntimeConfig(t, path, config)
	if _, err := d.reload(context.Background()); err == nil {
		t.Fatal("reload acknowledged an unusable query log")
	}
	if d.queryLog != oldLog || d.configDigest != oldDigest || d.generation != oldGeneration {
		t.Fatal("failed query log open changed the applied runtime")
	}
	request := new(dns.Msg)
	request.SetQuestion("router.lab.example.", dns.TypeA)
	result, err := d.resolve("127.0.0.1:1053", request)
	if err != nil || result.Response == nil || len(result.Response.Answer) != 1 || result.Response.Answer[0].(*dns.A).A.String() != "192.0.2.1" {
		t.Fatalf("failed reload replaced the old DNS answer: result=%+v error=%v", result, err)
	}
	if err := oldLog.Record(context.Background(), logstore.DNSQuery{Timestamp: time.Now()}); err != nil {
		t.Fatalf("failed reload closed active query log: %v", err)
	}
}

func TestReloadClosesStagedQueryLogWhenListenerFails(t *testing.T) {
	d, config, path := queryLogReloadFixture(t)
	config.Spec.QueryLog = api.DNSResolverQueryLogSpec{Enabled: true, Path: filepath.Join(filepath.Dir(path), "active.db")}
	writeRuntimeConfig(t, path, config)
	if _, err := d.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	oldLog, oldDigest, oldGeneration := d.queryLog, d.configDigest, d.generation
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	_, portText, err := net.SplitHostPort(occupied.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	stagedPath := filepath.Join(filepath.Dir(path), "staged.db")
	config.Spec.QueryLog.Path = stagedPath
	config.Spec.Listen = []api.DNSResolverListenSpec{{Name: "blocked", Addresses: []string{"127.0.0.1"}, Port: port}}
	writeRuntimeConfig(t, path, config)
	if _, err := d.reload(context.Background()); err == nil {
		t.Fatal("reload unexpectedly bound an occupied listener")
	}
	if d.queryLog != oldLog || d.configDigest != oldDigest || d.generation != oldGeneration {
		t.Fatal("failed listener changed the applied log or runtime")
	}
	if _, err := os.Stat(stagedPath); err != nil {
		t.Fatalf("new query log was not staged before listener failure: %v", err)
	}
	if fds, err := os.ReadDir("/proc/self/fd"); err == nil {
		for _, fd := range fds {
			target, _ := os.Readlink(filepath.Join("/proc/self/fd", fd.Name()))
			if strings.HasPrefix(target, stagedPath) {
				t.Fatalf("failed reload leaked staged query log descriptor: %s", target)
			}
		}
	}
}

func TestRunAlwaysOwnsQueryLogCleanup(t *testing.T) {
	d, config, path := queryLogReloadFixture(t)
	config.Spec.QueryLog = api.DNSResolverQueryLogSpec{Enabled: true, Path: filepath.Join(filepath.Dir(path), "enabled-after-start.db")}
	writeRuntimeConfig(t, path, config)
	if _, err := d.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	active := d.queryLog
	blocker := filepath.Join(filepath.Dir(path), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.opts.socketPath = filepath.Join(blocker, "resolver.sock")
	if err := d.Run(context.Background()); err == nil {
		t.Fatal("Run unexpectedly accepted an invalid socket directory")
	}
	if d.queryLog != nil {
		t.Fatal("Run left a dynamically enabled query log open")
	}
	if err := active.Record(context.Background(), logstore.DNSQuery{Timestamp: time.Now()}); err == nil {
		t.Fatal("Run cleanup did not close the dynamically enabled query log")
	}
}
