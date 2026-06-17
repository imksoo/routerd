// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	ShardAssignedEventType = "routerd.mobility.shard.assigned"
	ShardExpiredEventType  = "routerd.mobility.shard.expired"
	shardEventTTL          = 10 * time.Minute
)

type ShardStore interface {
	RecordFederationEvent(rec routerstate.EventRecord) error
	ListFederationEvents(group string, includeExpired bool, now int64) ([]routerstate.EventRecord, error)
	MergeObjectStatus(apiVersion, kind, name string, updates map[string]any) error
}

type ShardController struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  ShardStore
	Now    func() time.Time
}

func (c ShardController) HandleEvent(ctx context.Context, _ interface{}) error {
	return c.Reconcile(ctx)
}

func (c ShardController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	now := c.now()
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "SAMSubnetPolicy" {
			continue
		}
		spec, err := res.SAMSubnetPolicySpec()
		if err != nil {
			_ = c.Store.MergeObjectStatus(api.MobilityAPIVersion, "SAMSubnetPolicy", res.Metadata.Name, map[string]any{
				"shardPhase":  "Degraded",
				"shardReason": err.Error(),
			})
			continue
		}
		if err := c.reconcilePolicy(res.Metadata.Name, spec, now); err != nil {
			_ = c.Store.MergeObjectStatus(api.MobilityAPIVersion, "SAMSubnetPolicy", res.Metadata.Name, map[string]any{
				"shardPhase":  "Degraded",
				"shardReason": err.Error(),
			})
		}
	}
	return nil
}

func (c ShardController) reconcilePolicy(policyName string, spec api.SAMSubnetPolicySpec, now time.Time) error {
	selfNode, err := routerSelfNode(c.Router, spec.GroupRef)
	if err != nil {
		return err
	}

	emitted := 0
	for i, shard := range spec.Shards {
		prefix := strings.TrimSpace(shard.Prefix)
		if prefix == "" {
			continue
		}
		for _, nodeRef := range shard.AssignedNodes {
			nodeRef = strings.TrimSpace(nodeRef)
			if nodeRef == "" {
				continue
			}
			ev := shardAssignmentEvent(policyName, spec.GroupRef, spec.PoolRef, prefix, selfNode, nodeRef, i, now)
			if err := c.Store.RecordFederationEvent(ev); err != nil {
				return fmt.Errorf("emit shard event for %s shard[%d] node %s: %w", policyName, i, nodeRef, err)
			}
			emitted++
		}
	}

	_ = c.Store.MergeObjectStatus(api.MobilityAPIVersion, "SAMSubnetPolicy", policyName, map[string]any{
		"shardPhase":       "Active",
		"shardCount":       len(spec.Shards),
		"emittedEvents":    emitted,
		"lastReconciledAt": now.Format(time.RFC3339Nano),
	})
	return nil
}

func shardAssignmentEvent(policyName, group, poolRef, prefix, sourceNode, targetNode string, shardIndex int, now time.Time) routerstate.EventRecord {
	dedupeKey := fmt.Sprintf("shard:%s:%s:%s", policyName, prefix, targetNode)
	h := sha256.Sum256([]byte(dedupeKey))
	id := "shard-" + hex.EncodeToString(h[:8])
	return routerstate.EventRecord{
		ID:         id,
		Group:      group,
		SourceNode: sourceNode,
		Type:       ShardAssignedEventType,
		Subject:    fmt.Sprintf("SAMSubnetPolicy/%s/shard/%d", policyName, shardIndex),
		DedupeKey:  dedupeKey,
		Payload: map[string]string{
			"policy": policyName,
			"pool":   poolRef,
			"prefix": prefix,
			"node":   targetNode,
		},
		ObservedAt: now.UTC(),
		ExpiresAt:  now.Add(shardEventTTL).UTC(),
	}
}

type shardEventReader interface {
	ListFederationEvents(group string, includeExpired bool, now int64) ([]routerstate.EventRecord, error)
}

// ResolveShardScope reads shard assignment events from the federation store and
// returns the union of assigned shard prefixes for the given node and pool. If
// no shard events exist, it returns nil (no shard filtering).
func ResolveShardScope(store shardEventReader, group, poolRef, selfNode string, now time.Time) []string {
	events, err := store.ListFederationEvents(group, false, now.Unix())
	if err != nil {
		return nil
	}
	var prefixes []string
	seen := map[string]bool{}
	for _, ev := range events {
		if ev.Type != ShardAssignedEventType {
			continue
		}
		pool := ev.Payload["pool"]
		node := ev.Payload["node"]
		prefix := ev.Payload["prefix"]
		if pool != poolRef || node != selfNode || prefix == "" {
			continue
		}
		if seen[prefix] {
			continue
		}
		if _, err := netip.ParsePrefix(prefix); err != nil {
			continue
		}
		seen[prefix] = true
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	return prefixes
}

func (c ShardController) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
