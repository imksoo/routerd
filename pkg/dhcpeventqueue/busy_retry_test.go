//go:build linux || freebsd

// SPDX-License-Identifier: BSD-3-Clause

package dhcpeventqueue

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// A transient SQLite lock must not discard a callback while the existing
// operation budget remains. These are private database fixtures, not live
// router callbacks. The timer controls contention; it is not a throughput gate.
func TestStoreSurvivesTransientSQLiteLocks(t *testing.T) {
	for _, begin := range []string{"BEGIN IMMEDIATE", "BEGIN EXCLUSIVE", "BEGIN"} {
		t.Run(begin, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events", "pending.db")
			locker := openQueue(t, path)
			if _, err := locker.db.Exec(begin); err != nil {
				t.Fatal(err)
			}
			// BEGIN needs a read to retain a shared lock through producer COMMIT.
			var initial int
			if err := locker.db.QueryRow("SELECT count(*) FROM pending").Scan(&initial); err != nil {
				t.Fatal(err)
			}
			released := make(chan error, 1)
			go func() {
				time.Sleep(350 * time.Millisecond)
				_, err := locker.db.Exec("ROLLBACK")
				released <- err
			}()
			at := time.Date(2026, 10, 23, 5, 55, 34, 123456789, time.UTC)
			err := Store(context.Background(), path, lease("renewed"), at)
			if releaseErr := <-released; releaseErr != nil {
				t.Fatalf("fixture unlock failed: %v", releaseErr)
			}
			if err != nil {
				t.Fatalf("callback was not committed after transient lock: %v", err)
			}
			q := openQueue(t, path)
			ok, err := q.ProcessOne(context.Background(), func(_ context.Context, e Event) error {
				if !e.ObservedAt.Equal(at) || e.Lease != lease("renewed") {
					t.Fatalf("callback changed: %+v", e)
				}
				return nil
			})
			if !ok || err != nil {
				t.Fatalf("committed callback missing: %v, %v", ok, err)
			}
			if ok, err := q.ProcessOne(context.Background(), func(context.Context, Event) error {
				t.Fatal("retry duplicated the callback")
				return nil
			}); ok || err != nil {
				t.Fatalf("queue not empty after one callback: %v, %v", ok, err)
			}
		})
	}
}

func TestStoreContentionHonorsCallerDeadline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "pending.db")
	q := openQueue(t, path)
	if _, err := q.db.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer q.db.Exec("ROLLBACK")
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := Store(ctx, path, lease("renewed"), time.Now()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller deadline was not propagated: %v", err)
	}
	var n int
	if err := q.db.QueryRow("SELECT count(*) FROM pending").Scan(&n); err != nil || n != 0 {
		t.Fatalf("cancelled callback was committed: count=%d, error=%v", n, err)
	}
}
