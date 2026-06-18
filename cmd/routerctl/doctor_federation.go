// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	defaultFederationWarnLag        = 60
	defaultFederationFailLag        = 180
	defaultFederationExpiresSoonSec = 120
)

type federationSLOThresholds struct {
	LagWarnSeconds     int
	LagFailSeconds     int
	ExpiresSoonSeconds int
	MaxPendingRuns     int
	MaxFailedRuns      int
}

func defaultSLOThresholds() federationSLOThresholds {
	return federationSLOThresholds{
		LagWarnSeconds:     defaultFederationWarnLag,
		LagFailSeconds:     defaultFederationFailLag,
		ExpiresSoonSeconds: defaultFederationExpiresSoonSec,
	}
}

func loadSLOThresholds(router *api.Router, group string) federationSLOThresholds {
	t := defaultSLOThresholds()
	if router == nil {
		return t
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "FederationSLO" {
			continue
		}
		spec, err := res.FederationSLOSpec()
		if err != nil {
			continue
		}
		if strings.TrimSpace(spec.GroupRef) != strings.TrimSpace(group) {
			continue
		}
		if spec.Delivery.LagWarnSeconds > 0 {
			t.LagWarnSeconds = spec.Delivery.LagWarnSeconds
		}
		if spec.Delivery.LagFailSeconds > 0 {
			t.LagFailSeconds = spec.Delivery.LagFailSeconds
		}
		if spec.Delivery.ExpiresSoonSeconds > 0 {
			t.ExpiresSoonSeconds = spec.Delivery.ExpiresSoonSeconds
		}
		if spec.Subscription.MaxPendingRuns > 0 {
			t.MaxPendingRuns = spec.Subscription.MaxPendingRuns
		}
		if spec.Subscription.MaxFailedRuns > 0 {
			t.MaxFailedRuns = spec.Subscription.MaxFailedRuns
		}
		break
	}
	return t
}

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
			slo := loadSLOThresholds(r.router, row.Group)
			checks = append(checks, doctorFederationFailedCheck(label, row))
			checks = append(checks, doctorFederationPendingCheck(label, row, now, slo, hasFedEvents, fedStore, hasDeliveries, deliveryStore))
			checks = append(checks, doctorFederationStaleTTLCheck(label, row))
			checks = append(checks, doctorFederationLagCheck(label, row, slo))
			checks = append(checks, doctorFederationExpiresCheck(label, row, slo))
		}
	}

	checks = append(checks, r.doctorFederationExpectedPeerChecks(now, hasFedEvents, fedStore, hasDeliveries, deliveryStore)...)
	checks = append(checks, r.doctorFederationSubscriptionChecks()...)
	return checks
}

func doctorFederationFailedCheck(label string, row routerstate.DeliverySummaryRow) doctorCheck {
	name := label + " failed deliveries"
	if row.Failed > 0 {
		return doctorCheck{
			Area:   "federation",
			Code:   checkCodeFailedDeliveries,
			Name:   name,
			Status: doctorFail,
			Detail: fmt.Sprintf("%d of %d delivery(s) failed", row.Failed, row.Events),
			Remedy: "inspect failed deliveries with: routerctl federation event deliveries --group " + row.Group + " --peer " + row.Peer + " --status failed",
		}
	}
	return doctorCheck{Area: "federation", Code: checkCodeFailedDeliveries, Name: name, Status: doctorPass, Detail: "no failed deliveries"}
}

func doctorFederationPendingCheck(label string, row routerstate.DeliverySummaryRow, now time.Time, slo federationSLOThresholds, hasFedEvents bool, fedStore routerstate.FederationEventStore, hasDeliveries bool, deliveryStore routerstate.FederationDeliveryStore) doctorCheck {
	name := label + " pending deliveries"
	if row.Pending == 0 {
		return doctorCheck{Area: "federation", Code: checkCodePendingDeliveries, Name: name, Status: doctorPass, Detail: "no pending deliveries"}
	}
	if !hasFedEvents || !hasDeliveries {
		return doctorCheck{
			Area:   "federation",
			Code:   checkCodePendingDeliveries,
			Name:   name,
			Status: doctorWarn,
			Detail: fmt.Sprintf("%d pending delivery(s)", row.Pending),
			Remedy: "check outbox is running and peers are reachable",
		}
	}
	deliveries, err := deliveryStore.ListDeliveriesFiltered(row.Group, "", row.Peer, routerstate.DeliveryPending)
	if err != nil {
		return doctorCheck{Area: "federation", Code: checkCodePendingDeliveries, Name: name, Status: doctorWarn, Detail: fmt.Sprintf("%d pending; detail unavailable: %v", row.Pending, err)}
	}
	events, err := fedStore.ListFederationEvents(row.Group, false, now.Unix())
	if err != nil {
		return doctorCheck{Area: "federation", Code: checkCodePendingDeliveries, Name: name, Status: doctorWarn, Detail: fmt.Sprintf("%d pending; events unavailable: %v", row.Pending, err)}
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
		if !ev.ExpiresAt.IsZero() && ev.ExpiresAt.Sub(now).Seconds() < float64(slo.ExpiresSoonSeconds) {
			expiringSoon++
		}
	}
	if expiringSoon > 0 {
		return doctorCheck{
			Area:   "federation",
			Code:   checkCodePendingDeliveries,
			Name:   name,
			Status: doctorFail,
			Detail: fmt.Sprintf("%d pending; %d event(s) expire within %ds without delivery", row.Pending, expiringSoon, slo.ExpiresSoonSeconds),
			Remedy: "outbox may be stalled or peer unreachable; check eventd logs and peer endpoint",
		}
	}
	return doctorCheck{
		Area:   "federation",
		Code:   checkCodePendingDeliveries,
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
			Code:   checkCodeStaleTTL,
			Name:   name,
			Status: status,
			Detail: fmt.Sprintf("%d of %d delivered event(s) have stale TTL (event.expiresAt > delivery.eventExpiresAt)", row.StaleTTL, row.Delivered),
			Remedy: remedy,
		}
	}
	return doctorCheck{Area: "federation", Code: checkCodeStaleTTL, Name: name, Status: doctorPass, Detail: "no stale TTL deliveries"}
}

func doctorFederationLagCheck(label string, row routerstate.DeliverySummaryRow, slo federationSLOThresholds) doctorCheck {
	name := label + " delivery lag"
	if row.MaxLagSeconds == 0 && row.Delivered == 0 {
		return doctorCheck{Area: "federation", Code: checkCodeDeliveryLag, Name: name, Status: doctorSkip, Detail: "no delivered events to measure lag"}
	}
	detail := fmt.Sprintf("max delivery lag %ds", row.MaxLagSeconds)
	if row.MaxLagSeconds >= int64(slo.LagFailSeconds) {
		return doctorCheck{
			Area:   "federation",
			Code:   checkCodeDeliveryLag,
			Name:   name,
			Status: doctorFail,
			Detail: detail,
			Remedy: fmt.Sprintf("delivery lag exceeds SLO fail threshold %ds; check network latency to peer and outbox interval", slo.LagFailSeconds),
		}
	}
	if row.MaxLagSeconds >= int64(slo.LagWarnSeconds) {
		return doctorCheck{
			Area:   "federation",
			Code:   checkCodeDeliveryLag,
			Name:   name,
			Status: doctorWarn,
			Detail: detail,
			Remedy: fmt.Sprintf("delivery lag exceeds SLO warn threshold %ds; monitor peer connectivity", slo.LagWarnSeconds),
		}
	}
	return doctorCheck{Area: "federation", Code: checkCodeDeliveryLag, Name: name, Status: doctorPass, Detail: detail}
}

func doctorFederationExpiresCheck(label string, row routerstate.DeliverySummaryRow, slo federationSLOThresholds) doctorCheck {
	name := label + " event expiry"
	if row.MinExpiresInSeconds <= 0 && row.Events > 0 {
		detail := "events with no expiry"
		if row.MinExpiresInSeconds < 0 {
			detail = fmt.Sprintf("nearest event expires in %ds (already expired)", row.MinExpiresInSeconds)
		}
		return doctorCheck{Area: "federation", Code: checkCodeEventExpiry, Name: name, Status: doctorSkip, Detail: detail}
	}
	if row.MinExpiresInSeconds == 0 {
		return doctorCheck{Area: "federation", Code: checkCodeEventExpiry, Name: name, Status: doctorSkip, Detail: "no expiry data"}
	}
	detail := fmt.Sprintf("nearest event expires in %ds", row.MinExpiresInSeconds)
	if row.MinExpiresInSeconds < int64(slo.ExpiresSoonSeconds) {
		status := doctorWarn
		if row.Pending > 0 || row.Failed > 0 {
			status = doctorFail
		}
		return doctorCheck{
			Area:   "federation",
			Code:   checkCodeEventExpiry,
			Name:   name,
			Status: status,
			Detail: fmt.Sprintf("%s; %d pending %d failed", detail, row.Pending, row.Failed),
			Remedy: "event TTL running low; verify outbox re-push and peer delivery",
		}
	}
	return doctorCheck{Area: "federation", Code: checkCodeEventExpiry, Name: name, Status: doctorPass, Detail: detail}
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
					Code:   checkCodeExpectedDeliveryNoEndpoint,
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
					Code:   checkCodeExpectedDelivery,
					Name:   label,
					Status: doctorFail,
					Detail: detail,
					Remedy: "outbox never enqueued delivery for this peer; check EventPeer config and outbox peer filter",
				})
			} else {
				checks = append(checks, doctorCheck{
					Area:   "federation",
					Code:   checkCodeExpectedDelivery,
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

	if subStore, ok := r.store.(routerstate.SubscriptionRunStore); ok && r.router != nil {
		for _, res := range r.router.Spec.Resources {
			if res.Kind != "EventSubscription" {
				continue
			}
			subKey := "EventSubscription/" + res.Metadata.Name
			runs, err := subStore.ListSubscriptionRuns(subKey)
			if err != nil {
				continue
			}
			for _, run := range runs {
				fs.SubscriptionRunsTotal++
				switch run.Status {
				case "succeeded":
					fs.SubscriptionRunsSucceeded++
				case "failed":
					fs.SubscriptionRunsFailed++
				default:
					fs.SubscriptionRunsPending++
				}
			}
		}
	}

	sloStatus := r.buildFederationSLOStatus(rows)
	fs.SLO = &sloStatus
	return fs
}

func (r doctorRunner) buildFederationSLOStatus(rows []routerstate.DeliverySummaryRow) doctorFederationSLOStatus {
	groupSet := map[string]bool{}
	if r.router != nil {
		for _, res := range r.router.Spec.Resources {
			if res.Kind == "EventGroup" {
				groupSet[res.Metadata.Name] = true
			}
		}
	}
	for _, row := range rows {
		groupSet[row.Group] = true
	}

	groups := make([]string, 0, len(groupSet))
	for g := range groupSet {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	rowsByGroup := map[string][]routerstate.DeliverySummaryRow{}
	for _, row := range rows {
		rowsByGroup[row.Group] = append(rowsByGroup[row.Group], row)
	}

	sloResourceGroups := map[string]bool{}
	if r.router != nil {
		for _, res := range r.router.Spec.Resources {
			if res.Kind == "FederationSLO" {
				spec, err := res.FederationSLOSpec()
				if err != nil {
					continue
				}
				sloResourceGroups[strings.TrimSpace(spec.GroupRef)] = true
			}
		}
	}

	var status doctorFederationSLOStatus
	for _, g := range groups {
		slo := loadSLOThresholds(r.router, g)
		gs := doctorFederationSLOGroupStatus{
			Group:   g,
			Defined: sloResourceGroups[g],
			Thresholds: doctorFederationSLOThresholds{
				Delivery: doctorFederationSLODeliveryValues{
					LagWarnSeconds:     slo.LagWarnSeconds,
					LagFailSeconds:     slo.LagFailSeconds,
					ExpiresSoonSeconds: slo.ExpiresSoonSeconds,
				},
				Subscription: doctorFederationSLOSubscriptionValues{
					MaxPendingRuns: slo.MaxPendingRuns,
					MaxFailedRuns:  slo.MaxFailedRuns,
				},
			},
			Violations: []doctorFederationViolation{},
		}
		for _, row := range rowsByGroup[g] {
			label := row.Group + "/" + row.Peer
			if row.MaxLagSeconds >= int64(slo.LagFailSeconds) {
				gs.Violations = append(gs.Violations, doctorFederationViolation{
					Check:     label + " delivery lag",
					Threshold: fmt.Sprintf("lagFailSeconds=%d", slo.LagFailSeconds),
					Actual:    fmt.Sprintf("%ds", row.MaxLagSeconds),
					Severity:  doctorFail,
				})
			} else if row.MaxLagSeconds >= int64(slo.LagWarnSeconds) {
				gs.Violations = append(gs.Violations, doctorFederationViolation{
					Check:     label + " delivery lag",
					Threshold: fmt.Sprintf("lagWarnSeconds=%d", slo.LagWarnSeconds),
					Actual:    fmt.Sprintf("%ds", row.MaxLagSeconds),
					Severity:  doctorWarn,
				})
			}
			if row.Failed > 0 {
				gs.Violations = append(gs.Violations, doctorFederationViolation{
					Check:     label + " failed deliveries",
					Threshold: "failedDeliveryCount=0",
					Actual:    fmt.Sprintf("%d", row.Failed),
					Severity:  doctorFail,
				})
			}
			if row.Pending > 0 && row.MinExpiresInSeconds > 0 && row.MinExpiresInSeconds < int64(slo.ExpiresSoonSeconds) {
				gs.Violations = append(gs.Violations, doctorFederationViolation{
					Check:     label + " pending expiring-soon",
					Threshold: fmt.Sprintf("expiresSoonSeconds=%d", slo.ExpiresSoonSeconds),
					Actual:    fmt.Sprintf("minExpiresIn=%ds, pending=%d", row.MinExpiresInSeconds, row.Pending),
					Severity:  doctorFail,
				})
			}
		}
		status.Groups = append(status.Groups, gs)
	}
	return status
}

func (r doctorRunner) buildFederationRemediationPlan(checks []doctorCheck) *doctorRemediationPlan {
	plan := &doctorRemediationPlan{
		GeneratedAt: doctorNow().UTC().Format("2006-01-02T15:04:05Z"),
		Actions:     []doctorRemediationAction{},
	}

	type actionKey struct {
		action, group, peer, resource string
	}
	seen := map[actionKey]bool{}

	for _, c := range checks {
		if c.Area != "federation" || c.Status == doctorPass || c.Status == doctorSkip {
			continue
		}
		action := remediationActionFromCheck(c)
		if action.Action == "" {
			continue
		}
		k := actionKey{action.Action, action.TargetGroup, action.TargetPeer, action.TargetResource}
		if seen[k] {
			continue
		}
		seen[k] = true
		plan.Actions = append(plan.Actions, action)
	}

	sort.Slice(plan.Actions, func(i, j int) bool {
		a, b := plan.Actions[i], plan.Actions[j]
		if a.Action != b.Action {
			return a.Action < b.Action
		}
		if a.TargetGroup != b.TargetGroup {
			return a.TargetGroup < b.TargetGroup
		}
		if a.TargetPeer != b.TargetPeer {
			return a.TargetPeer < b.TargetPeer
		}
		return a.TargetResource < b.TargetResource
	})
	return plan
}

func remediationActionFromCheck(c doctorCheck) doctorRemediationAction {
	group, peer := splitGroupPeerFromLabel(c.Name)
	switch c.Code {
	case checkCodeFailedDeliveries:
		return doctorRemediationAction{
			Action: remediationRetryFailedDeliveries, Reason: c.Detail,
			TargetPeer: peer, TargetGroup: group,
			Safe: true, RequiresOperatorApproval: false,
		}
	case checkCodePendingDeliveries:
		return doctorRemediationAction{
			Action: remediationInvestigatePendingDeliveries, Reason: c.Detail,
			TargetPeer: peer, TargetGroup: group,
			Safe: true, RequiresOperatorApproval: false,
		}
	case checkCodeStaleTTL:
		return doctorRemediationAction{
			Action: remediationForceRepushStaleTTL, Reason: c.Detail,
			TargetPeer: peer, TargetGroup: group,
			Safe: true, RequiresOperatorApproval: false,
		}
	case checkCodeDeliveryLag:
		return doctorRemediationAction{
			Action: remediationCheckPeerConnectivity, Reason: c.Detail,
			TargetPeer: peer, TargetGroup: group,
			Safe: true, RequiresOperatorApproval: false,
		}
	case checkCodeExpectedDeliveryNoEndpoint:
		return doctorRemediationAction{
			Action: remediationConfigurePeerEndpoint, Reason: c.Detail,
			TargetPeer: peer, TargetGroup: group,
			Safe: false, RequiresOperatorApproval: true,
		}
	case checkCodeExpectedDelivery:
		return doctorRemediationAction{
			Action: remediationInvestigateMissingDeliveryRows, Reason: c.Detail,
			TargetPeer: peer, TargetGroup: group,
			Safe: true, RequiresOperatorApproval: false,
		}
	case checkCodeSubscriptionRuns:
		targetResource := strings.TrimSuffix(c.Name, " subscription runs")
		return doctorRemediationAction{
			Action: remediationInspectFailedSubscriptionRuns, Reason: c.Detail,
			TargetResource: targetResource,
			Safe: true, RequiresOperatorApproval: false,
		}
	}
	return doctorRemediationAction{}
}

func splitGroupPeerFromLabel(name string) (string, string) {
	// name format: "group/peer check-type" e.g. "cloudedge/leaf-az delivery lag"
	parts := strings.SplitN(name, " ", 2)
	if len(parts) == 0 {
		return "", ""
	}
	groupPeer := parts[0]
	if idx := strings.Index(groupPeer, "/"); idx >= 0 {
		return groupPeer[:idx], groupPeer[idx+1:]
	}
	return groupPeer, ""
}

func (r doctorRunner) doctorFederationSubscriptionChecks() []doctorCheck {
	if r.router == nil {
		return nil
	}
	subStore, ok := r.store.(routerstate.SubscriptionRunStore)
	if !ok {
		return nil
	}
	type subInfo struct {
		name     string
		groupRef string
	}
	var subs []subInfo
	for _, res := range r.router.Spec.Resources {
		if res.Kind == "EventSubscription" {
			spec, err := res.EventSubscriptionSpec()
			if err != nil {
				continue
			}
			subs = append(subs, subInfo{name: res.Metadata.Name, groupRef: strings.TrimSpace(spec.GroupRef)})
		}
	}
	if len(subs) == 0 {
		return nil
	}
	var checks []doctorCheck
	for _, sub := range subs {
		subKey := "EventSubscription/" + sub.name
		slo := loadSLOThresholds(r.router, sub.groupRef)
		runs, err := subStore.ListSubscriptionRuns(subKey)
		if err != nil {
			checks = append(checks, doctorCheck{
				Area: "federation", Code: checkCodeSubscriptionRuns, Name: subKey + " subscription runs",
				Status: doctorWarn, Detail: "subscription runs unavailable: " + err.Error(),
			})
			continue
		}
		if len(runs) == 0 {
			checks = append(checks, doctorCheck{
				Area: "federation", Code: checkCodeSubscriptionRuns, Name: subKey + " subscription runs",
				Status: doctorSkip, Detail: "no subscription run records",
			})
			continue
		}
		var succeeded, failed, pending int
		var maxAttempts int
		for _, run := range runs {
			switch run.Status {
			case "succeeded":
				succeeded++
			case "failed":
				failed++
			default:
				pending++
			}
			if run.Attempts > maxAttempts {
				maxAttempts = run.Attempts
			}
		}
		if failed > slo.MaxFailedRuns {
			checks = append(checks, doctorCheck{
				Area:   "federation",
				Code:   checkCodeSubscriptionRuns,
				Name:   subKey + " subscription runs",
				Status: doctorFail,
				Detail: fmt.Sprintf("%d succeeded, %d failed, %d pending (max attempts %d)", succeeded, failed, pending, maxAttempts),
				Remedy: "inspect failed runs: routerctl federation subscription runs --subscription " + subKey,
			})
		} else if pending > slo.MaxPendingRuns {
			checks = append(checks, doctorCheck{
				Area:   "federation",
				Code:   checkCodeSubscriptionRuns,
				Name:   subKey + " subscription runs",
				Status: doctorWarn,
				Detail: fmt.Sprintf("%d succeeded, %d pending (max attempts %d)", succeeded, pending, maxAttempts),
				Remedy: "pending runs may resolve on next reconcile tick; if persistent, check plugin and eventd logs",
			})
		} else {
			checks = append(checks, doctorCheck{
				Area:   "federation",
				Code:   checkCodeSubscriptionRuns,
				Name:   subKey + " subscription runs",
				Status: doctorPass,
				Detail: fmt.Sprintf("all %d run(s) succeeded", succeeded),
			})
		}
	}
	return checks
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
