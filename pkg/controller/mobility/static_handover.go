// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// A release authorizes only its declared transfer, never another pool, target
// or attempt that happens to use the same address. It remains a finite lease.
func staticHandoverReleaseMatches(pool NormalizedMobilityPool, handover api.MobilityStaticHandover, event routerstate.EventRecord, now time.Time) bool {
	id := strings.TrimSpace(handover.ID)
	from, to := strings.TrimSpace(handover.FromNodeRef), strings.TrimSpace(handover.ToNodeRef)
	address, valid := normalizeLeaseAddress(handover.Address, pool.Prefix)
	if now.IsZero() || !valid || id == "" || from == "" || to == "" || from == to {
		return false
	}
	if event.Group != pool.Spec.GroupRef || event.Type != ExpiredEventType || event.SourceNode != from || event.Payload["sourceType"] != staticHandoverType || event.Payload["pool"] != pool.Name || event.Payload["handoverID"] != id || event.Payload["fromNodeRef"] != from || event.Payload["toNodeRef"] != to {
		return false
	}
	payloadAddress, valid := normalizeLeaseAddress(event.Payload["address"], pool.Prefix)
	if !valid || payloadAddress != address {
		return false
	}
	subject, valid := normalizeLeaseAddress(event.Subject, pool.Prefix)
	return valid && subject == address && !event.ObservedAt.IsZero() && !event.ExpiresAt.IsZero() && event.ExpiresAt.After(event.ObservedAt) && now.Before(event.ExpiresAt)
}

// A live release already certifies this transfer. Its normal remote delivery
// captures may resume, including while refreshing the finite release lease.
// Without that proof, withdraw this address before asking native owners to ack.
func planHandoverNativeWithdrawals(snapshot PoolRuntimeSnapshot, plan dynamicconfig.MobilityDataplanePlan) dynamicconfig.MobilityDataplanePlan {
	pool := snapshot.Pool
	for _, handover := range pool.Spec.StaticHandovers {
		if strings.TrimSpace(handover.FromNodeRef) != pool.SelfNode {
			continue
		}
		live := false
		for _, event := range snapshot.Events {
			if staticHandoverReleaseMatches(pool, handover, event, snapshot.Now) {
				live = true
				break
			}
		}
		if live {
			continue
		}
		address, valid := normalizeLeaseAddress(handover.Address, pool.Prefix)
		if !valid {
			continue
		} // Transfer identity admission is owned by the planner.
		for i := range plan.Captures {
			if plan.Captures[i].PoolRef == pool.Name && plan.Captures[i].Address == address {
				plan.Captures[i].Disposition = dynamicconfig.CaptureRelease
				plan.Captures[i].Reason = "static-handover-native-withdrawal"
			}
		}
		routes := plan.Routes[:0]
		for _, route := range plan.Routes {
			if route.PoolRef != pool.Name || route.Destination != address {
				routes = append(routes, route)
			}
		}
		plan.Routes = routes
		addresses := plan.StaticAddresses[:0]
		for _, local := range plan.StaticAddresses {
			if local.PoolRef != pool.Name || local.Address != address {
				addresses = append(addresses, local)
			}
		}
		plan.StaticAddresses = addresses
		plan.PoolPrefix = pool.Prefix.String()
		plan.Withdrawals = append(plan.Withdrawals, dynamicconfig.MobilityWithdrawalRequest{ID: strings.TrimSpace(handover.ID), GroupRef: pool.Spec.GroupRef, PoolRef: pool.Name, PoolPrefix: pool.Prefix.String(), Address: address, FromNodeRef: pool.SelfNode, ToNodeRef: strings.TrimSpace(handover.ToNodeRef)})
	}
	digest := dynamicconfig.MobilityNativePlanDigest(plan)
	for i := range plan.Withdrawals {
		plan.Withdrawals[i].PlanDigest = digest
	}
	return plan
}

func (c Controller) confirmedNativeWithdrawals(pool NormalizedMobilityPool, requests []dynamicconfig.MobilityWithdrawalRequest, now time.Time) (map[dynamicconfig.MobilityWithdrawalRequest]bool, error) {
	confirmed := make(map[dynamicconfig.MobilityWithdrawalRequest]bool, len(requests))
	if len(requests) == 0 {
		return confirmed, nil
	}
	parts, err := c.Store.GetDynamicConfigPartsBySource(pool.Source)
	if err != nil {
		return nil, fmt.Errorf("recheck current native handover request: %w", err)
	}
	active, invalid := codec.ActiveMobilityPoolPlanRecords(parts, now)
	if len(invalid) != 0 || len(active) != 1 {
		return nil, fmt.Errorf("native handover source %s is not one valid active plan", pool.Source)
	}
	current, err := codec.DecodeMobilityDataplanePlan(active[0].Record.MobilityDataplaneJSON)
	if err != nil {
		return nil, err
	}
	if err := dynamicconfig.ValidateMobilityDataplanePlanScope(current, pool.Name); err != nil {
		return nil, err
	}
	for _, request := range requests {
		found := false
		for _, stored := range current.Withdrawals {
			if request == stored && request.FromNodeRef == pool.SelfNode {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("native handover request %q changed before publication", request.ID)
		}
	}
	status, err := c.Store.ReadObjectStatus(api.RouterAPIVersion, "Router", dynamicconfig.MobilityNativeStatusName)
	if err != nil {
		return nil, fmt.Errorf("read native handover completion: %w", err)
	}
	for _, request := range requests {
		conflict, err := c.staticAddressConflictsWithHandover(request)
		if err != nil {
			return nil, err
		}
		confirmed[request] = !conflict
	}
	for _, field := range []string{dynamicconfig.MobilitySAMWithdrawalAcks, dynamicconfig.MobilityRouteWithdrawalAcks, dynamicconfig.MobilityAddressWithdrawalAcks} {
		matches := map[dynamicconfig.MobilityWithdrawalRequest]bool{}
		if value, present := status[field]; present {
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("encode native %s status: %w", field, err)
			}
			var acks []dynamicconfig.MobilityWithdrawalRequest
			if err := json.Unmarshal(encoded, &acks); err != nil || acks == nil {
				return nil, fmt.Errorf("invalid native %s acknowledgement array", field)
			}
			for _, ack := range acks {
				if err := dynamicconfig.ValidateMobilityWithdrawalRequest(ack); err != nil {
					return nil, fmt.Errorf("invalid native %s acknowledgement: %w", field, err)
				}
				matches[ack] = true
			}
		}
		for _, request := range requests {
			confirmed[request] = confirmed[request] && matches[request]
		}
	}
	return confirmed, nil
}

func (c Controller) staticAddressConflictsWithHandover(request dynamicconfig.MobilityWithdrawalRequest) (bool, error) {
	declared := c.DeclaredRouter
	if declared == nil {
		declared = c.Router
	}
	if declared == nil {
		return false, nil
	}
	host, _ := netip.ParsePrefix(request.Address)
	for _, resource := range declared.Spec.Resources {
		if resource.Kind != "IPv4StaticAddress" {
			continue
		}
		spec, err := resource.IPv4StaticAddressSpec()
		if err != nil {
			return false, err
		}
		address, err := netip.ParsePrefix(spec.Address)
		if err != nil {
			return false, err
		}
		if address.Addr() != host.Addr() {
			continue
		}
		eligible := true
		if resourcequery.ResourceWhenPresent(spec.When) {
			stateStore, _ := c.Store.(resourcequery.StateStore)
			eligible, err = resourcequery.ResourceWhenEligible(spec.When, stateStore)
			if err != nil {
				return false, err
			}
		}
		if eligible {
			return true, nil
		}
		status, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", resource.Metadata.Name)
		if err != nil {
			return false, err
		}
		removed, _ := status["staticAddressRemoved"].(bool)
		dryRun, _ := status["dryRun"].(bool)
		if !removed || dryRun || status["interface"] != spec.Interface || status["address"] != spec.Address {
			return true, nil
		}
	}
	return false, nil
}

func validateHandoverReleaseEffects(pool NormalizedMobilityPool, plan PoolPlan) error {
	for _, release := range plan.HandoverReleases {
		matched := false
		for _, declared := range pool.Spec.StaticHandovers {
			address, valid := normalizeLeaseAddress(declared.Address, pool.Prefix)
			if valid && release.Address == address && release.ID != "" && release.ID == strings.TrimSpace(declared.ID) && release.FromNodeRef == pool.SelfNode && release.FromNodeRef == strings.TrimSpace(declared.FromNodeRef) && release.ToNodeRef != release.FromNodeRef && release.ToNodeRef == strings.TrimSpace(declared.ToNodeRef) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("handover release %q is not this node's declared transfer", release.ID)
		}
		for _, path := range plan.BGPPaths {
			if address, valid := normalizeLeaseAddress(path.Prefix, pool.Prefix); valid && address == release.Address {
				return fmt.Errorf("handover release %q still advertises %s", release.ID, release.Address)
			}
		}
	}
	return nil
}

func planStaticHandoverReleases(snapshot PoolRuntimeSnapshot) ([]api.MobilityStaticHandover, error) {
	pool := snapshot.Pool
	var releases []api.MobilityStaticHandover
	for _, handover := range pool.Spec.StaticHandovers {
		handover.ID = strings.TrimSpace(handover.ID)
		if handover.ID == "" {
			return nil, fmt.Errorf("MobilityPool/%s static handover id is required", pool.Name)
		}
		handover.FromNodeRef = strings.TrimSpace(handover.FromNodeRef)
		handover.ToNodeRef = strings.TrimSpace(handover.ToNodeRef)
		if handover.FromNodeRef != pool.SelfNode {
			continue
		}
		address, valid := normalizeLeaseAddress(handover.Address, pool.Prefix)
		if !valid || handover.ToNodeRef == "" || handover.ToNodeRef == handover.FromNodeRef {
			return nil, fmt.Errorf("MobilityPool/%s static handover %q has invalid transfer identity", pool.Name, handover.ID)
		}
		handover.Address = address
		fresh := false
		for _, event := range snapshot.Events {
			if staticHandoverReleaseMatches(pool, handover, event, snapshot.Now) && event.ExpiresAt.After(snapshot.Now.Add(DefaultLeaseTTL/2)) {
				fresh = true
				break
			}
		}
		if !fresh {
			releases = append(releases, handover)
		}
	}
	return releases, nil
}

func staticHandoverReleaseEvent(pool NormalizedMobilityPool, handover api.MobilityStaticHandover, now time.Time) routerstate.EventRecord {
	// Include every immutable payload identity: federation storage refreshes
	// times on repeated IDs while retaining the original payload.
	identity := strings.Join([]string{pool.Spec.GroupRef, pool.Name, handover.ID, handover.Address, handover.FromNodeRef, handover.ToNodeRef}, "\x00")
	id := fmt.Sprintf("mobility:static-release:%x", sha256.Sum256([]byte(identity)))
	return routerstate.EventRecord{
		ID: id, DedupeKey: id, Group: pool.Spec.GroupRef,
		SourceNode: handover.FromNodeRef, Type: ExpiredEventType, Subject: handover.Address,
		Payload: map[string]string{
			"pool": pool.Name, "handoverID": handover.ID,
			"address": handover.Address, "fromNodeRef": handover.FromNodeRef,
			"toNodeRef": handover.ToNodeRef, "sourceType": staticHandoverType,
		},
		ObservedAt: now.UTC(), RecordedAt: now.UTC(), ExpiresAt: now.UTC().Add(DefaultLeaseTTL),
	}
}
