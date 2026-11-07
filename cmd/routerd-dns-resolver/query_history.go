// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/imksoo/routerd/pkg/logstore"
)

const (
	queryHistoryCapacity     = 256
	queryHistoryWriteTimeout = 250 * time.Millisecond
	queryHistoryOpenTimeout  = time.Second
)

type queuedQuery struct {
	query      logstore.DNSQuery
	generation uint64
}

// History is best-effort observability: the fixed queue drops on saturation,
// reconfiguration or shutdown rather than delaying a DNS response. Counters
// belong to this daemon lifetime and remain visible across log replacement.
type queryHistory struct {
	operation   sync.Mutex // disk work and reload publication; never held by enqueue
	queue       chan queuedQuery
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	generation  uint64 // protected together with queryLog by queryLogMu
	dropped     atomic.Uint64
	writeErrors atomic.Uint64
}

func (d *daemon) startQueryHistory(parent context.Context) {
	d.queryLogMu.Lock()
	if d.queryHistory.cancel != nil {
		d.queryLogMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	queue := make(chan queuedQuery, queryHistoryCapacity)
	done := make(chan struct{})
	d.queryHistory.queue, d.queryHistory.ctx = queue, ctx
	d.queryHistory.cancel, d.queryHistory.done = cancel, done
	d.queryLogMu.Unlock()
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				for {
					select {
					case <-queue:
						d.queryHistory.dropped.Add(1)
					default:
						return
					}
				}
			case entry := <-queue:
				d.writeQueryHistory(ctx, entry)
			}
		}
	}()
}

func (d *daemon) enqueueQueryHistory(query logstore.DNSQuery) {
	d.queryLogMu.Lock()
	queue, ctx := d.queryHistory.queue, d.queryHistory.ctx
	entry := queuedQuery{query: query, generation: d.queryHistory.generation}
	enabled := d.queryLog != nil
	d.queryLogMu.Unlock()
	if !enabled || queue == nil {
		return
	}
	if ctx.Err() != nil {
		d.queryHistory.dropped.Add(1)
		return
	}
	select {
	case queue <- entry:
	default:
		d.queryHistory.dropped.Add(1)
	}
}

func (d *daemon) writeQueryHistory(ctx context.Context, entry queuedQuery) {
	d.queryHistory.operation.Lock()
	defer d.queryHistory.operation.Unlock()
	d.queryLogMu.Lock()
	current := d.queryLog
	currentGeneration := d.queryHistory.generation
	d.queryLogMu.Unlock()
	if current == nil || currentGeneration != entry.generation || ctx.Err() != nil {
		d.queryHistory.dropped.Add(1)
		return
	}
	writeCtx, cancelWrite := context.WithTimeout(ctx, queryHistoryWriteTimeout)
	err := current.Record(writeCtx, entry.query)
	cancelWrite()
	if err == nil {
		return
	}
	d.queryHistory.writeErrors.Add(1)
	if ctx.Err() != nil {
		d.queryHistory.dropped.Add(1)
		return
	}
	// Reopen only the still-current generation. A queued query may never revive
	// a log path that a successful reload replaced or disabled.
	openCtx, cancelOpen := context.WithTimeout(ctx, queryHistoryOpenTimeout)
	reopened, err := logstore.OpenDNSQueryLogContext(openCtx, current.Stats().Path)
	cancelOpen()
	if err != nil {
		d.queryHistory.dropped.Add(1)
		return
	}
	d.queryLogMu.Lock()
	active := d.queryLog == current && d.queryHistory.generation == entry.generation
	if active {
		d.queryLog = reopened
	}
	d.queryLogMu.Unlock()
	if !active {
		_ = reopened.Close()
		d.queryHistory.dropped.Add(1)
		return
	}
	_ = current.Close()
	writeCtx, cancelWrite = context.WithTimeout(ctx, queryHistoryWriteTimeout)
	err = reopened.Record(writeCtx, entry.query)
	cancelWrite()
	if err != nil {
		d.queryHistory.writeErrors.Add(1)
		d.queryHistory.dropped.Add(1)
	}
}

func (d *daemon) closeQueryLog() {
	d.queryLogMu.Lock()
	cancel, done := d.queryHistory.cancel, d.queryHistory.done
	d.queryLogMu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	d.queryLogMu.Lock()
	current := d.queryLog
	d.queryLog = nil
	d.queryHistory.cancel, d.queryHistory.done = nil, nil
	d.queryHistory.queue, d.queryHistory.ctx = nil, nil
	d.queryHistory.generation++
	d.queryLogMu.Unlock()
	if current != nil {
		_ = current.Close()
	}
}
