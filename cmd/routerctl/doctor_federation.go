// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"sort"
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

	var checks []doctorCheck
	if len(rows) == 0 {
		checks = append(checks, doctorCheck{Area: "federation", Name: "delivery summary", Status: doctorSkip, Detail: "no federation delivery records"})
	} else {
		for _, row := range rows {
			label := row.Group + "/" + row.Peer
			checks = append(checks, doctorFederationFailedCheck(label, row))
			checks = append(checks, doctorFederationPendingCheck(label, row, now, hasFedEvents, fedStore, hasDeliveries, deliveryStore))
			checks = append(checks, doctorFederationStaleTTLCheck(label, row))
			checks = append(checks, doctorFederationLagCheck(label, row))
			checks = append(checks, doctorFederationExpiresCheck(label, row))
		}
	}

	checks = append(checks, r.doctorFederationExpectedPeerChecks(now, hasFedEvents, fedStore, hasDeliveries, deliveryStore)...)
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

// doctorFederationExpectedPeerChecks compares the expected peer set (derived from
// EventPeer resources in the startup config) against actual delivery rows in the
// state store. A peer that should receive active events but has no delivery row
// is a FAIL — the outbox never enqueued delivery for that peer.
func (r doctorRunner) doctorFederationExpectedPeerChecks(now time.Time, hasFedEvents bool, fedStore routerstate.FederationEventStore, hasDeliveries bool, deliveryStore routerstate.FederationDeliveryStore) []doctorCheck {
	if r.router == nil {
		return []doctorCheck{{Area: "federation", Name: "expected peers", Status: doctorSkip, Detail: "startup config unavailable"}}
	}
	if !hasFedEvents || !hasDeliveries {
		return []doctorCheck{{Area: "federation", Name: "expected peers", Status: doctorSkip, Detail: "federation store unavailable"}}
	}

	type expectedPeer struct {
		nodeName string
		endpoint string
	}
	groupPeers := map[string][]expectedPeer{}
	groupSelf := map[string]string{}

	for _, res := range r.router.Spec.Resources {
		if res.Kind == "EventGroup" {
			spec, err := res.EventGroupSpec()
			if err != nil {
				continue
			}
			groupSelf[res.Metadata.Name] = strings.TrimSpace(spec.NodeName)
		}
	}

	for _, res := range r.router.Spec.Resources {
		if res.Kind != "EventPeer" {
			continue
		}
		spec, err := res.EventPeerSpec()
		if err != nil {
			continue
		}
		groupRef := strings.TrimSpace(spec.GroupRef)
		nodeName := strings.TrimSpace(spec.NodeName)
		if groupRef == "" || nodeName == "" {
			continue
		}
		self := groupSelf[groupRef]
		if nodeName == self {
			continue
		}
		groupPeers[groupRef] = append(groupPeers[groupRef], expectedPeer{
			nodeName: nodeName,
			endpoint: strings.TrimSpace(spec.Endpoint),
		})
	}

	if len(groupPeers) == 0 {
		return []doctorCheck{{Area: "federation", Name: "expected peers", Status: doctorSkip, Detail: "no EventPeer resources configured"}}
	}

	var checks []doctorCheck
	for group, peers := range groupPeers {
		events, err := fedStore.ListFederationEvents(group, false, now.Unix())
		if err != nil {
			checks = append(checks, doctorCheck{
				Area: "federation", Name: group + " expected peers",
				Status: doctorWarn, Detail: "events unavailable: " + err.Error(),
			})
			continue
		}
		selfEmitted := filterSelfEmittedEvents(events, groupSelf[group])
		if len(selfEmitted) == 0 {
			checks = append(checks, doctorCheck{
				Area: "federation", Name: group + " expected peers",
				Status: doctorSkip, Detail: "no self-emitted active events to deliver",
			})
			continue
		}

		for _, peer := range peers {
			label := group + "/" + peer.nodeName + " expected delivery"

			if peer.endpoint == "" {
				checks = append(checks, doctorCheck{
					Area:   "federation",
					Name:   label,
					Status: doctorFail,
					Detail: "EventPeer endpoint is empty",
					Remedy: "set spec.endpoint on EventPeer/" + peer.nodeName + " for group " + group,
				})
				continue
			}

			deliveries, err := deliveryStore.ListDeliveriesFiltered(group, "", peer.nodeName, "")
			if err != nil {
				checks = append(checks, doctorCheck{
					Area: "federation", Name: label,
					Status: doctorWarn, Detail: "delivery lookup failed: " + err.Error(),
				})
				continue
			}
			deliveredEvents := map[string]bool{}
			for _, d := range deliveries {
				deliveredEvents[d.EventID] = true
			}

			var missing []string
			for _, ev := range selfEmitted {
				if !deliveredEvents[ev.ID] {
					missing = append(missing, ev.ID)
				}
			}
			if len(missing) > 0 {
				sort.Strings(missing)
				detail := fmt.Sprintf("%d of %d active event(s) have no delivery row", len(missing), len(selfEmitted))
				if len(missing) <= 3 {
					detail += ": " + strings.Join(missing, ", ")
				} else {
					detail += ": " + strings.Join(missing[:3], ", ") + fmt.Sprintf(" +%d more", len(missing)-3)
				}
				checks = append(checks, doctorCheck{
					Area:   "federation",
					Name:   label,
					Status: doctorFail,
					Detail: detail,
					Remedy: "outbox never enqueued delivery for this peer; check EventPeer config and outbox peer filter",
				})
			} else {
				checks = append(checks, doctorCheck{
					Area:   "federation",
					Name:   label,
					Status: doctorPass,
					Detail: fmt.Sprintf("all %d active event(s) have delivery rows", len(selfEmitted)),
				})
			}
		}
	}
	return checks
}

func (r doctorRunner) buildFederationSummary(checks []doctorCheck) *doctorFederationSummary {
	fs := &doctorFederationSummary{}
	for _, c := range checks {
		if c.Area != "federation" {
			continue
		}
		switch c.Status {
		case doctorPass:
			fs.SeverityCounts.Pass++
		case doctorWarn:
			fs.SeverityCounts.Warn++
		case doctorFail:
			fs.SeverityCounts.Fail++
		case doctorSkip:
			fs.SeverityCounts.Skip++
		}
		if strings.Contains(c.Name, "expected delivery") && c.Status == doctorFail {
			fs.MissingExpectedPeerCount++
		}
	}

	summaryStore, ok := r.store.(routerstate.FederationDeliverySummaryStore)
	if !ok {
		return fs
	}
	rows, err := summaryStore.ListDeliverySummary("", "", "", false, doctorNow().UTC())
	if err != nil {
		return fs
	}
	for _, row := range rows {
		fs.TotalEvents += row.Events
		fs.TotalDelivered += row.Delivered
		fs.FailedDeliveryCount += row.Failed
		fs.StaleTTLCount += row.StaleTTL
		fs.PendingDeliveryCount += row.Pending
		if row.MaxLagSeconds > fs.MaxDeliveryLagSeconds {
			fs.MaxDeliveryLagSeconds = row.MaxLagSeconds
		}
		if row.MinExpiresInSeconds != 0 && (fs.MinExpiresInSeconds == 0 || row.MinExpiresInSeconds < fs.MinExpiresInSeconds) {
			fs.MinExpiresInSeconds = row.MinExpiresInSeconds
		}
	}
	return fs
}

func filterSelfEmittedEvents(events []routerstate.EventRecord, selfNode string) []routerstate.EventRecord {
	if selfNode == "" {
		return events
	}
	var result []routerstate.EventRecord
	for _, ev := range events {
		if strings.TrimSpace(ev.SourceNode) == selfNode {
			result = append(result, ev)
		}
	}
	return result
}
