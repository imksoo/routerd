// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/raobserver"
)

func TestObserverFileQueueIsBoundedAndRealWriterDrains(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	d := newDaemon(options{eventFile: path})
	for i := 0; i <= eventFileQueueSize; i++ {
		d.recordAdvertisement(raobserver.Advertisement{SourceMAC: fmt.Sprintf("02:00:00:00:00:%02x", i)})
	}
	if len(d.fileQueue) != eventFileQueueSize || d.fileDropped != 1 || len(d.events) != eventFileQueueSize+1 {
		t.Fatalf("queue/ring lost its bound: queued=%d dropped=%d ring=%d", len(d.fileQueue), d.fileDropped, len(d.events))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("receive path performed disk I/O: %v", err)
	}
	if d.status().Observed["eventFileError"] == "" {
		t.Fatal("queue loss was not visible")
	}
	observed, written := make(chan struct{}), make(chan struct{})
	close(observed)
	go func() { defer close(written); d.writeEvents(t.Context()) }()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := d.finishWorkers(ctx, observed, written); err == nil || !strings.Contains(err.Error(), "lost 1 events") {
		t.Fatalf("shutdown concealed a lost event: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		var event daemonapi.DaemonEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil || event.Reason != "RogueRADetected" {
			t.Fatalf("real JSONL event is invalid: %s %v", scanner.Bytes(), err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != eventFileQueueSize || d.fileWritten != eventFileQueueSize || d.fileErrors != 0 {
		t.Fatalf("joined writer failed to drain: lines=%d written=%d errors=%d", count, d.fileWritten, d.fileErrors)
	}
}

func TestObserverFileFailureIsVisibleAndFailsShutdown(t *testing.T) {
	d := newDaemon(options{eventFile: t.TempDir()}) // a directory cannot be a JSONL file
	d.publishLocked("detected", "warning", "Detected", "", nil)
	observed, written := make(chan struct{}), make(chan struct{})
	close(observed)
	go func() { defer close(written); d.writeEvents(t.Context()) }()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := d.finishWorkers(ctx, observed, written); err == nil {
		t.Fatal("failed persistence was acknowledged as successful shutdown")
	}
	status := d.status()
	if status.Health != daemonapi.HealthDegraded || status.Observed["eventFileErrors"] != "1" || status.Observed["eventFileError"] == "" {
		t.Fatalf("file failure disappeared from status: %+v", status)
	}
	if condition := status.Resources[0].Conditions[1]; condition.Type != "EventPersistence" || condition.Status != "False" {
		t.Fatalf("file condition failed to expose failure: %+v", condition)
	}
}

func TestObserverFileConditionRetainsOnlyActualTransitionTime(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	d := newDaemon(options{})
	d.clock = func() time.Time { return now }
	d.setFileResultLocked(fmt.Errorf("first"))
	failedAt := d.fileCondition.LastTransitionTime
	now = now.Add(time.Second)
	d.setFileResultLocked(fmt.Errorf("second"))
	if d.fileCondition.LastTransitionTime != failedAt {
		t.Fatal("same failure status moved its transition time")
	}
	now = now.Add(time.Second)
	d.setFileResultLocked(nil)
	if d.fileCondition.LastTransitionTime != now || d.fileCondition.Status != "True" {
		t.Fatal("recovery did not record its actual transition")
	}
}

func TestObserverEmptyWriterCancellationAndMissedJoins(t *testing.T) {
	d := newDaemon(options{})
	ctx, cancel := context.WithCancel(t.Context())
	written := make(chan struct{})
	go func() { defer close(written); d.writeEvents(ctx) }()
	cancel()
	select {
	case <-written:
	case <-time.After(time.Second):
		t.Fatal("empty writer ignored cancellation")
	}
	for _, pending := range []string{"capture", "writer"} {
		t.Run(pending, func(t *testing.T) {
			d := newDaemon(options{})
			observed, written := make(chan struct{}), make(chan struct{})
			if pending == "writer" {
				close(observed)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			if err := d.finishWorkers(ctx, observed, written); err == nil || !strings.Contains(err.Error(), pending+" did not join") {
				t.Fatalf("unjoined worker was acknowledged: %v", err)
			}
			if pending == "capture" {
				// A live producer must retain its queue, rather than panic on
				// publication because shutdown closed it before joining.
				d.fileQueue <- daemonapi.DaemonEvent{}
			}
		})
	}
}

func TestObserverCanceledStartupCreatesNoWorkersOrSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "observer.sock")
	d := newDaemon(options{socketPath: path})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := d.run(ctx); err != context.Canceled {
		t.Fatalf("canceled start accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("canceled start changed filesystem: %v", err)
	}
}
