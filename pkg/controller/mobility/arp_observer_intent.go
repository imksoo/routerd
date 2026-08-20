// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

// ARPObserverDynamicSource is independent from the BGP delivery source because
// ARP observation is a discovery input, not an effect of a completed BGP plan.
func ARPObserverDynamicSource(poolName, selfNode string) string {
	return DynamicSource(poolName, selfNode) + "/arp-observer"
}

// arpObserverIntents projects the already-normalized local discovery overlay
// into the small daemon-bootstrap contract consumed by chain. It resolves
// status-backed source addresses and interface aliases once here, so the
// daemon supervisor never needs to reopen MobilityPool configuration.
func (c DiscoveryController) arpObserverIntents(pool NormalizedMobilityPool) []dynamicconfig.ARPObserverIntent {
	self := pool.Self
	if strings.TrimSpace(self.Role) != "onprem" || strings.TrimSpace(self.Capture.Type) != "proxy-arp" || strings.TrimSpace(self.OwnershipDiscovery.Mode) != "onprem-l2" {
		return nil
	}
	prefix := pool.Prefix
	if !prefix.IsValid() || !prefix.Addr().Is4() || prefix.Bits() == 32 {
		return nil
	}
	prefix = prefix.Masked()
	captureSourceAddress := resolveCaptureSourceAddress(c.Store, pool.Self.CaptureSourceAddress, pool.Self.CaptureSourceAddressFrom, prefix)
	ignoredSenderMACs := normalizedSAMNodeSetMACAddresses(c.Router)
	var out []dynamicconfig.ARPObserverIntent
	sourceIndex := 0
	for _, source := range onPremDiscoverySources(self.OwnershipDiscovery) {
		sourceType := strings.TrimSpace(source.Type)
		if sourceType != OnPremSourceARPObserver && sourceType != OnPremSourceOnDemandARP && sourceType != OnPremSourcePVESVNet {
			continue
		}
		eventInterface := strings.TrimSpace(firstNonEmpty(source.Interface, self.Capture.Interface, source.Bridge, source.Network))
		if eventInterface == "" {
			continue
		}
		resourceName := arpObserverIntentResourceName(pool, source, sourceIndex, eventInterface)
		sourceIndex++
		ifName := strings.TrimSpace(api.ResolveInterfaceIfName(c.Router, eventInterface))
		if ifName == "" {
			ifName = eventInterface
		}
		sourceAddress := ""
		if sourceType == OnPremSourceOnDemandARP {
			sourceAddress = resolveARPObserverSourceAddress(c.Store, source.SourceAddressFrom)
			if sourceAddress == "" {
				// The capture sender is already the normalized local source for
				// proxy-ARP delivery. Reuse it when the probe source does not
				// override it, as PVE pool configuration normally does. Never
				// emit an invalid on-demand intent without a verified sender.
				if captureSource, ok := captureSourcePrefix(captureSourceAddress, prefix); ok {
					sourceAddress = captureSource.Addr().String()
				}
			}
			if sourceAddress == "" {
				continue
			}
		}
		out = append(out, dynamicconfig.ARPObserverIntent{
			ResourceName:      resourceName,
			PoolRef:           strings.TrimSpace(pool.Name),
			Prefix:            prefix.String(),
			SourceType:        sourceType,
			IfName:            ifName,
			EventInterface:    eventInterface,
			Network:           strings.TrimSpace(source.Network),
			Bridge:            strings.TrimSpace(source.Bridge),
			SourceAddress:     sourceAddress,
			Observe:           sourceType == OnPremSourceARPObserver || sourceType == OnPremSourcePVESVNet,
			OnDemand:          sourceType == OnPremSourceOnDemandARP,
			ProbeTimeout:      strings.TrimSpace(source.ProbeTimeout),
			ProbeRetries:      source.ProbeRetries,
			ScanInterval:      strings.TrimSpace(source.ScanInterval),
			IgnoredSenderMACs: append([]string(nil), ignoredSenderMACs...),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ResourceName == out[j].ResourceName {
			return out[i].SourceType < out[j].SourceType
		}
		return out[i].ResourceName < out[j].ResourceName
	})
	return out
}

// arpObserverIntentResourceName is the internal daemon, socket, and ownership
// identity for one normalized source. source.Resource is intentionally only a
// fingerprint input: it is not safe as a process identity because separate
// pools and source types may legitimately reuse it.
func arpObserverIntentResourceName(pool NormalizedMobilityPool, source api.MobilityOwnershipDiscoverySource, sourceIndex int, eventInterface string) string {
	identity := strings.Join([]string{
		strings.TrimSpace(pool.Name),
		strings.TrimSpace(pool.SelfNode),
		strconv.Itoa(sourceIndex),
		strings.TrimSpace(source.Type),
		strings.TrimSpace(source.Resource),
		strings.TrimSpace(eventInterface),
		strings.TrimSpace(source.Network),
		strings.TrimSpace(source.Bridge),
	}, "\x00")
	sum := sha256.Sum256([]byte(identity))
	poolName := safeName(pool.Name)
	if len(poolName) > 24 {
		poolName = poolName[:24]
	}
	return "mobility-arp-" + poolName + "-" + safeName(source.Type) + "-" + strconv.Itoa(sourceIndex) + "-" + hex.EncodeToString(sum[:8])
}

// resolveARPObserverSourceAddress resolves the sender address for an active
// ARP observer. Unlike capture.SourceAddressFrom, this source is not a
// mobility-prefix address: it can be any local IPv4 address on the probing
// interface. Keep its validation here, at the typed producer boundary, rather
// than borrowing the Pool capture resolver and accidentally applying that
// resolver's in-pool constraint.
func resolveARPObserverSourceAddress(reader resourcequery.Store, source api.StatusValueSourceSpec) string {
	for _, raw := range resourcequery.Values(reader, source) {
		if prefix, ok := parseIPv4AddressOrPrefix(raw); ok {
			return prefix.Addr().String()
		}
	}
	return ""
}

func normalizedSAMNodeSetMACAddresses(router *api.Router) []string {
	if router == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "SAMNodeSet" {
			continue
		}
		spec, err := res.SAMNodeSetSpec()
		if err != nil {
			continue
		}
		for _, node := range spec.Nodes {
			for _, value := range node.MACAddresses {
				mac, err := net.ParseMAC(strings.TrimSpace(value))
				if err == nil {
					seen[strings.ToLower(mac.String())] = true
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for mac := range seen {
		out = append(out, mac)
	}
	sort.Strings(out)
	return out
}

func (c DiscoveryController) upsertARPObserverIntents(pool NormalizedMobilityPool, intents []dynamicconfig.ARPObserverIntent, now time.Time) error {
	now = now.UTC()
	part := dynamicconfig.NewPart(safeName("mobility-"+pool.Name+"-"+pool.SelfNode+"-arp-observer"), ARPObserverDynamicSource(pool.Name, pool.SelfNode), []api.OwnerRef{{
		APIVersion: api.MobilityAPIVersion,
		Kind:       "MobilityPool",
		Name:       pool.Name,
	}}, dynamicGeneration, now, now.Add(DefaultLeaseTTL))
	part.Spec.Resources = []api.Resource{}
	part.Spec.ActionPlans = []dynamicconfig.ActionPlan{}
	part.Spec.MobilityDataplane = dynamicconfig.MobilityDataplanePlan{}
	part.Spec.ARPObserverIntents = append([]dynamicconfig.ARPObserverIntent(nil), intents...)
	part.Spec.FIBVerdicts = []dynamicconfig.FIBVerdict{}
	part.Spec.Digest = digestDynamicPart(part)
	record, err := codec.Encode(part)
	if err != nil {
		return err
	}
	return c.Store.UpsertDynamicConfigPart(record)
}
