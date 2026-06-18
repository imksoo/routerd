// SPDX-License-Identifier: BSD-3-Clause

package eventd

import (
	"context"
	"time"
)

// Pruner enforces EventGroup retention by periodically calling
// PruneFederationEvents for the daemon's group.
type Pruner struct {
	store     EventStore
	group     string
	retention Retention
	interval  time.Duration
	now       func() time.Time
	metrics   *Metrics
}

// NewPruner builds a Pruner. now may be nil to use time.Now. interval <= 0 falls
// back to DefaultPruneInterval.
func NewPruner(store EventStore, group string, retention Retention, interval time.Duration, now func() time.Time) *Pruner {
	if now == nil {
		now = time.Now
	}
	if interval <= 0 {
		interval = DefaultPruneInterval
	}
	return &Pruner{
		store:     store,
		group:     group,
		retention: retention,
		interval:  interval,
		now:       now,
	}
}

func (p *Pruner) SetMetrics(m *Metrics) { p.metrics = m }

// PruneOnce runs a single retention pass and returns rows deleted.
func (p *Pruner) PruneOnce(ctx context.Context) (int64, error) {
	pruned, err := p.store.PruneFederationEvents(p.group, p.retention.MaxAge, p.retention.MaxEvents, p.now())
	p.metrics.RecordPrunerTick(ctx, p.group)
	if err != nil {
		p.metrics.RecordPrunerTickError(ctx, p.group)
	} else if pruned > 0 {
		p.metrics.RecordPrunerPruned(ctx, p.group, pruned)
	}
	return pruned, err
}

// Run prunes once immediately, then on every interval tick until ctx is done.
// onError, when non-nil, is invoked with any prune error so the caller can log
// or publish it.
func (p *Pruner) Run(ctx context.Context, onError func(error)) {
	if _, err := p.PruneOnce(ctx); err != nil && onError != nil {
		onError(err)
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := p.PruneOnce(ctx); err != nil && onError != nil {
				onError(err)
			}
		}
	}
}
