// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type showOptions struct {
	Target           string
	Output           string
	ConfigPath       string
	StatePath        string
	LedgerPath       string
	Diff             bool
	LedgerOnly       bool
	AdoptOnly        bool
	Events           bool
	SpecOnly         bool
	StatusOnly       bool
	Verbose          bool
	IncludeStale     bool
	ConnectionsLimit int
}

func derivedResourcesCurrentRouter(router *api.Router, store routerstate.Store) (*api.Router, error) {
	if router == nil {
		return nil, nil
	}
	effective := resourcequery.FilterRouterByWhen(router, store)
	current, err := chain.BuildDynamicRouteSAMEffectiveRouter(effective, store, time.Now().UTC(), platform.CurrentOS())
	if err != nil {
		return nil, err
	}
	return current, nil
}

func resourceIDSet(router *api.Router) map[string]bool {
	out := map[string]bool{}
	if router == nil {
		return out
	}
	for _, res := range router.Spec.Resources {
		out[res.ID()] = true
	}
	return out
}

func plannedEffectiveDerivedResources(startup, current *api.Router) []showResource {
	if current == nil {
		return nil
	}
	explicit := resourceIDSet(startup)
	var rows []showResource
	for _, res := range current.Spec.Resources {
		if res.APIVersion == "" || res.Kind == "" || res.Metadata.Name == "" || explicit[res.ID()] {
			continue
		}
		const source = "dynamic-effective"
		rows = append(rows, showResource{
			APIVersion: res.APIVersion,
			Kind:       res.Kind,
			Name:       res.Metadata.Name,
			Source:     source,
			State:      map[string]any{"phase": "Planned", "source": source},
		})
	}
	return rows
}

func staleObjectStatus(status routerstate.ObjectStatus) map[string]any {
	out := map[string]any{}
	for key, value := range status.Status {
		out[key] = value
	}
	reason := "StaleStateNotInCurrentConfig"
	if showAPIVersionForKnownKind(canonicalShowKind(status.Kind)) == "" {
		reason = "UnsupportedResourceKind"
	}
	if phase := statusvalue.Text(out["phase"]); phase != "" {
		out["previousPhase"] = phase
	}
	out["phase"] = "Stale"
	out["reason"] = reason
	out["stale"] = true
	out["message"] = "state row is not derived from the current router config"
	return out
}

func plannedDerivedShowResources(router *api.Router) []showResource {
	if router == nil {
		return nil
	}
	var rows []showResource
	addServiceUnit := func(name, source string) {
		rows = append(rows, showResource{
			APIVersion: api.SystemAPIVersion,
			Kind:       "ServiceUnit",
			Name:       name,
			Source:     source,
			State:      map[string]any{"phase": "Planned", "source": source},
		})
	}
	addServiceUnit(render.RouterdUnitName, "Router/"+router.Metadata.Name)
	if render.RouterWantsDPIClassifier(router) {
		addServiceUnit(render.DPIClassifierUnitName, "TrafficFlowLog/FirewallEventLog")
	}
	if render.RouterWantsNDPIAgent(router) {
		addServiceUnit(render.NDPIAgentUnitName, "TrafficFlowLog/FirewallEventLog")
	}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "TailscaleNode":
			spec, err := res.TailscaleNodeSpec()
			if err != nil {
				continue
			}
			unit := render.TailscaleSystemdSpec(res.Metadata.Name, spec)
			addServiceUnit(stringutil.FirstNonEmpty(unit.UnitName, render.TailscaleUnitName(res.Metadata.Name)), "TailscaleNode/"+res.Metadata.Name)
		case "HealthCheck":
			addServiceUnit("routerd-healthcheck@"+res.Metadata.Name+".service", "HealthCheck/"+res.Metadata.Name)
		case "FirewallEventLog":
			spec, err := res.FirewallEventLogSpec()
			if err == nil && spec.Enabled {
				addServiceUnit("routerd-firewall-logger.service", "FirewallEventLog/"+res.Metadata.Name)
			}
		}
	}
	return rows
}

func listObjectStatuses(store routerstate.Store) ([]routerstate.ObjectStatus, error) {
	lister, ok := store.(routerstate.ObjectStatusLister)
	if !ok {
		return nil, nil
	}
	return lister.ListObjectStatuses()
}

func eventsForResourceLimit(store routerstate.Store, res api.Resource, limit int) []routerstate.Event {
	recorder, ok := store.(routerstate.EventRecorder)
	if !ok {
		return nil
	}
	return recorder.Events(res.APIVersion, res.Kind, res.Metadata.Name, limit)
}

func observeInterface(ifname string) map[string]any {
	if ifname == "" {
		return map[string]any{"exists": false}
	}
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return map[string]any{"ifname": ifname, "exists": false, "error": err.Error()}
	}
	addrs, _ := iface.Addrs()
	var addressStrings []string
	for _, addr := range addrs {
		addressStrings = append(addressStrings, addr.String())
	}
	sort.Strings(addressStrings)
	return map[string]any{
		"ifname":       ifname,
		"exists":       true,
		"flags":        iface.Flags.String(),
		"mtu":          iface.MTU,
		"hardwareAddr": iface.HardwareAddr.String(),
		"addresses":    addressStrings,
	}
}

func interfaceIPv4Addresses(ifname string) []string {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}
	var out []string
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil || ip.To4() == nil {
			continue
		}
		out = append(out, addr.String())
	}
	sort.Strings(out)
	return out
}

func statePrefixForKind(kind, name string) string {
	prefixes := map[string]string{
		"Interface":            "interface.",
		"IPv4StaticAddress":    "ipv4StaticAddress.",
		"DHCPv4Client":         "dhcpv4Client.",
		"DSLiteTunnel":         "dsLiteTunnel.",
		"PPPoESession":         "pppoeSession.",
		"FirewallPolicy":       "firewallPolicy.",
		"FirewallZone":         "firewallZone.",
		"FirewallRule":         "firewallRule.",
		"IPAddressSet":         "ipAddressSet.",
		"LocalServiceRedirect": "localServiceRedirect.",
	}
	if prefix := prefixes[kind]; prefix != "" {
		return prefix + name + "."
	}
	return strings.ToLower(kind[:1]) + kind[1:] + "." + name + "."
}

func prefixedState(store routerstate.Store, prefix string) map[string]any {
	out := map[string]any{}
	for key, value := range store.Variables() {
		if strings.HasPrefix(key, prefix) {
			out[strings.TrimPrefix(key, prefix)] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stateString(store routerstate.Store, key string) string {
	value := store.Get(key)
	if value.Status != routerstate.StatusSet {
		return ""
	}
	return value.Value
}

func flattenAny(value any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	flattenValue("", decoded, out)
	return out
}

func flattenValue(prefix string, value any, out map[string]any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			flattenValue(next, child, out)
		}
	case []any:
		out[prefix] = typed
	default:
		if prefix != "" {
			out[prefix] = typed
		}
	}
}
