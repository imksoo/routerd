// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/logstore"
)

func blockingObservationCommand(t *testing.T, dir, name string, holdPipe bool) string {
	t.Helper()
	pidPath := filepath.Join(dir, name+".pid")
	command := fmt.Sprintf("#!/bin/sh\nprintf '%%s' $$ > %q\nexec /bin/sleep 30\n", pidPath)
	if holdPipe {
		command = fmt.Sprintf("#!/bin/sh\n/bin/sleep 30 &\nprintf '%%s' $! > %q\nprintf '%%s' $$ > %q\nwait\n", pidPath+".child", pidPath)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(command), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, path := range []string{pidPath, pidPath + ".child"} {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			pid, err := strconv.Atoi(string(data))
			if err == nil {
				process, _ := os.FindProcess(pid)
				_ = process.Kill()
			}
		}
	})
	return pidPath
}

func waitObservationStarted(t *testing.T, pidPaths ...string) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		for _, path := range pidPaths {
			if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
				return
			}
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("observation command did not start")
		}
	}
}

func TestCommandCancellationClosesInheritedOutputPipe(t *testing.T) {
	dir := t.TempDir()
	pidPath := blockingObservationCommand(t, dir, "blocked-command", true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := commandOutputTimeout(ctx, 2*time.Second, filepath.Join(dir, "blocked-command"))
		done <- err
	}()
	waitObservationStarted(t, pidPath)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("command cancellation = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled command waited for its descendant's output pipe")
	}
}

func TestObservationCommandKeepsDataOutOfDiagnostics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status-command")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'private-key-output'\nprintf 'command failed' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := commandOutputTimeout(context.Background(), 2*time.Second, path)
	if string(out) != "private-key-output" || err == nil || strings.Contains(err.Error(), "private-key-output") || !strings.Contains(err.Error(), "command failed") {
		t.Fatalf("observation data mixed with error diagnostics: stdout=%q error=%v", out, err)
	}
}

func TestObservationRoutesCancelTheirChildCommands(t *testing.T) {
	for _, route := range []string{
		"vpn", "routes", "clients", "connections?limit=1",
		"summary?events=-1&connections=1&firewallLogs=-1&dnsQueries=-1&trafficFlows=-1&clients=0&vpn=0",
		"summary?events=-1&connections=-1&firewallLogs=-1&dnsQueries=-1&trafficFlows=-1&clients=1&vpn=0",
	} {
		t.Run(route, func(t *testing.T) {
			dir := t.TempDir()
			var pidPaths []string
			for _, name := range []string{"ip", "arp", "ndp", "wg", "tailscale", "conntrack"} {
				pidPaths = append(pidPaths, blockingObservationCommand(t, dir, name, false))
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			missing := filepath.Join(dir, "missing.db")
			h := New(Options{DHCPLeasePaths: []string{missing}, DHCPFingerprintLogPath: missing, DHCPStickyLogPath: missing, DNSQueryLogPath: missing, TrafficFlowLogPath: missing, FirewallLogPath: missing})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := httptest.NewRequest(http.MethodGet, "/api/v1/"+route, nil).WithContext(ctx)
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { h.ServeHTTP(w, r); close(done) }()
			waitObservationStarted(t, pidPaths...)
			cancel()
			select {
			case <-done:
				if !strings.Contains(w.Body.String(), context.Canceled.Error()) {
					t.Fatalf("route lost cancellation: status=%d body=%s", w.Code, w.Body.String())
				}
			case <-time.After(time.Second):
				t.Fatal("request cancellation did not stop the observation command")
			}
		})
	}
}

func TestSnapshotPassesOwnerContextToVPNObserver(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	h := New(Options{VPNStatus: func(observationCtx context.Context) (VPNStatus, error) {
		if observationCtx != ctx {
			return VPNStatus{}, errors.New("owner context was replaced")
		}
		close(started)
		<-observationCtx.Done()
		return VPNStatus{}, observationCtx.Err()
	}})
	done := make(chan Snapshot, 1)
	go func() {
		done <- h.Snapshot(ctx, SnapshotOptions{EventLimit: -1, ConnectionsLimit: -1, FirewallLimit: -1, DNSQueryLimit: -1, TrafficFlowLimit: -1, SkipDHCPLeases: true, SkipResources: true, IncludeVPN: true})
	}()
	defer cancel()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("VPN observer did not receive the snapshot owner context")
	}
	cancel()
	select {
	case snapshot := <-done:
		if len(snapshot.Errors) != 1 || snapshot.Errors[0] != context.Canceled.Error() {
			t.Fatalf("snapshot lost owner cancellation: %v", snapshot.Errors)
		}
	case <-time.After(time.Second):
		t.Fatal("snapshot did not stop its VPN observer")
	}
}

func TestLeaseLogQueriesUseOwnerDeadline(t *testing.T) {
	for _, kind := range []string{"fingerprint", "sticky"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "observations.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			// Sorting this finite view makes the real SELECT remain active past
			// the owner deadline without a test hook or a locked production store.
			view := `CREATE VIEW dhcp_fingerprint AS WITH RECURSIVE seq(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM seq WHERE n<2000000)
SELECT 'fixture' AS mac, '' AS hostname, '' AS vendor_class, '' AS requested_options, '' AS os_family, '' AS device_class, '' AS device_name, 0 AS confidence, '' AS signal, n AS observed_at, '' AS source FROM seq`
			if kind == "sticky" {
				view = `CREATE VIEW dhcp_sticky_leases AS WITH RECURSIVE seq(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM seq WHERE n<2000000)
SELECT 'fixture' AS mac, '192.0.2.1' AS ip, '' AS hostname, 'ipv4' AS family, 0 AS allocated_at, 0 AS released_at, n AS last_request_at, n AS sticky_until FROM seq`
			}
			if _, err := db.Exec(view); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			h := New(Options{DHCPFingerprintLogPath: path, DHCPStickyLogPath: path})
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if kind == "fingerprint" {
				_, err = h.dhcpFingerprintList(ctx, logstore.DHCPFingerprintFilter{Limit: 1})
			} else {
				_, err = h.dhcpStickyLeaseList(ctx, logstore.DHCPStickyFilter{Limit: 1})
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("lease log query ignored owner deadline: %v", err)
			}
		})
	}
}
