// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventfile"
)

const eventFileQueueSize = 64

// One daemon-owned writer receives a bounded best-effort file stream. The
// memory ring and long-poll notification never wait for filesystem I/O. Queue
// overflow and failed writes are explicit loss counters, not persistence ACKs.
func (d *daemon) writeEvents(ctx context.Context) {
	for {
		var event daemonapi.DaemonEvent
		select {
		case <-ctx.Done():
			d.mu.Lock()
			d.fileDropped += uint64(len(d.fileQueue))
			d.mu.Unlock()
			return
		case next, ok := <-d.fileQueue:
			if !ok {
				return
			}
			event = next
		}
		if ctx.Err() != nil {
			d.mu.Lock()
			d.fileDropped += uint64(1 + len(d.fileQueue))
			d.mu.Unlock()
			return
		}
		err := appendObserverEvent(d.opts.eventFile, event)
		d.mu.Lock()
		if err != nil {
			d.fileErrors++
		} else {
			d.fileWritten++
		}
		d.setFileResultLocked(err)
		d.mu.Unlock()
	}
}

func (d *daemon) setFileResultLocked(err error) {
	status, reason, message := "True", "EventPersistenceReady", ""
	if err != nil {
		status, reason, message = "False", "EventPersistenceFailed", fmt.Sprintf("%.2048s", err.Error())
	}
	transition := d.now()
	if d.fileCondition.Status == status {
		transition = d.fileCondition.LastTransitionTime
	}
	d.fileCondition = daemonapi.Condition{Type: "EventPersistence", Status: status, Reason: reason, Message: message, LastTransitionTime: transition}
}

func appendObserverEvent(path string, event daemonapi.DaemonEvent) error {
	info, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("event file is not a regular file: %s", path)
	}
	return eventfile.AppendJSONLine(path, event)
}
