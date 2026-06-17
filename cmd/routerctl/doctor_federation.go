// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"strings"
	"time"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	doctorFederationWarnLag        = 60
	doctorFederationFailLag        = 180
	doctorFederationExpiresSoonSec = 120
)

func (r doctorRunner) doctorFederation() []doctorCheck {
	summaryStore, ok := r.store.(routerstate.FederationDeliverySummaryStore)
	if !ok {
		return []doctorCheck{{Area: "federation", Name: "delivery summary", Status: doctorSkip, Detail: "state backend does not support federation delivery summary"}}
	}
	fedStore, hasFedEvents := r.store.(routerstate.FederationEventStore)
	deliveryStore, hasDeliveries := r.store.(routerstate.FederationDeliveryStore)

	now := doctorNow().UTC()
	rows, err := summaryStore.ListDeliverySummary("", "", "", false, now)
	if err != nil {
		return []doctorCheck{{Area: "federation", Name: "delivery summary", Status: doctorFail, Detail: err.Error(), Remedy: "check state backend and retry"}}
	}
	if len(rows) == 0 {
		return []doctorCheck{{Area: "federation", Name: "delivery summary", Status: doctorSkip, Detail: "no federation delivery records"}}
	}

	var checks []doctorCheck
	for _, row := range rows {
		label := row.Group + "/" + row.Peer
		checks = append(checks, doctorFederationFailedCheck(label, row))
		checks = append(checks, doctorFederationPendingCheck(label, row, now, hasFedEvents, fedStore, hasDeliveries, deliveryStore))
		checks = append(checks, doctorFederationStaleTTLCheck(label, row))
		checks = append(checks, doctorFederationLagCheck(label, row))
		checks = append(checks, doctorFederationExpiresCheck(label, row))
	}
	return checks
}

func doctorFederationFailedCheck(label string, row routerstate.DeliverySummaryRow) doctorCheck {
	name := label + " failed deliveries"
	if row.Failed > 0 {
		return doctorCheck{
			Area:   "federation",
			Name:   name,
			Status: doctorFail,
			Detail: fmt.Sprintf("%d of %d delivery(s) failed", row.Failed, row.Events),
			Remedy: "inspect failed deliveries with: routerctl federation event deliveries --group " + row.Group + " --peer " + row.Peer + " --status failed",
		}
	}
	return doctorCheck{Area: "federation", Name: name, Status: doctorPass, Detail: "no failed deliveries"}
}

func doctorFederationPendingCheck(label string, row routerstate.DeliverySummaryRow, now time.Time, hasFedEvents bool, fedStore routerstate.FederationEventStore, hasDeliveries bool, deliveryStore routerstate.FederationDeliveryStore) doctorCheck {
	name := label + " pending deliveries"
	if row.Pending == 0 {
		return doctorCheck{Area: "federation", Name: name, Status: doctorPass, Detail: "no pending deliveries"}
	}
	if !hasFedEvents || !hasDeliveries {
		return doctorCheck{
			Area:   "federation",
			Name:   name,
			Status: doctorWarn,
			Detail: fmt.Sprintf("%d pending delivery(s)", row.Pending),
			Remedy: "check outbox is running and peers are reachable",
		}
	}
	deliveries, err := deliveryStore.ListDeliveriesFiltered(row.Group, "", row.Peer, routerstate.DeliveryPending)
	if err != nil {
		return doctorCheck{Area: "federation", Name: name, Status: doctorWarn, Detail: fmt.Sprintf("%d pending; detail unavailable: %v", row.Pending, err)}
	}
	events, err := fedStore.ListFederationEvents(row.Group, false, now.Unix())
	if err != nil {
		return doctorCheck{Area: "federation", Name: name, Status: doctorWarn, Detail: fmt.Sprintf("%d pending; events unavailable: %v", row.Pending, err)}
	}
	eventMap := map[string]routerstate.EventRecord{}
	for _, ev := range events {
		eventMap[ev.ID] = ev
	}
	var expiringSoon int
	for _, d := range deliveries {
		ev, ok := eventMap[d.EventID]
		if !ok {
			continue
		}
		if !ev.ExpiresAt.IsZero() && ev.ExpiresAt.Sub(now).Seconds() < doctorFederationExpiresSoonSec {
			expiringSoon++
		}
	}
	if expiringSoon > 0 {
		return doctorCheck{
			Area:   "federation",
			Name:   name,
			Status: doctorFail,
			Detail: fmt.Sprintf("%d pending; %d event(s) expire within %ds without delivery", row.Pending, expiringSoon, doctorFederationExpiresSoonSec),
			Remedy: "outbox may be stalled or peer unreachable; check eventd logs and peer endpoint",
		}
	}
	return doctorCheck{
		Area:   "federation",
		Name:   name,
		Status: doctorWarn,
		Detail: fmt.Sprintf("%d pending delivery(s)", row.Pending),
		Remedy: "check outbox is running and peers are reachable",
	}
}

func doctorFederationStaleTTLCheck(label string, row routerstate.DeliverySummaryRow) doctorCheck {
	name := label + " stale TTL"
	if row.StaleTTL > 0 {
		status := doctorWarn
		remedy := "outbox should re-push refreshed events on next tick; if this persists, check outbox interval and delivery filtering"
		if row.StaleTTL == row.Delivered {
			status = doctorFail
			remedy = "all delivered events have stale TTL; outbox re-push appears broken"
		}
		return doctorCheck{
			Area:   "federation",
			Name:   name,
			Status: status,
			Detail: fmt.Sprintf("%d of %d delivered event(s) have stale TTL (event.expiresAt > delivery.eventExpiresAt)", row.StaleTTL, row.Delivered),
			Remedy: remedy,
		}
	}
	return doctorCheck{Area: "federation", Name: name, Status: doctorPass, Detail: "no stale TTL deliveries"}
}

func doctorFederationLagCheck(label string, row routerstate.DeliverySummaryRow) doctorCheck {
	name := label + " delivery lag"
	if row.MaxLagSeconds == 0 && row.Delivered == 0 {
		return doctorCheck{Area: "federation", Name: name, Status: doctorSkip, Detail: "no delivered events to measure lag"}
	}
	detail := fmt.Sprintf("max delivery lag %ds", row.MaxLagSeconds)
	if row.MaxLagSeconds >= doctorFederationFailLag {
		return doctorCheck{
			Area:   "federation",
			Name:   name,
			Status: doctorFail,
			Detail: detail,
			Remedy: fmt.Sprintf("delivery lag exceeds %ds; check network latency to peer and outbox interval", doctorFederationFailLag),
		}
	}
	if row.MaxLagSeconds >= doctorFederationWarnLag {
		return doctorCheck{
			Area:   "federation",
			Name:   name,
			Status: doctorWarn,
			Detail: detail,
			Remedy: fmt.Sprintf("delivery lag exceeds %ds; monitor peer connectivity", doctorFederationWarnLag),
		}
	}
	return doctorCheck{Area: "federation", Name: name, Status: doctorPass, Detail: detail}
}

func doctorFederationExpiresCheck(label string, row routerstate.DeliverySummaryRow) doctorCheck {
	name := label + " event expiry"
	if row.MinExpiresInSeconds <= 0 && row.Events > 0 {
		detail := "events with no expiry"
		if row.MinExpiresInSeconds < 0 {
			detail = fmt.Sprintf("nearest event expires in %ds (already expired)", row.MinExpiresInSeconds)
		}
		return doctorCheck{Area: "federation", Name: name, Status: doctorSkip, Detail: detail}
	}
	if row.MinExpiresInSeconds == 0 {
		return doctorCheck{Area: "federation", Name: name, Status: doctorSkip, Detail: "no expiry data"}
	}
	detail := fmt.Sprintf("nearest event expires in %ds", row.MinExpiresInSeconds)
	if row.MinExpiresInSeconds < doctorFederationExpiresSoonSec {
		status := doctorWarn
		if row.Pending > 0 || row.Failed > 0 {
			status = doctorFail
		}
		return doctorCheck{
			Area:   "federation",
			Name:   name,
			Status: status,
			Detail: fmt.Sprintf("%s; %d pending %d failed", detail, row.Pending, row.Failed),
			Remedy: strings.TrimSpace(fmt.Sprintf("event TTL running low; verify outbox re-push and peer delivery")),
		}
	}
	return doctorCheck{Area: "federation", Name: name, Status: doctorPass, Detail: detail}
}
