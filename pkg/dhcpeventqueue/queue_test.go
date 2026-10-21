//go:build linux || freebsd

// SPDX-License-Identifier: BSD-3-Clause
//
package dhcpeventqueue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/controlapi"
)

func lease(action string) controlapi.DHCPLeaseEventRequest {
	return controlapi.DHCPLeaseEventRequest{Action: action, MAC: "02:00:00:00:00:42", IP: "192.0.2.42", Hostname: "fixture", Interface: "test0"}
}
func openQueue(t *testing.T, path string) *Queue {
	t.Helper()
	q, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { q.Close() })
	return q
}

func TestCommittedCallbacksSurviveReopenInOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "pending.db")
	at := time.Date(2026, 10, 21, 1, 2, 3, 456, time.UTC)
	for i, a := range []string{"added", "removed", "renewed"} {
		if err := Store(context.Background(), path, lease(a), at.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	q := openQueue(t, path)
	for i, want := range []string{"added", "removed", "renewed"} {
		ok, err := q.ProcessOne(context.Background(), func(_ context.Context, e Event) error {
			if e.Lease.Action != want || !e.ObservedAt.Equal(at.Add(time.Duration(i)*time.Hour)) {
				t.Fatalf("event=%+v", e)
			}
			return nil
		})
		if !ok || err != nil {
			t.Fatalf("processed=%v error=%v", ok, err)
		}
	}
	if ok, err := q.ProcessOne(context.Background(), func(context.Context, Event) error { t.Fatal("unexpected duplicate"); return nil }); ok || err != nil {
		t.Fatalf("empty=%v %v", ok, err)
	}
}

func TestFailedHeadAndConsumerLockDoNotBlockProducers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "pending.db")
	q := openQueue(t, path)
	q2 := openQueue(t, path)
	if err := q.Enqueue(context.Background(), lease("removed"), time.Now()); err != nil {
		t.Fatal(err)
	}
	failed := errors.New("simulated journal failure")
	if ok, err := q.ProcessOne(context.Background(), func(ctx context.Context, e Event) error {
		if e.Lease.Action != "removed" {
			t.Fatal(e)
		}
		if ok, err := q2.ProcessOne(ctx, func(context.Context, Event) error { t.Fatal("two consumers"); return nil }); ok || err == nil {
			t.Fatal("consumer lock not enforced")
		}
		if err := q2.Enqueue(ctx, lease("renewed"), time.Now()); err != nil {
			t.Fatalf("producer blocked by consumer: %v", err)
		}
		return failed
	}); ok || !errors.Is(err, failed) {
		t.Fatalf("%v %v", ok, err)
	}
	for _, want := range []string{"removed", "renewed"} {
		if ok, err := q.ProcessOne(context.Background(), func(_ context.Context, e Event) error {
			if e.Lease.Action != want {
				t.Fatalf("overtaken event=%+v", e)
			}
			return nil
		}); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
}

func TestCrashBeforeDeleteReplaysHeadBeforeNewerEvent(t *testing.T) {
	if path := os.Getenv("ROUTERD_TEST_QUEUE_CRASH"); path != "" {
		q, err := Open(context.Background(), path)
		if err != nil {
			os.Exit(90)
		}
		q.ProcessOne(context.Background(), func(_ context.Context, e Event) error {
			if e.Lease.Action != "removed" {
				os.Exit(91)
			}
			os.Exit(23)
			return nil
		})
		os.Exit(92)
	}
	path := filepath.Join(t.TempDir(), "events", "pending.db")
	for _, a := range []string{"removed", "renewed"} {
		if err := Store(context.Background(), path, lease(a), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashBeforeDeleteReplaysHeadBeforeNewerEvent$")
	cmd.Env = append(os.Environ(), "ROUTERD_TEST_QUEUE_CRASH="+path)
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("child did not crash at processing boundary: %v", err)
	}
	q := openQueue(t, path)
	for _, want := range []string{"removed", "renewed"} {
		if ok, err := q.ProcessOne(context.Background(), func(_ context.Context, e Event) error {
			if e.Lease.Action != want {
				t.Fatal(e)
			}
			return nil
		}); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
}

func TestCapacityAndSizeFailuresAreExplicit(t *testing.T) {
	q := openQueue(t, filepath.Join(t.TempDir(), "events", "pending.db"))
	// Populate the bounded fixture in one transaction, then exercise the real producer.
	_, err := q.db.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<4096) INSERT INTO pending(observed_at,payload) SELECT 1,'{}' FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(context.Background(), lease("renewed"), time.Now()); !errors.Is(err, ErrFull) {
		t.Fatalf("full error=%v", err)
	}
	if _, err := q.db.Exec("DELETE FROM pending"); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"mac", "ip", "hostname", "interface"} {
		r := lease("added")
		switch field {
		case "mac":
			r.MAC = strings.Repeat("m", 65)
		case "ip":
			r.IP = strings.Repeat("i", 65)
		case "hostname":
			r.Hostname = strings.Repeat("h", 254)
		case "interface":
			r.Interface = strings.Repeat("i", 65)
		}
		if err := q.Enqueue(context.Background(), r, time.Now()); err == nil {
			t.Fatal("accepted oversized", field)
		}
	}
	var n int
	if err := q.db.QueryRow("SELECT count(*) FROM pending").Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestSQLiteLockAndCancelledContextCannotReportSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "pending.db")
	q := openQueue(t, path)
	if _, err := q.db.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer q.db.Exec("ROLLBACK")
	start := time.Now()
	err := Store(context.Background(), path, lease("renewed"), time.Now())
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("lock wait not bounded: %s, %v", time.Since(start), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Store(ctx, path, lease("renewed"), time.Now()); err == nil {
		t.Fatal("cancelled save succeeded")
	}
}

func TestSQLiteFullDoesNotAcknowledgeLostRow(t *testing.T) {
	q := openQueue(t, filepath.Join(t.TempDir(), "events", "pending.db"))
	if _, err := q.db.Exec("PRAGMA max_page_count=4"); err != nil {
		t.Fatal(err)
	}
	n := 0
	r := lease("added")
	r.Hostname = strings.Repeat("x", 253)
	for ; n < 100; n++ {
		if err := q.Enqueue(context.Background(), r, time.Now()); err != nil {
			break
		}
	}
	if n == 100 {
		t.Fatal("fixture did not fill database")
	}
	var stored int
	if err := q.db.QueryRow("SELECT count(*) FROM pending").Scan(&stored); err != nil || stored != n {
		t.Fatal(stored, n, err)
	}
}

func TestUnsafePathsAndPermissionsAreRejected(t *testing.T) {
	for _, kind := range []string{"directory-mode", "file-mode", "symlink", "hardlink", "sidecar-symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "events")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "pending.db")
			target := filepath.Join(t.TempDir(), "target")
			if err := os.WriteFile(target, []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "directory-mode":
				os.Chmod(dir, 0755)
			case "file-mode":
				os.WriteFile(path, nil, 0644)
			case "symlink":
				os.Symlink(target, path)
			case "hardlink":
				os.Link(target, path)
			case "sidecar-symlink":
				os.Symlink(target, path+"-journal")
			}
			q, err := Open(context.Background(), path)
			if q != nil {
				q.Close()
			}
			if err == nil {
				t.Fatalf("accepted unsafe %s", kind)
			}
			b, _ := os.ReadFile(target)
			if string(b) != "preserve" {
				t.Fatal("modified symlink target")
			}
		})
	}
}

func TestConcurrentAcceptedEventsHaveUniqueOrderedSequences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "pending.db")
	q := openQueue(t, path)
	results := make(chan error, 24)
	for i := 0; i < 24; i++ {
		go func(i int) {
			r := lease("renewed")
			r.Hostname = fmt.Sprint(i)
			results <- Store(context.Background(), path, r, time.Now())
		}(i)
	}
	accepted := 0
	for i := 0; i < 24; i++ {
		if err := <-results; err == nil {
			accepted++
		}
	}
	seen := map[string]bool{}
	var last int64
	for {
		ok, err := q.ProcessOne(context.Background(), func(_ context.Context, e Event) error {
			if e.Sequence <= last || seen[e.Lease.Hostname] {
				t.Fatal(e)
			}
			last = e.Sequence
			seen[e.Lease.Hostname] = true
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
	}
	if accepted == 0 || len(seen) != accepted {
		t.Fatalf("accepted=%d stored=%d", accepted, len(seen))
	}
}
