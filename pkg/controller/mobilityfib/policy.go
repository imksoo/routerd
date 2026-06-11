// SPDX-License-Identifier: BSD-3-Clause

package mobilityfib

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
)

type StatusReader interface {
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type Snapshot struct {
	pools []poolSnapshot
}

type poolSnapshot struct {
	name        string
	selfNode    string
	prefix      netip.Prefix
	verdicts    map[string]Verdict
	staticLocal map[string]bool
}

const (
	ActionDeliverRemote = "deliver-remote"
	ActionLocalRoute    = "local-route"
	ActionWithhold      = "withhold"
)

type Verdict struct {
	Address   string
	Action    string
	OwnerNode string
	Reason    string
}

func NewSnapshot(router *api.Router, reader StatusReader) Snapshot {
	if router == nil {
		return Snapshot{}
	}
	selfByGroup := eventGroupSelfNodes(router)
	if len(selfByGroup) == 0 {
		return Snapshot{}
	}
	var pools []poolSnapshot
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "MobilityPool" {
			continue
		}
		spec, err := resource.MobilityPoolSpec()
		if err != nil || mobilityDeliveryMode(spec) != "bgp" {
			continue
		}
		selfNode := strings.TrimSpace(selfByGroup[strings.TrimSpace(spec.GroupRef)])
		if selfNode == "" {
			continue
		}
		spec, _, err = mobilityconfig.NormalizeMobilityPool(spec, selfNode)
		if err != nil {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
		if err != nil || !prefix.Addr().Is4() {
			continue
		}
		pool := poolSnapshot{
			name:        resource.Metadata.Name,
			selfNode:    selfNode,
			prefix:      prefix.Masked(),
			verdicts:    map[string]Verdict{},
			staticLocal: map[string]bool{},
		}
		for _, member := range spec.Members {
			if strings.TrimSpace(member.NodeRef) != selfNode {
				continue
			}
			for _, raw := range member.StaticOwnedAddresses {
				if address, ok := normalizePoolAddress(raw, pool.prefix); ok {
					pool.staticLocal[address] = true
				}
			}
		}
		if reader != nil {
			status := reader.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", resource.Metadata.Name)
			for _, row := range statusMapSlice(status["ownershipResolverFIBVerdicts"]) {
				parsed := verdictFromMap(row)
				address, ok := normalizePoolAddress(parsed.Address, pool.prefix)
				if !ok {
					continue
				}
				parsed.Address = address
				pool.verdicts[address] = parsed
			}
		}
		pools = append(pools, pool)
	}
	return Snapshot{pools: pools}
}

func (s Snapshot) AdmitBGPRoute(prefix netip.Prefix) bool {
	prefix = prefix.Masked()
	pool, ok := s.poolFor(prefix)
	if !ok {
		return true
	}
	if prefix.Bits() != 32 {
		return false
	}
	address := prefix.String()
	if pool.staticLocal[address] {
		return false
	}
	verdict, ok := pool.verdicts[address]
	if !ok {
		return false
	}
	return strings.TrimSpace(verdict.Action) == ActionDeliverRemote
}

func (s Snapshot) LocalRouteAddresses() []string {
	seen := map[string]bool{}
	for _, pool := range s.pools {
		for _, address := range pool.localRouteAddresses() {
			seen[address] = true
		}
	}
	return sortedKeys(seen)
}

func (s Snapshot) LocalRouteAddressesForPool(poolName string) []string {
	poolName = strings.TrimSpace(poolName)
	seen := map[string]bool{}
	for _, pool := range s.pools {
		if pool.name != poolName {
			continue
		}
		for _, address := range pool.localRouteAddresses() {
			seen[address] = true
		}
	}
	return sortedKeys(seen)
}

func (p poolSnapshot) localRouteAddresses() []string {
	seen := map[string]bool{}
	for address := range p.staticLocal {
		seen[address] = true
	}
	for address, verdict := range p.verdicts {
		if strings.TrimSpace(verdict.Action) == ActionLocalRoute {
			seen[address] = true
		}
	}
	return sortedKeys(seen)
}

func (s Snapshot) poolFor(prefix netip.Prefix) (poolSnapshot, bool) {
	if !prefix.Addr().Is4() {
		return poolSnapshot{}, false
	}
	for _, pool := range s.pools {
		if pool.prefix.Contains(prefix.Addr()) {
			return pool, true
		}
	}
	return poolSnapshot{}, false
}

func verdictFromMap(row map[string]string) Verdict {
	return Verdict{
		Address:   row["address"],
		Action:    row["action"],
		OwnerNode: row["ownerNode"],
		Reason:    row["reason"],
	}
}

func eventGroupSelfNodes(router *api.Router) map[string]string {
	out := map[string]string{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.FederationAPIVersion || resource.Kind != "EventGroup" {
			continue
		}
		spec, err := resource.EventGroupSpec()
		if err == nil {
			out[resource.Metadata.Name] = strings.TrimSpace(spec.NodeName)
		}
	}
	return out
}

func mobilityDeliveryMode(spec api.MobilityPoolSpec) string {
	switch strings.TrimSpace(spec.DeliveryPolicy.Mode) {
	case "":
		return "bgp"
	default:
		return strings.TrimSpace(spec.DeliveryPolicy.Mode)
	}
}

func normalizePoolAddress(value string, pool netip.Prefix) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || !pool.Addr().Is4() {
		return "", false
	}
	var addr netip.Addr
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !prefix.Addr().Is4() {
			return "", false
		}
		addr = prefix.Addr()
	} else {
		parsed, err := netip.ParseAddr(value)
		if err != nil || !parsed.Is4() {
			return "", false
		}
		addr = parsed
	}
	if !pool.Contains(addr) {
		return "", false
	}
	return netip.PrefixFrom(addr, 32).String(), true
}

func statusMapSlice(value any) []map[string]string {
	var out []map[string]string
	switch typed := value.(type) {
	case []map[string]string:
		return append([]map[string]string(nil), typed...)
	case []map[string]any:
		for _, item := range typed {
			out = append(out, statusAnyMapToStringMap(item))
		}
	case []any:
		for _, item := range typed {
			switch v := item.(type) {
			case map[string]string:
				out = append(out, v)
			case map[string]any:
				out = append(out, statusAnyMapToStringMap(v))
			}
		}
	}
	return out
}

func statusAnyMapToStringMap(values map[string]any) map[string]string {
	out := map[string]string{}
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "<nil>" {
			text = ""
		}
		out[key] = text
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
