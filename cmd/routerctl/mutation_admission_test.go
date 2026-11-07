// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMutationOutputFormatRejectedBeforeEffects(t *testing.T) {
	t.Run("federation event", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "events.db")
		err := federationEventEmitCommand([]string{"--group", "test", "--type", "routerd.test.observed", "--state-file", path, "-o", "invalid"}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "unsupported output") {
			t.Fatalf("format admission: %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("invalid output created event store: %v", err)
		}
	})
	t.Run("enrollment submit", func(t *testing.T) {
		err := mobilityEnrollmentSubmitCommand([]string{"--claim", "leaf", "--config", filepath.Join(t.TempDir(), "absent.yaml"), "-o", "invalid"}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "unsupported output") {
			t.Fatalf("format not checked before reading/submitting candidate: %v", err)
		}
	})
	t.Run("enrollment revoke", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			http.Error(w, "unexpected mutation", http.StatusInternalServerError)
		}))
		defer server.Close()
		err := mobilityEnrollmentRevokeCommand([]string{"--claim", "leaf", "--rr-url", server.URL, "-o", "invalid"}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "unsupported output") || requests.Load() != 0 {
			t.Fatalf("invalid format reached mutation endpoint: requests=%d err=%v", requests.Load(), err)
		}
	})
}

func TestPruneEventsRejectsOverflowBeforeOpeningState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.db")
	err := ledgerPruneEventsCommand([]string{"--older-than", "213504d", "--state-file", path}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "invalid --older-than") {
		t.Fatalf("overflowing destructive cutoff accepted: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid cutoff created state: %v", err)
	}
}
