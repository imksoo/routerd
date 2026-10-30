// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dhcpeventqueue"
	"github.com/imksoo/routerd/pkg/logstore"
)

// The consumer lives in the existing serve process and starts only after the
// controllers have subscribed. No callback waits for it, and --once never runs it.
func runDHCPEventQueue(ctx context.Context, path, stickyPath, defaultLeaseFile string, currentRouter func() *api.Router, eventBus *bus.Bus) {
	started := time.Now().UTC()
	var q *dhcpeventqueue.Queue
	defer func() {
		if q != nil {
			_ = q.Close()
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastWarning time.Time
	for {
		if ctx.Err() != nil {
			return
		}
		var err error
		if q == nil {
			openCtx, cancel := context.WithTimeout(ctx, dhcpeventqueue.OperationTimeout)
			if _, statErr := os.Lstat(path); statErr == nil {
				q, err = dhcpeventqueue.Open(openCtx, path)
			} else if !os.IsNotExist(statErr) {
				err = statErr
			}
			cancel()
		}
		if err == nil && q != nil {
			// Bound each turn; a permanently failing head is retained, never skipped.
			for n := 0; n < 32 && ctx.Err() == nil; n++ {
				processCtx, cancel := context.WithTimeout(ctx, dhcpeventqueue.OperationTimeout)
				var processed bool
				processed, err = q.ProcessOne(processCtx, func(ctx context.Context, event dhcpeventqueue.Event) error {
					router := currentRouter()
					disposition := prepareDHCPLeaseSource(router, &event.Lease)
					if event.ObservedAt.Before(started) {
						var err error
						if disposition == "" {
							disposition, err = dhcpReplayDisposition(router, defaultLeaseFile, event.Lease, time.Now().UTC())
						}
						if err != nil {
							return err
						}
					}
					return processDHCPLeaseEventDisposition(ctx, event, stickyPath, dhcpStickyHoldDays(router, event.Lease.IP), eventBus, disposition, event.ObservedAt.Before(started))
				})
				cancel()
				if err != nil || !processed {
					break
				}
			}
		}
		if err != nil && ctx.Err() == nil && (lastWarning.IsZero() || time.Since(lastWarning) >= 30*time.Second) {
			slog.Warn("DHCP event queue processing failed; pending events retained", "error", err)
			lastWarning = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func processDHCPLeaseEventDisposition(ctx context.Context, pending dhcpeventqueue.Event, stickyPath string, holdDays int, eventBus *bus.Bus, disposition string, replayed bool) error {
	req := pending.Lease
	topic, ok := daemonapi.DHCPLeaseEventType(req.Action)
	if !ok || pending.ObservedAt.IsZero() {
		return errors.New("invalid queued DHCP event")
	}
	if eventBus == nil {
		return errors.New("DHCP event bus unavailable")
	}
	current := true
	if disposition == "" { // Record ordering metadata even when reservations are disabled (holdDays=0).
		sticky, err := logstore.OpenDHCPStickyLogContext(ctx, stickyPath)
		if err != nil {
			return err
		}
		defer sticky.Close()
		current, err = sticky.RecordLeaseEventIfCurrent(ctx, req.Action, req.MAC, req.IP, req.Hostname, holdDays, pending.ObservedAt)
		if err != nil {
			return err
		}
	}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd-dhcp-event-relay", Kind: "routerd-dhcp-event-relay"}, topic, daemonapi.SeverityInfo)
	event.Time = pending.ObservedAt
	event.Attributes = map[string]string{"mac": req.MAC, "ip": req.IP, "hostname": req.Hostname, "interface": req.Interface, "observedAt": pending.ObservedAt.UTC().Format(time.RFC3339Nano)}
	if kind, name, ok := strings.Cut(req.Source, "/"); ok {
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: kind, Name: name}
	}
	if !req.ExpiresAt.IsZero() {
		event.Attributes["expiresAt"] = req.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	if req.Infinite {
		event.Attributes["infinite"] = "true"
	}
	if replayed {
		event.Attributes["replayed"] = "true"
	}
	if !current {
		disposition = "superseded"
	}
	if disposition != "" {
		event.Attributes["replayDisposition"] = disposition
		return eventBus.RecordDurable(ctx, event)
	}
	// A crash after publication and before queue deletion can repeat delivery. It is
	// at least once; the original timestamp and FIFO head make sticky writes
	// idempotent, and no newer queued event can be overtaken by that retry.
	return eventBus.PublishDurable(ctx, event)
}
