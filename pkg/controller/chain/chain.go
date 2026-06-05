// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/conntrack"
	bfdcontroller "github.com/imksoo/routerd/pkg/controller/bfd"
	bgpcontroller "github.com/imksoo/routerd/pkg/controller/bgp"
	"github.com/imksoo/routerd/pkg/controller/conntrackobserver"
	dhcpv4client "github.com/imksoo/routerd/pkg/controller/dhcpv4client"
	dnsresolvercontroller "github.com/imksoo/routerd/pkg/controller/dnsresolver"
	eventfederationcontroller "github.com/imksoo/routerd/pkg/controller/eventfederation"
	eventsubscriptioncontroller "github.com/imksoo/routerd/pkg/controller/eventsubscription"
	firewallcontroller "github.com/imksoo/routerd/pkg/controller/firewall"
	"github.com/imksoo/routerd/pkg/controller/framework"
	ingressservicecontroller "github.com/imksoo/routerd/pkg/controller/ingressservice"
	mobilitycontroller "github.com/imksoo/routerd/pkg/controller/mobility"
	"github.com/imksoo/routerd/pkg/controller/nat44"
	"github.com/imksoo/routerd/pkg/controller/pppoesession"
	provideractioncontroller "github.com/imksoo/routerd/pkg/controller/provideraction"
	vrrpcontroller "github.com/imksoo/routerd/pkg/controller/vrrp"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/derived"
	"github.com/imksoo/routerd/pkg/egressroute"
	"github.com/imksoo/routerd/pkg/eventrule"
	"github.com/imksoo/routerd/pkg/ha"
	"github.com/imksoo/routerd/pkg/healthcheck"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observabilitypipeline"
	"github.com/imksoo/routerd/pkg/platform"
	provideraction "github.com/imksoo/routerd/pkg/provideraction"
	"github.com/imksoo/routerd/pkg/providerinventory"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/resourcequery"
	daemonsource "github.com/imksoo/routerd/pkg/source/daemon"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

var dnsmasqMu sync.Mutex

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type eventedStore struct {
	Store Store
	Bus   *bus.Bus
}

// eventSubscriptionStore composes the evented status store (ownership + bus
// publication on status changes) with the raw SQLite data store that the
// EventSubscriptionController needs for federation events, subscription runs,
// dynamic config parts, and plugin runs.
type eventSubscriptionStore struct {
	evented eventedStore
	data    eventsubscriptioncontroller.DataStore
}

type mobilityDataStore interface {
	ListFederationEvents(group string, includeExpired bool, now int64) ([]routerstate.EventRecord, error)
	RecordFederationEvent(routerstate.EventRecord) error
	UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord) error
	GetDynamicConfigPartsBySource(source string) ([]routerstate.DynamicConfigPartRecord, error)
	ListActions(routerstate.ActionExecutionFilter) ([]routerstate.ActionExecutionRecord, error)
}

type mobilityStore struct {
	evented eventedStore
	data    mobilityDataStore
}

func (s mobilityStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	return s.evented.SaveObjectStatus(apiVersion, kind, name, status)
}

func (s mobilityStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	return s.evented.ObjectStatus(apiVersion, kind, name)
}

func (s mobilityStore) ListFederationEvents(group string, includeExpired bool, now int64) ([]routerstate.EventRecord, error) {
	return s.data.ListFederationEvents(group, includeExpired, now)
}

func (s mobilityStore) RecordFederationEvent(rec routerstate.EventRecord) error {
	return s.data.RecordFederationEvent(rec)
}

func (s mobilityStore) UpsertDynamicConfigPart(rec routerstate.DynamicConfigPartRecord) error {
	return s.data.UpsertDynamicConfigPart(rec)
}

func (s mobilityStore) GetDynamicConfigPartsBySource(source string) ([]routerstate.DynamicConfigPartRecord, error) {
	return s.data.GetDynamicConfigPartsBySource(source)
}

func (s mobilityStore) ListActions(filter routerstate.ActionExecutionFilter) ([]routerstate.ActionExecutionRecord, error) {
	return s.data.ListActions(filter)
}

func (s eventSubscriptionStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	return s.evented.SaveObjectStatus(apiVersion, kind, name, status)
}

func (s eventSubscriptionStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	return s.evented.ObjectStatus(apiVersion, kind, name)
}

func (s eventSubscriptionStore) ListFederationEvents(group string, includeExpired bool, now int64) ([]routerstate.EventRecord, error) {
	return s.data.ListFederationEvents(group, includeExpired, now)
}

func (s eventSubscriptionStore) SubscriptionRunStatus(subscription, eventID string) (string, int, bool, error) {
	return s.data.SubscriptionRunStatus(subscription, eventID)
}

func (s eventSubscriptionStore) UpsertSubscriptionRunStart(subscription, eventID, eventGroup, plugin string) error {
	return s.data.UpsertSubscriptionRunStart(subscription, eventID, eventGroup, plugin)
}

func (s eventSubscriptionStore) MarkSubscriptionRunResult(subscription, eventID, status, dynamicSource string, dynamicGeneration int64, errMsg string) error {
	return s.data.MarkSubscriptionRunResult(subscription, eventID, status, dynamicSource, dynamicGeneration, errMsg)
}

func (s eventSubscriptionStore) UpsertDynamicConfigPart(part routerstate.DynamicConfigPartRecord) error {
	return s.data.UpsertDynamicConfigPart(part)
}

func (s eventSubscriptionStore) RecordPluginRun(run routerstate.PluginRunRecord) (int64, error) {
	return s.data.RecordPluginRun(run)
}

func (s eventSubscriptionStore) CompletePluginRun(id int64, completedAt time.Time, exitCode *int, status, stdoutDigest, stderrText, runError string) error {
	return s.data.CompletePluginRun(id, completedAt, exitCode, status, stdoutDigest, stderrText, runError)
}

func (s eventedStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	if s.Store == nil {
		return nil
	}
	status = statusWithOwnership(apiVersion, kind, status)
	current := s.Store.ObjectStatus(apiVersion, kind, name)
	if newerStatus(current, status) {
		return nil
	}
	publishChanged := statusChangedForEvent(apiVersion, kind, current, status)
	if err := s.Store.SaveObjectStatus(apiVersion, kind, name, status); err != nil {
		return err
	}
	if publishChanged && s.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "store"}, "routerd.resource.status.changed", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: apiVersion, Kind: kind, Name: name}
		event.Attributes = map[string]string{
			"phase":         fmt.Sprint(status["phase"]),
			"previousPhase": fmt.Sprint(current["phase"]),
			"changedFields": strings.Join(statusChangedFieldsForEvent(apiVersion, kind, current, status), ","),
		}
		return s.Bus.Publish(context.Background(), event)
	}
	return nil
}

func statusWithOwnership(_ string, kind string, status map[string]any) map[string]any {
	out := make(map[string]any, len(status)+3)
	for key, value := range status {
		out[key] = value
	}
	if _, ok := out["owner"]; !ok {
		if owner := resourceOwnerController(kind); owner != "" {
			out["owner"] = owner
		}
	}
	if _, ok := out["managedBy"]; !ok {
		if managed, ok := statusBool(out["managed"]); ok && !managed {
			out["managedBy"] = "external"
		} else {
			out["managedBy"] = "routerd"
		}
	}
	if _, ok := out["management"]; !ok {
		switch strings.ToLower(strings.TrimSpace(fmt.Sprint(out["managedBy"]))) {
		case "", "routerd":
			out["management"] = "managed"
		case "external":
			out["management"] = "adopted"
		default:
			if managed, ok := statusBool(out["managed"]); ok && !managed {
				out["management"] = "adopted"
			} else {
				out["management"] = "managed"
			}
		}
	}
	return out
}

func statusBool(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "yes", "1":
			return true, true
		case "false", "no", "0":
			return false, true
		}
	}
	return false, false
}

func resourceOwnerController(kind string) string {
	switch kind {
	case "IPv4StaticAddress", "IPv6DelegatedAddress", "IPv6RAAddress", "Interface":
		return "address"
	case "VirtualAddress":
		return "vrrp"
	case "BGPRouter", "BGPPeer":
		return "bgp"
	case "BFD":
		return "bfd"
	case "DHCPv4Client":
		return "dhcpv4client"
	case "DHCPv4Server", "DHCPv6Server", "DHCPv6Information", "IPv6RouterAdvertisement":
		return "dhcpv6"
	case "DNSResolver", "DNSForwarder", "DNSUpstream", "DNSZone":
		return "dns-resolver"
	case "DSLiteTunnel":
		return "dslite"
	case "TunnelInterface":
		return "tunnel"
	case "WireGuardInterface", "WireGuardPeer":
		return "wireguard"
	case "FirewallZone", "FirewallPolicy", "FirewallRule", "ClientPolicy":
		return "firewall"
	case "NAT44Rule":
		return "nat"
	case "IPAddressSet", "LocalServiceRedirect":
		return "ip-address-set"
	case "NetworkAdoption":
		return "network-adoption"
	case "Package", "KernelModule":
		return "package"
	case "PPPoESession":
		return "pppoesession"
	case "IPv4Route", "IPv4StaticRoute", "IPv6StaticRoute", "EgressRoutePolicy", "HybridRoute", "AddressMobilityDomain", "RemoteAddressClaim":
		return "route"
	case "ServiceUnit", "TailscaleNode", "HealthCheck", "NTPClient", "NTPServer", "SysctlProfile", "Sysctl", "LogRetention", "Hostname", "ConntrackTuning":
		return "service-unit"
	case "ConntrackObserver", "TrafficFlowLog":
		return "conntrack"
	default:
		return ""
	}
}

func (s eventedStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if s.Store == nil {
		return nil
	}
	return s.Store.ObjectStatus(apiVersion, kind, name)
}

func (s eventedStore) Get(name string) routerstate.Value {
	store, ok := s.Store.(routerstate.Store)
	if !ok {
		now := time.Now().UTC()
		return routerstate.Value{Status: routerstate.StatusUnknown, Since: now, UpdatedAt: now}
	}
	return store.Get(name)
}

func (s eventedStore) Set(name, value, reason string) routerstate.Value {
	store, ok := s.Store.(routerstate.Store)
	if !ok {
		now := time.Now().UTC()
		return routerstate.Value{Status: routerstate.StatusUnknown, Value: value, Reason: reason, Since: now, UpdatedAt: now}
	}
	return store.Set(name, value, reason)
}

func (s eventedStore) Unset(name, reason string) routerstate.Value {
	store, ok := s.Store.(routerstate.Store)
	if !ok {
		now := time.Now().UTC()
		return routerstate.Value{Status: routerstate.StatusUnknown, Reason: reason, Since: now, UpdatedAt: now}
	}
	return store.Unset(name, reason)
}

func (s eventedStore) Forget(name, reason string) routerstate.Value {
	store, ok := s.Store.(routerstate.Store)
	if !ok {
		now := time.Now().UTC()
		return routerstate.Value{Status: routerstate.StatusUnknown, Reason: reason, Since: now, UpdatedAt: now}
	}
	return store.Forget(name, reason)
}

func (s eventedStore) Delete(name string) {
	store, ok := s.Store.(routerstate.Store)
	if ok {
		store.Delete(name)
	}
}

func (s eventedStore) Age(name string) time.Duration {
	store, ok := s.Store.(routerstate.Store)
	if !ok {
		return 0
	}
	return store.Age(name)
}

func (s eventedStore) Now() time.Time {
	store, ok := s.Store.(routerstate.Store)
	if !ok {
		return time.Now().UTC()
	}
	return store.Now()
}

func (s eventedStore) Save(path string) error {
	store, ok := s.Store.(routerstate.Store)
	if !ok {
		return nil
	}
	return store.Save(path)
}

func (s eventedStore) Variables() map[string]routerstate.Value {
	store, ok := s.Store.(routerstate.Store)
	if !ok {
		return nil
	}
	return store.Variables()
}

func (s eventedStore) ListObjectStatuses() ([]routerstate.ObjectStatus, error) {
	if s.Store == nil {
		return nil, nil
	}
	lister, ok := s.Store.(interface {
		ListObjectStatuses() ([]routerstate.ObjectStatus, error)
	})
	if !ok {
		return nil, nil
	}
	return lister.ListObjectStatuses()
}

func (s eventedStore) DeleteObject(apiVersion, kind, name string) error {
	if s.Store == nil {
		return nil
	}
	deleter, ok := s.Store.(interface {
		DeleteObject(apiVersion, kind, name string) error
	})
	if !ok {
		return nil
	}
	return deleter.DeleteObject(apiVersion, kind, name)
}

func newerStatus(current, next map[string]any) bool {
	currentTime, currentOK := comparableStatusTime(current)
	nextTime, nextOK := comparableStatusTime(next)
	return currentOK && nextOK && currentTime.After(nextTime)
}

func comparableStatusTime(status map[string]any) (time.Time, bool) {
	for _, key := range []string{"lastCheckedAt", "updatedAt", "observedAt"} {
		if parsed, ok := parseStatusTimestamp(status[key]); ok {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func parseStatusTimestamp(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func statusChanged(current, next map[string]any) bool {
	if len(current) == 0 && len(next) == 0 {
		return false
	}
	currentData, currentErr := json.Marshal(stableStatus(current))
	nextData, nextErr := json.Marshal(stableStatus(next))
	if currentErr == nil && nextErr == nil {
		return !bytes.Equal(currentData, nextData)
	}
	return !reflect.DeepEqual(stableStatus(current), stableStatus(next))
}

func objectStatusChanged(kind string, current, next map[string]any) bool {
	return statusChanged(current, statusWithOwnership("", kind, next))
}

func statusChangedFields(current, next map[string]any) []string {
	currentStable := stableStatus(current)
	nextStable := stableStatus(next)
	return changedFields(currentStable, nextStable)
}

func changedFields(currentStable, nextStable map[string]any) []string {
	keys := map[string]bool{}
	for key := range currentStable {
		keys[key] = true
	}
	for key := range nextStable {
		keys[key] = true
	}
	var out []string
	for key := range keys {
		if !reflect.DeepEqual(currentStable[key], nextStable[key]) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func statusChangedForEvent(apiVersion, kind string, current, next map[string]any) bool {
	currentStable := statusForEvent(apiVersion, kind, current)
	nextStable := statusForEvent(apiVersion, kind, next)
	currentData, currentErr := json.Marshal(currentStable)
	nextData, nextErr := json.Marshal(nextStable)
	if currentErr == nil && nextErr == nil {
		return !bytes.Equal(currentData, nextData)
	}
	return !reflect.DeepEqual(currentStable, nextStable)
}

func statusChangedFieldsForEvent(apiVersion, kind string, current, next map[string]any) []string {
	return changedFields(statusForEvent(apiVersion, kind, current), statusForEvent(apiVersion, kind, next))
}

func statusForEvent(apiVersion, kind string, status map[string]any) map[string]any {
	stable := stableStatus(status)
	if apiVersion != api.MobilityAPIVersion || kind != "MobilityPool" {
		return stable
	}
	out := make(map[string]any, len(stable))
	for key, value := range stable {
		if mobilityStatusEventVolatileField(key) {
			continue
		}
		out[key] = value
	}
	return out
}

func mobilityStatusEventVolatileField(key string) bool {
	switch key {
	case "plannedAt", "projectedAt", "dynamicExpiresAt":
		return true
	default:
		return false
	}
}

func stableStatus(status map[string]any) map[string]any {
	if status == nil {
		return nil
	}
	out := map[string]any{}
	for key, value := range status {
		switch key {
		case "updatedAt", "observedAt", "installedAt", "lastCheckedAt", "lastTransitionAt", "consecutivePassed", "consecutiveFailed", "createdHint", "packetRing", "conditions", "mtuObservedAt":
			continue
		case "handshakeAgeSeconds", "latestHandshake", "transferRxBytes", "transferTxBytes", "peers", "internalHoles":
			continue
		case "activeFlows", "count", "max", "usageRatio":
			if fmt.Sprint(status["phase"]) == "Observed" {
				continue
			}
		default:
			out[key] = stableStatusValue(value)
		}
	}
	return out
}

func stableStatusValue(value any) any {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int8:
		return int64(typed)
	case int16:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case uint:
		return uint64(typed)
	case uint8:
		return uint64(typed)
	case uint16:
		return uint64(typed)
	case uint32:
		return uint64(typed)
	case uint64:
		return typed
	case float32:
		return stableFloat64(float64(typed))
	case float64:
		return stableFloat64(typed)
	case []any:
		if typed == nil {
			return nil
		}
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, stableStatusValue(item))
		}
		return out
	case []string:
		if typed == nil {
			return nil
		}
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if volatileNestedStatusField(key) {
				continue
			}
			out[key] = stableStatusValue(item)
		}
		return out
	default:
		if normalized, ok := stableJSONValue(value); ok {
			return stableStatusValue(normalized)
		}
		return value
	}
}

func volatileNestedStatusField(key string) bool {
	switch key {
	case "healthyCount", "unhealthyCount", "observedAt", "updatedAt", "lastCheckedAt", "lastTransitionAt", "lastHealthyAt", "lastUnhealthyAt":
		return true
	default:
		return false
	}
}

func stableJSONValue(value any) (any, bool) {
	if value == nil {
		return nil, false
	}
	switch reflect.TypeOf(value).Kind() {
	case reflect.Struct, reflect.Slice, reflect.Array:
	default:
		return nil, false
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, false
	}
	return out, true
}

func stableFloat64(value float64) any {
	if value == float64(int64(value)) {
		return int64(value)
	}
	return value
}

func statusSubscriptions(kinds ...string) []bus.Subscription {
	allowed := map[string]bool{}
	for _, kind := range kinds {
		allowed[kind] = true
	}
	return []bus.Subscription{{
		Topics: []string{"routerd.resource.status.changed", "routerd.controller.bootstrap"},
		Filter: func(event daemonapi.DaemonEvent) bool {
			if event.Type == "routerd.controller.bootstrap" {
				return true
			}
			if event.Resource == nil {
				return false
			}
			return allowed[event.Resource.Kind]
		},
	}}
}

func bootstrapSubscriptions() []bus.Subscription {
	return []bus.Subscription{{Topics: []string{"routerd.controller.bootstrap"}}}
}

func ipv4RouteStatusSubscriptions() []bus.Subscription {
	return statusSubscriptions("DSLiteTunnel", "TunnelInterface", "EgressRoutePolicy", "VirtualAddress")
}

func hybridRouteStatusSubscriptions() []bus.Subscription {
	return statusSubscriptions("IPv4Route", "HealthCheck", "WireGuardInterface", "TunnelInterface", "Interface", "VirtualAddress")
}

func samStatusSubscriptions() []bus.Subscription {
	return statusSubscriptions("IPv4Route", "Sysctl", "WireGuardInterface", "TunnelInterface", "Interface", "VirtualAddress")
}

func becamePhase(event daemonapi.DaemonEvent, phase string) bool {
	if event.Resource == nil {
		return false
	}
	if event.Attributes["phase"] != phase {
		return false
	}
	previous := event.Attributes["previousPhase"]
	return previous == "" || previous != phase
}

type commandFunc func(ctx context.Context, name string, args ...string) error

type Options struct {
	DaemonSockets           map[string]string
	DryRunAddress           bool
	DryRunDSLite            bool
	DryRunRoute             bool
	DryRunDHCPv6            bool
	DryRunDHCPv4Client      bool
	DryRunPPPoESession      bool
	DryRunDNSResolver       bool
	DryRunEventFederation   bool
	DryRunEventSubscription bool
	DryRunLeaseSync         bool
	DryRunNAT44SessionSync  bool
	DryRunProviderAction    bool
	DryRunNAT               bool
	DryRunIngress           bool
	DryRunFirewall          bool
	DryRunBGP               bool
	DryRunVRRP              bool
	DryRunPackage           bool
	DryRunNetworkAdoption   bool
	DryRunServiceUnit       bool
	SuperviseClientDaemons  bool
	FirewallDisabled        bool
	DnsmasqCommand          string
	DnsmasqConfig           string
	DnsmasqPID              string
	DnsmasqPort             int
	DnsmasqListen           []string
	NftablesPath            string
	FirewallPath            string
	LedgerPath              string
	NftCommand              string
	BGPSocketPath           string
	BGPControlSocketPath    string
	BGPStatePath            string
	ConntrackInterval       time.Duration
	Logger                  *slog.Logger
	ControllerObserver      framework.Observer
	EnabledControllers      []string
	ProviderActionRunner    provideraction.ExecutorRunner
	ProviderInventoryRunner providerinventory.Runner
}

type Runner struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	Opts   Options
}

func (r *Runner) effectiveRouter(store eventedStore) *api.Router {
	return resourcequery.FilterRouterByWhen(r.Router, store)
}

func (r *Runner) saveWhenFalseStatuses(store eventedStore) error {
	if r.Router == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, res := range r.Router.Spec.Resources {
		when := resourcequery.ResourceWhen(res)
		if !resourcequery.ResourceWhenPresent(when) || resourcequery.ResourceWhenMatches(when, store) {
			continue
		}
		apiVersion := res.APIVersion
		if apiVersion == "" {
			apiVersion = resourcequery.APIVersionForKind(res.Kind)
		}
		if err := store.SaveObjectStatus(apiVersion, res.Kind, res.Metadata.Name, map[string]any{
			"phase":      "Pending",
			"reason":     "WhenFalse",
			"observedAt": now,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) effectiveRouterForReconcile(store eventedStore) (*api.Router, error) {
	if err := r.saveWhenFalseStatuses(store); err != nil {
		return nil, err
	}
	return r.effectiveRouter(store), nil
}

func (r *Runner) Start(ctx context.Context) error {
	if r.Router == nil || r.Bus == nil || r.Store == nil {
		return fmt.Errorf("router, bus, and store are required")
	}
	logger := r.Opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if r.Opts.SuperviseClientDaemons && r.controllerEnabled("daemon-supervisor") {
		r.superviseClientDaemons(ctx, logger)
	}
	for _, resource := range r.Router.Spec.Resources {
		if resource.Kind != "DHCPv6PrefixDelegation" {
			continue
		}
		name := resource.Metadata.Name
		socket := r.Opts.DaemonSockets[name]
		if socket == "" {
			defaults, _ := platform.Current()
			socket = filepath.Join(defaults.RuntimeDir, "dhcpv6-client", name+".sock")
		}
		source := daemonsource.DaemonSource{
			Daemon:    daemonapi.DaemonRef{Name: "routerd-dhcpv6-client-" + name, Kind: "routerd-dhcpv6-client", Instance: name},
			Socket:    socket,
			Publisher: r.Bus,
		}
		go func() {
			if err := source.Run(ctx); err != nil && ctx.Err() == nil {
				logger.Warn("daemon source stopped", "resource", name, "error", err)
			}
		}()
	}
	for _, resource := range r.Router.Spec.Resources {
		if resource.Kind != "DHCPv4Client" {
			continue
		}
		name := resource.Metadata.Name
		socket := r.Opts.DaemonSockets[name]
		if socket == "" {
			defaults, _ := platform.Current()
			socket = filepath.Join(defaults.RuntimeDir, "dhcpv4-client", name+".sock")
		}
		source := daemonsource.DaemonSource{
			Daemon:    daemonapi.DaemonRef{Name: "routerd-dhcpv4-client-" + name, Kind: "routerd-dhcpv4-client", Instance: name},
			Socket:    socket,
			Publisher: r.Bus,
		}
		go func() {
			if err := source.Run(ctx); err != nil && ctx.Err() == nil {
				logger.Warn("dhcpv4 daemon source stopped", "resource", name, "error", err)
			}
		}()
	}
	for _, resource := range r.Router.Spec.Resources {
		if resource.Kind != "HealthCheck" {
			continue
		}
		spec, err := resource.HealthCheckSpec()
		if err != nil || healthCheckDisabled(spec) {
			continue
		}
		name := resource.Metadata.Name
		defaults, _ := platform.Current()
		socket := filepath.Join(defaults.RuntimeDir, "healthcheck", name+".sock")
		source := daemonsource.DaemonSource{
			Daemon:    daemonapi.DaemonRef{Name: "routerd-healthcheck-" + name, Kind: "routerd-healthcheck", Instance: name},
			Socket:    socket,
			Publisher: r.Bus,
		}
		go func() {
			if err := source.Run(ctx); err != nil && ctx.Err() == nil {
				logger.Warn("healthcheck daemon source stopped", "resource", name, "error", err)
			}
		}()
	}
	for _, resource := range r.Router.Spec.Resources {
		if resource.Kind != "PPPoESession" {
			continue
		}
		_, err := resource.PPPoESessionSpec()
		if err != nil {
			continue
		}
		name := resource.Metadata.Name
		socket := r.Opts.DaemonSockets[name]
		if socket == "" {
			socket = filepath.Join("/run/routerd/pppoe-client", name+".sock")
		}
		source := daemonsource.DaemonSource{
			Daemon:    daemonapi.DaemonRef{Name: "routerd-pppoe-client-" + name, Kind: "routerd-pppoe-client", Instance: name},
			Socket:    socket,
			Publisher: r.Bus,
		}
		go func() {
			if err := source.Run(ctx); err != nil && ctx.Err() == nil {
				logger.Warn("pppoe daemon source stopped", "resource", name, "error", err)
			}
		}()
	}

	store := eventedStore{Store: r.Store, Bus: r.Bus}
	haDecision, err := acquireClusterLease(ctx, r.Router, store)
	if err != nil {
		return err
	}
	if haDecision.Enabled && haDecision.Leader && haDecision.Lease != nil {
		go haDecision.Lease.Heartbeat(ctx, func(err error) {
			logger.Warn("routerd cluster lease heartbeat failed", "error", err)
		})
		defer haDecision.Lease.Close()
	}
	opts := r.Opts
	if haDecision.Enabled && !haDecision.Leader {
		logger.Info("routerd cluster standby mode; mutating controllers run dry-run", "holder", haDecision.Holder, "leasePath", haDecision.LeasePath)
		opts.DryRunAddress = true
		opts.DryRunDSLite = true
		opts.DryRunRoute = true
		opts.DryRunDHCPv6 = true
		opts.DryRunDHCPv4Client = true
		opts.DryRunPPPoESession = true
		opts.DryRunDNSResolver = true
		opts.DryRunEventFederation = true
		opts.DryRunEventSubscription = true
		opts.DryRunLeaseSync = true
		opts.DryRunNAT44SessionSync = true
		opts.DryRunProviderAction = true
		opts.DryRunNAT = true
		opts.DryRunIngress = true
		opts.DryRunFirewall = true
		opts.DryRunBGP = true
		opts.DryRunVRRP = true
		opts.DryRunPackage = true
		opts.DryRunNetworkAdoption = true
		opts.DryRunServiceUnit = true
	}
	if platform.IsNixOSHost() {
		opts.DryRunServiceUnit = true
	}
	r.Opts = opts
	packages := PackageController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunPackage}
	sysctl := SysctlController{Router: r.Router, Bus: r.Bus, Store: store}
	kernelModules := KernelModuleController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunPackage}
	adoption := NetworkAdoptionController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunNetworkAdoption}
	serviceUnits := SystemdUnitController{Router: r.Router, DeclaredRouter: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunServiceUnit, SynthesizeClientDaemonUnits: !r.Opts.SuperviseClientDaemons}
	logRetention := LogRetentionController{Router: r.Router, Bus: r.Bus, Store: store}
	ntpClient := NTPClientController{Router: r.Router, Bus: r.Bus, Store: store}
	ntpServer := NTPServerController{Router: r.Router, Bus: r.Bus, Store: store}
	info := DHCPv6InformationController{Router: r.Router, Bus: r.Bus, Store: store, DaemonSockets: r.Opts.DaemonSockets, Logger: logger}
	link := LinkController{Router: r.Router, Store: store, Logger: logger}
	tunnel := TunnelInterfaceController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunRoute, OS: platform.CurrentOS(), Logger: logger}
	wireGuard := WireGuardController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunRoute, Logger: logger}
	ipv4Static := IPv4StaticAddressController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunAddress, Logger: logger}
	lan := LANAddressController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunAddress, Logger: logger}
	dslite := DSLiteTunnelController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunDSLite, ResolverPort: r.Opts.DnsmasqPort, Logger: logger}
	route := IPv4RouteController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunRoute, Logger: logger}
	hybridRoute := HybridRouteController{Router: r.Router, EffectiveRouter: r.Router, Store: store}
	samController := SAMController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunRoute}
	policyRoute := IPv4PolicyRouteController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunRoute, NftCommand: r.Opts.NftCommand, LedgerPath: r.Opts.LedgerPath, Logger: logger}
	pathMTU := PathMTUController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunRoute, NftCommand: r.Opts.NftCommand}
	dhcpv6 := DHCPv6ServerController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunDHCPv6, Command: r.Opts.DnsmasqCommand, ConfigPath: r.Opts.DnsmasqConfig, PIDFile: r.Opts.DnsmasqPID, Port: r.Opts.DnsmasqPort, ListenAddresses: r.Opts.DnsmasqListen, Logger: logger}
	dhcp4Client := dhcpv4client.Controller{Router: r.Router, Bus: r.Bus, Store: store, DaemonSockets: r.Opts.DaemonSockets, DryRun: r.Opts.DryRunDHCPv4Client, Logger: logger}
	pppoeSession := pppoesession.Controller{Router: r.Router, Bus: r.Bus, Store: store, DaemonSockets: r.Opts.DaemonSockets, DryRun: r.Opts.DryRunPPPoESession, Logger: logger}
	defaults, _ := platform.Current()
	dnsResolver := dnsresolvercontroller.Controller{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunDNSResolver, RuntimeDir: defaults.RuntimeDir, StateDir: defaults.StateDir}
	eventFederation := eventfederationcontroller.Controller{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunEventFederation, RuntimeDir: defaults.RuntimeDir, StateDir: defaults.StateDir}
	leaseSync := FileSyncController{Router: r.Router, Store: store, DryRun: r.Opts.DryRunLeaseSync}
	nat44SessionSync := NAT44SessionSyncController{Router: r.Router, Store: store, DryRun: r.Opts.DryRunNAT44SessionSync}
	bgpDaemon := bgpcontroller.DefaultDaemonSpec()
	if strings.TrimSpace(r.Opts.BGPSocketPath) != "" {
		bgpDaemon.SocketPath = strings.TrimSpace(r.Opts.BGPSocketPath)
		if strings.TrimSpace(r.Opts.BGPControlSocketPath) == "" {
			bgpDaemon.ControlSocketPath = filepath.Join(filepath.Dir(bgpDaemon.SocketPath), "control.sock")
		}
	}
	if strings.TrimSpace(r.Opts.BGPControlSocketPath) != "" {
		bgpDaemon.ControlSocketPath = strings.TrimSpace(r.Opts.BGPControlSocketPath)
	}
	if strings.TrimSpace(r.Opts.BGPStatePath) != "" {
		bgpDaemon.StatePath = strings.TrimSpace(r.Opts.BGPStatePath)
	}
	// EventSubscriptionController needs the SQLite-backed federation/dynamic/
	// plugin methods in addition to status writes. The raw r.Store is the
	// *state.SQLiteStore; status writes are routed through the evented store so
	// they keep ownership annotation + bus publication parity with peers.
	var eventSubscription eventsubscriptioncontroller.Controller
	if rawStore, ok := r.Store.(eventsubscriptioncontroller.DataStore); ok {
		eventSubscription = eventsubscriptioncontroller.Controller{
			Router:     r.Router,
			Bus:        r.Bus,
			Store:      eventSubscriptionStore{evented: store, data: rawStore},
			DryRun:     r.Opts.DryRunEventSubscription,
			RuntimeDir: defaults.RuntimeDir,
			StateDir:   defaults.StateDir,
		}
	}
	var mobility mobilitycontroller.Controller
	var mobilityDiscovery mobilitycontroller.DiscoveryController
	if rawStore, ok := r.Store.(mobilityDataStore); ok {
		mobilityDiscovery = mobilitycontroller.DiscoveryController{
			Router: r.Router,
			Bus:    r.Bus,
			Store:  mobilityStore{evented: store, data: rawStore},
			Runner: r.Opts.ProviderInventoryRunner,
		}
		mobility = mobilitycontroller.Controller{
			Router:   r.Router,
			Bus:      r.Bus,
			Store:    mobilityStore{evented: store, data: rawStore},
			BGPPaths: bgpdaemon.NewControlClient(bgpDaemon.ControlSocketPath),
		}
	}
	var providerAction provideractioncontroller.Controller
	if rawStore, ok := r.Store.(provideractioncontroller.Store); ok {
		providerAction = provideractioncontroller.Controller{
			Router: r.Router,
			Store:  rawStore,
			Runner: r.Opts.ProviderActionRunner,
			DryRun: r.Opts.DryRunProviderAction,
			Logger: logger,
		}
	}
	daemonStatusSync := DaemonStatusController{Router: r.Router, Bus: r.Bus, Store: store, DaemonSockets: r.Opts.DaemonSockets, Logger: logger}
	wan := egressroute.Controller{Router: r.Router, Bus: r.Bus, Store: store, Logger: logger}
	rules := eventrule.Controller{Router: r.Router, Bus: r.Bus, Store: store, Logger: logger}
	derivedEvents := derived.Controller{Router: r.Router, Bus: r.Bus, Store: store, Logger: logger}
	observabilityPipeline := observabilitypipeline.Controller{Router: r.Router, Bus: r.Bus, Store: store}
	health := healthcheck.Controller{Router: r.Router, Bus: r.Bus, Store: store, Logger: logger}
	nat := nat44.Controller{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunNAT, IngressLive: !r.Opts.DryRunIngress, NftablesPath: r.Opts.NftablesPath, NftCommand: r.Opts.NftCommand, Logger: logger}
	ingressService := ingressservicecontroller.Controller{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunIngress, Resolver: ingressServiceDNSResolver(r.Router, store), Logger: logger}
	bfd := bfdcontroller.Controller{Router: r.Router, Store: store, DryRun: r.Opts.DryRunBGP, RuntimeDir: defaults.RuntimeDir}
	bgp := bgpcontroller.Controller{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunBGP, Logger: logger, Daemon: bgpDaemon}
	vrrp := vrrpcontroller.Controller{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunVRRP, Logger: logger}
	ipAddressSet := IPAddressSetController{Router: r.Router, Store: store, DryRunNAT: r.Opts.DryRunNAT, DryRunRoute: r.Opts.DryRunRoute, DryRunFirewall: r.Opts.DryRunFirewall, NftCommand: r.Opts.NftCommand, RuntimeDir: defaults.RuntimeDir}
	firewall := firewallcontroller.Controller{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunFirewall, NftablesPath: firstNonEmpty(r.Opts.FirewallPath, "/run/routerd/firewall.nft"), NftCommand: r.Opts.NftCommand, Logger: logger}
	conntrackObs := conntrackobserver.Controller{Router: r.Router, Bus: r.Bus, Store: store, Paths: conntrack.DefaultPaths(), Interval: r.Opts.ConntrackInterval, Logger: logger}
	effectiveForReconcile := func() (*api.Router, error) {
		return r.effectiveRouterForReconcile(store)
	}
	if r.controllerEnabled("event-rule") {
		rules.Start(ctx)
	}
	if r.controllerEnabled("derived-event") {
		derivedEvents.Start(ctx)
	}
	if r.controllerEnabled("observability-pipeline") {
		effective, err := effectiveForReconcile()
		if err != nil {
			return err
		}
		observabilityPipeline.Router = effective
		if err := observabilityPipeline.Start(ctx); err != nil {
			return err
		}
	}
	if r.controllerEnabled("healthcheck") {
		health.Start(ctx)
	}
	if r.controllerEnabled("conntrack-observer") {
		conntrackObs.Start(ctx)
	}
	if r.controllerEnabled("bgp") {
		bgp.Start(ctx)
	}
	controllers := []framework.Controller{
		framework.FuncController{ControllerName: "observability-pipeline", Every: 30 * time.Second, PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			observabilityPipeline.Router = effective
			return observabilityPipeline.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "daemon-status", Every: 5 * time.Second, Subs: []bus.Subscription{{Topics: []string{"routerd.dhcpv6.client.**", "routerd.dhcpv4.client.**", "routerd.healthcheck.**", "routerd.pppoe.client.**"}}}, PeriodicFunc: daemonStatusSync.Reconcile},
		framework.FuncController{ControllerName: "dhcp-lease-sync", Every: 30 * time.Second, Subs: statusSubscriptions("DHCPv4ServerLeaseSync", "DHCPv6ServerLeaseSync", "DHCPv6PrefixDelegationLeaseSync", "VirtualAddress", "RouterdCluster"), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := leaseSync
			current.Router = effective
			return current.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "nat44-session-sync", Every: 30 * time.Second, Subs: statusSubscriptions("NAT44SessionSync", "NAT44Rule", "VirtualAddress", "RouterdCluster"), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := nat44SessionSync
			current.Router = effective
			return current.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "package", Every: 5 * time.Minute, PeriodicFunc: packages.Reconcile},
		framework.FuncController{ControllerName: "kernel-module", Every: 5 * time.Minute, PeriodicFunc: kernelModules.Reconcile},
		framework.FuncController{ControllerName: "sysctl", Every: 30 * time.Second, PeriodicFunc: sysctl.Reconcile},
		framework.FuncController{ControllerName: "network-adoption", Every: 5 * time.Minute, PeriodicFunc: adoption.Reconcile},
		framework.FuncController{ControllerName: "service-unit", Every: 5 * time.Minute, PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := serviceUnits
			current.Router = effective
			return current.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "log-retention", Every: time.Hour, PeriodicFunc: logRetention.Reconcile},
		framework.FuncController{ControllerName: "ntp-client", Every: 5 * time.Minute, Subs: statusSubscriptions("DHCPv4Client", "DHCPv6Information"), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := ntpClient
			current.Router = effective
			return current.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "ntp-server", Every: 5 * time.Minute, Subs: statusSubscriptions("DHCPv4Client", "DHCPv6Information", "IPv4StaticAddress", "IPv6DelegatedAddress"), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := ntpServer
			current.Router = effective
			return current.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "link", Every: 30 * time.Second, PeriodicFunc: link.Reconcile},
		framework.FuncController{ControllerName: "tunnel", Every: 30 * time.Second, Subs: statusSubscriptions("TunnelInterface"), PeriodicFunc: tunnel.Reconcile},
		framework.FuncController{ControllerName: "wireguard", Every: 30 * time.Second, Subs: statusSubscriptions("WireGuardInterface", "BGPRouter"), PeriodicFunc: wireGuard.Reconcile},
		framework.FuncController{ControllerName: "ipv4-static-address", Subs: statusSubscriptions("WireGuardInterface", "TunnelInterface"), PeriodicFunc: ipv4Static.Reconcile},
		framework.FuncController{ControllerName: "dhcpv6-information", Every: 30 * time.Second, Subs: statusSubscriptions("DHCPv6PrefixDelegation"), ReconcileFunc: func(ctx context.Context, event daemonapi.DaemonEvent) error {
			request := event.Type == "routerd.controller.bootstrap" || becamePhase(event, daemonapi.ResourcePhaseBound)
			for _, resource := range r.Router.Spec.Resources {
				if resource.Kind == "DHCPv6PrefixDelegation" {
					if err := info.reconcile(ctx, resource.Metadata.Name, request); err != nil {
						return err
					}
				}
			}
			return nil
		}},
		framework.FuncController{ControllerName: "lan-address", Every: 30 * time.Second, Subs: statusSubscriptions("DHCPv6PrefixDelegation", "Interface"), ReconcileFunc: func(ctx context.Context, _ daemonapi.DaemonEvent) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := lan
			current.Router = effective
			for _, resource := range effective.Spec.Resources {
				if resource.Kind == "DHCPv6PrefixDelegation" {
					if err := current.reconcile(ctx, resource.Metadata.Name); err != nil {
						return err
					}
				}
			}
			return nil
		}},
		framework.FuncController{ControllerName: "dslite", Every: 30 * time.Second, Subs: statusSubscriptions("DHCPv6Information", "IPv6DelegatedAddress", "DNSResolver"), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := dslite
			current.Router = effective
			return current.reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "ipv4-policy-route", Subs: statusSubscriptions("DSLiteTunnel", "HealthCheck", "IPv4StaticAddress", "Interface"), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := policyRoute
			current.Router = effective
			return current.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "ipv4-route", Every: 30 * time.Second, Subs: ipv4RouteStatusSubscriptions(), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			view, err := buildDynamicRouteSAMView(effective, r.Store, time.Now().UTC(), platform.CurrentOS())
			if err != nil {
				return err
			}
			current := route
			current.Router = view.RouteRouter
			return current.reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "hybrid-route", Subs: hybridRouteStatusSubscriptions(), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			view, err := buildDynamicRouteSAMView(effective, r.Store, time.Now().UTC(), platform.CurrentOS())
			if err != nil {
				return err
			}
			current := hybridRoute
			current.Router = view.EffectiveRouter
			current.EffectiveRouter = view.RouteRouter
			current.Lowerings = view.HybridLowerings
			return current.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "sam", Subs: samStatusSubscriptions(), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			view, err := buildDynamicRouteSAMView(effective, r.Store, time.Now().UTC(), platform.CurrentOS())
			if err != nil {
				return err
			}
			current := samController
			current.Router = view.EffectiveRouter
			current.Lowerings = view.SAMLowerings
			return current.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "path-mtu", Subs: statusSubscriptions("DSLiteTunnel", "PPPoESession", "WireGuardInterface", "TunnelInterface", "Interface", "FirewallZone", "DHCPv6Server", "IPv6RouterAdvertisement", "MobilityPool"), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			view, err := buildDynamicRouteSAMView(effective, r.Store, time.Now().UTC(), platform.CurrentOS())
			if err != nil {
				return err
			}
			current := pathMTU
			current.Router = view.EffectiveRouter
			return current.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "dhcpv6-server", Every: 30 * time.Second, Subs: []bus.Subscription{{Topics: []string{"routerd.resource.status.changed", "routerd.dhcp.lease.**"}}}, PeriodicFunc: func(ctx context.Context) error {
			if _, err := effectiveForReconcile(); err != nil {
				return err
			}
			return dhcpv6.reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "dhcpv4-lease", Every: 10 * time.Second, Subs: []bus.Subscription{{Topics: []string{"routerd.dhcpv4.client.**"}}}, ReconcileFunc: func(ctx context.Context, _ daemonapi.DaemonEvent) error {
			return dhcp4Client.ReconcileAll(ctx)
		}, PeriodicFunc: dhcp4Client.ReconcileAll},
		framework.FuncController{ControllerName: "pppoe-session", Subs: []bus.Subscription{{Topics: []string{"routerd.pppoe.client.**"}}}, ReconcileFunc: func(ctx context.Context, _ daemonapi.DaemonEvent) error {
			for _, resource := range r.Router.Spec.Resources {
				if resource.Kind == "PPPoESession" {
					if err := pppoeSession.Reconcile(ctx, resource.Metadata.Name); err != nil {
						return err
					}
				}
			}
			return nil
		}},
		framework.FuncController{ControllerName: "dns-resolver", Subs: []bus.Subscription{{Topics: []string{"routerd.resource.status.changed", "routerd.dhcp.lease.**"}}}, ReconcileFunc: func(ctx context.Context, event daemonapi.DaemonEvent) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := dnsResolver
			current.Router = effective
			return current.HandleEvent(ctx, event)
		}, PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := dnsResolver
			current.Router = effective
			return current.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "event-federation", Subs: []bus.Subscription{{Topics: []string{"routerd.resource.status.changed"}}}, ReconcileFunc: eventFederation.HandleEvent, PeriodicFunc: eventFederation.Reconcile},
		framework.FuncController{ControllerName: "event-subscription", Every: 5 * time.Second, Subs: []bus.Subscription{{Topics: []string{"routerd.resource.status.changed"}}}, PeriodicFunc: eventSubscription.Reconcile},
		framework.FuncController{ControllerName: "mobility-discovery", Every: 30 * time.Second, Subs: []bus.Subscription{{Topics: []string{"routerd.resource.status.changed"}}}, ReconcileFunc: mobilityDiscovery.HandleEvent, PeriodicFunc: mobilityDiscovery.Reconcile},
		framework.FuncController{ControllerName: "mobility", Every: 5 * time.Second, Subs: []bus.Subscription{{Topics: []string{"routerd.resource.status.changed"}}}, ReconcileFunc: mobility.HandleEvent, PeriodicFunc: mobility.Reconcile},
		framework.FuncController{ControllerName: "provider-action-execution", Every: 5 * time.Second, Subs: []bus.Subscription{{Topics: []string{"routerd.resource.status.changed"}}}, PeriodicFunc: providerAction.Reconcile},
		framework.FuncController{ControllerName: "egress-route-policy", Every: 15 * time.Second, Subs: statusSubscriptions("HealthCheck", "DSLiteTunnel", "Interface", "DHCPv4Client", "PPPoESession"), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := wan
			current.Router = effective
			return current.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "ingress-service", Every: 5 * time.Second, Subs: bootstrapSubscriptions(), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := ingressService
			current.Router = effective
			return current.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "nat44", Subs: statusSubscriptions("EgressRoutePolicy", "IngressService"), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := nat
			current.Router = effective
			return current.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "bfd", Every: time.Second, Subs: statusSubscriptions("BGPPeer", "BFD"), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := bfd
			current.Router = effective
			return current.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "bgp", Every: bgpcontroller.PollInterval(r.Router), Subs: statusSubscriptions("BFD", "BGPRouter", "BGPPeer"), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			bgp.Router = effective
			return bgp.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "vrrp", Every: 15 * time.Second, Subs: statusSubscriptions("BGPRouter", "BGPPeer", "IngressService"), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			vrrp.Router = effective
			return vrrp.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "ip-address-set", Every: 30 * time.Second, Subs: statusSubscriptions("IPAddressSet", "LocalServiceRedirect"), PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := ipAddressSet
			current.Router = effective
			return current.Reconcile(ctx)
		}},
	}
	if !r.Opts.FirewallDisabled {
		controllers = append(controllers, framework.FuncController{ControllerName: "firewall", Subs: []bus.Subscription{{Topics: []string{"routerd.resource.status.changed", "routerd.firewall.**"}}}, PeriodicFunc: func(ctx context.Context) error {
			effective, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := firewall
			current.Router = effective
			return current.Reconcile(ctx)
		}})
	}
	controllers = r.filterControllers(controllers)
	if r.controllerEnabled("daemon-status") {
		r.warmDaemonStatuses(ctx, daemonStatusSync, logger)
	}
	go func() {
		loop := framework.Runner{Bus: r.Bus, Logger: logger, Interval: 30 * time.Second, Observer: r.Opts.ControllerObserver}
		if err := loop.Run(ctx, controllers...); err != nil && ctx.Err() == nil {
			logger.Warn("controller event loop stopped", "error", err)
		}
	}()
	return nil
}

func (r *Runner) controllerEnabled(name string) bool {
	if len(r.Opts.EnabledControllers) == 0 {
		return true
	}
	for _, candidate := range r.Opts.EnabledControllers {
		if strings.TrimSpace(candidate) == name || strings.TrimSpace(candidate) == "all" {
			return true
		}
	}
	return false
}

func (r *Runner) filterControllers(controllers []framework.Controller) []framework.Controller {
	if len(r.Opts.EnabledControllers) == 0 {
		return controllers
	}
	out := make([]framework.Controller, 0, len(controllers))
	for _, controller := range controllers {
		if r.controllerEnabled(controller.Name()) {
			out = append(out, controller)
		}
	}
	return out
}

func acquireClusterLease(ctx context.Context, router *api.Router, store Store) (ha.Decision, error) {
	resource, spec, ok, err := routerdClusterResource(router)
	if err != nil || !ok {
		return ha.Decision{Leader: true}, err
	}
	ttl := 30 * time.Second
	if strings.TrimSpace(spec.LeaseTTL) != "" {
		ttl, _ = time.ParseDuration(spec.LeaseTTL)
	}
	decision, err := ha.Acquire(ctx, ha.Config{
		Name:      resource.Metadata.Name,
		Identity:  spec.Identity,
		Peers:     spec.Peers,
		LeasePath: spec.LeasePath,
		TTL:       ttl,
	})
	if err != nil {
		return decision, err
	}
	if store != nil {
		phase := "Standby"
		if decision.Leader {
			phase = "Leader"
		}
		_ = store.SaveObjectStatus(api.SystemAPIVersion, "RouterdCluster", resource.Metadata.Name, map[string]any{
			"phase":      phase,
			"identity":   decision.Identity,
			"holder":     decision.Holder,
			"leasePath":  decision.LeasePath,
			"expiresAt":  decision.ExpiresAt.Format(time.RFC3339Nano),
			"reason":     decision.Reason,
			"observedAt": time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	return decision, nil
}

func routerdClusterResource(router *api.Router) (api.Resource, api.RouterdClusterSpec, bool, error) {
	if router == nil {
		return api.Resource{}, api.RouterdClusterSpec{}, false, nil
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.SystemAPIVersion || resource.Kind != "RouterdCluster" {
			continue
		}
		spec, err := resource.RouterdClusterSpec()
		return resource, spec, true, err
	}
	return api.Resource{}, api.RouterdClusterSpec{}, false, nil
}

func ingressServiceDNSResolver(router *api.Router, store Store) *net.Resolver {
	endpoint, ok := dnsResolverEndpoint(router, store)
	if !ok {
		return nil
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			dialer := net.Dialer{}
			return dialer.DialContext(ctx, network, endpoint)
		},
	}
}

func dnsResolverEndpoint(router *api.Router, store Store) (string, bool) {
	if router == nil {
		return "", false
	}
	var candidates []string
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "DNSResolver" {
			continue
		}
		spec, err := resource.DNSResolverSpec()
		if err != nil {
			continue
		}
		for _, listen := range spec.Listen {
			port := listen.Port
			if port == 0 {
				port = 53
			}
			for _, address := range append([]string(nil), listen.Addresses...) {
				candidates = append(candidates, net.JoinHostPort(normalizeResolverAddress(address), strconv.Itoa(port)))
			}
			for _, source := range listen.AddressFrom {
				if address := normalizeResolverAddress(resourcequery.Value(store, source)); address != "" {
					candidates = append(candidates, net.JoinHostPort(address, strconv.Itoa(port)))
				}
			}
		}
	}
	for _, candidate := range candidates {
		host, _, err := net.SplitHostPort(candidate)
		if err == nil && (host == "127.0.0.1" || host == "::1") {
			return candidate, true
		}
	}
	if len(candidates) > 0 {
		return candidates[0], true
	}
	return "", false
}

func normalizeResolverAddress(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Addr().String()
	}
	return value
}

func (r *Runner) warmDaemonStatuses(ctx context.Context, controller DaemonStatusController, logger *slog.Logger) {
	warmCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := controller.Reconcile(warmCtx); err != nil && ctx.Err() == nil && logger != nil {
		logger.Warn("initial daemon status reconcile failed", "error", err)
	}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.controller.bootstrap", daemonapi.SeverityInfo)
	if err := r.Bus.Publish(ctx, event); err != nil && ctx.Err() == nil && logger != nil {
		logger.Warn("initial controller bootstrap event failed", "error", err)
	}
}

func (r *Runner) superviseClientDaemons(ctx context.Context, logger *slog.Logger) {
	for _, resource := range r.Router.Spec.Resources {
		switch resource.Kind {
		case "DHCPv6PrefixDelegation":
			spec, err := resource.DHCPv6PrefixDelegationSpec()
			if err != nil {
				continue
			}
			ifname := interfaceIfName(r.Router, spec.Interface)
			if ifname == "" {
				ifname = spec.Interface
			}
			defaults, _ := platform.Current()
			args := []string{"daemon", "--resource", resource.Metadata.Name, "--interface", ifname,
				"--socket", filepath.Join(defaults.RuntimeDir, "dhcpv6-client", resource.Metadata.Name+".sock"),
				"--lease-file", filepath.Join(defaults.StateDir, "dhcpv6-client", resource.Metadata.Name, "lease.json"),
				"--event-file", filepath.Join(defaults.StateDir, "dhcpv6-client", resource.Metadata.Name, "events.jsonl"),
			}
			if spec.IAID != "" {
				args = append(args, "--iaid", spec.IAID)
			}
			if spec.ClientDUID != "" {
				args = append(args, "--client-duid", spec.ClientDUID)
			}
			r.startSupervisedDaemon(ctx, logger, resource.Metadata.Name, "routerd-dhcpv6-client", args)
		case "DHCPv4Client":
			spec, err := resource.DHCPv4ClientSpec()
			if err != nil {
				continue
			}
			ifname := interfaceIfName(r.Router, spec.Interface)
			if ifname == "" {
				ifname = spec.Interface
			}
			defaults, _ := platform.Current()
			args := []string{"daemon", "--resource", resource.Metadata.Name, "--interface", ifname,
				"--socket", filepath.Join(defaults.RuntimeDir, "dhcpv4-client", resource.Metadata.Name+".sock"),
				"--lease-file", filepath.Join(defaults.StateDir, "dhcpv4-client", resource.Metadata.Name, "lease.json"),
				"--event-file", filepath.Join(defaults.StateDir, "dhcpv4-client", resource.Metadata.Name, "events.jsonl"),
			}
			if spec.Hostname != "" {
				args = append(args, "--hostname", spec.Hostname)
			}
			if spec.RequestedAddress != "" {
				args = append(args, "--requested-address", spec.RequestedAddress)
			}
			if spec.ClassID != "" {
				args = append(args, "--class-id", spec.ClassID)
			}
			if spec.ClientID != "" {
				args = append(args, "--client-id", spec.ClientID)
			}
			r.startSupervisedDaemon(ctx, logger, resource.Metadata.Name, "routerd-dhcpv4-client", args)
		case "PPPoESession":
			spec, err := resource.PPPoESessionSpec()
			if err != nil {
				continue
			}
			ifname := interfaceIfName(r.Router, spec.Interface)
			if ifname == "" {
				ifname = spec.Interface
			}
			args := []string{"daemon", "--resource", resource.Metadata.Name, "--interface", ifname, "--username", spec.Username}
			if spec.PasswordFile != "" {
				args = append(args, "--password-file", spec.PasswordFile)
			} else if spec.Password != "" {
				args = append(args, "--password", spec.Password)
			}
			if spec.AuthMethod != "" {
				args = append(args, "--auth-method", spec.AuthMethod)
			}
			if spec.MTU != 0 {
				args = append(args, "--mtu", fmt.Sprintf("%d", spec.MTU))
			}
			if spec.MRU != 0 {
				args = append(args, "--mru", fmt.Sprintf("%d", spec.MRU))
			}
			if spec.ServiceName != "" {
				args = append(args, "--service-name", spec.ServiceName)
			}
			if spec.ACName != "" {
				args = append(args, "--ac-name", spec.ACName)
			}
			if spec.LCPEchoInterval != 0 {
				args = append(args, "--lcp-echo-interval", fmt.Sprintf("%d", spec.LCPEchoInterval))
			}
			if spec.LCPEchoFailure != 0 {
				args = append(args, "--lcp-echo-failure", fmt.Sprintf("%d", spec.LCPEchoFailure))
			}
			r.startSupervisedDaemon(ctx, logger, resource.Metadata.Name, "routerd-pppoe-client", args)
		}
	}
}

func (r *Runner) startSupervisedDaemon(ctx context.Context, logger *slog.Logger, resourceName, binary string, args []string) {
	go func() {
		for ctx.Err() == nil {
			if clientSocketReady(defaultClientSocket(binary, resourceName)) {
				select {
				case <-time.After(10 * time.Second):
					continue
				case <-ctx.Done():
					return
				}
			}
			path := routerdClientBinary(binary)
			cmd := exec.CommandContext(ctx, path, args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if logger != nil {
				logger.Info("starting supervised routerd client daemon", "binary", path, "resource", resourceName)
			}
			err := cmd.Run()
			if ctx.Err() != nil {
				return
			}
			if logger != nil {
				logger.Warn("supervised routerd client daemon exited", "binary", path, "resource", resourceName, "error", err)
			}
			select {
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
				return
			}
		}
	}()
}

func routerdClientBinary(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), name)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	return filepath.Join("/usr/local/sbin", name)
}

func defaultClientSocket(binary, resource string) string {
	defaults, _ := platform.Current()
	switch binary {
	case "routerd-dhcpv6-client":
		return filepath.Join(defaults.RuntimeDir, "dhcpv6-client", resource+".sock")
	case "routerd-dhcpv4-client":
		return filepath.Join(defaults.RuntimeDir, "dhcpv4-client", resource+".sock")
	case "routerd-pppoe-client":
		return filepath.Join(defaults.RuntimeDir, "pppoe-client", resource+".sock")
	default:
		return ""
	}
}

func clientSocketReady(socket string) bool {
	if socket == "" {
		return false
	}
	conn, err := net.DialTimeout("unix", socket, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

type PrefixDelegationController struct {
	Router        *api.Router
	Bus           *bus.Bus
	Store         Store
	DaemonSockets map[string]string
	Logger        *slog.Logger
}

func (c PrefixDelegationController) Start(ctx context.Context) {
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.dhcpv6.client.prefix.*"}}, 32)
	go func() {
		for event := range ch {
			if event.Resource == nil || event.Resource.Kind != "DHCPv6PrefixDelegation" {
				continue
			}
			if err := c.reconcile(ctx, event); err != nil && c.Logger != nil {
				c.Logger.Warn("prefix delegation reconcile failed", "resource", event.Resource.Name, "error", err)
			}
		}
	}()
}

func (c PrefixDelegationController) reconcile(ctx context.Context, event daemonapi.DaemonEvent) error {
	status, err := daemonStatus(ctx, c.socketFor(event.Resource.Name))
	if err != nil {
		return err
	}
	for _, resource := range status.Resources {
		if resource.Resource.Kind != "DHCPv6PrefixDelegation" || resource.Resource.Name != event.Resource.Name {
			continue
		}
		next := map[string]any{
			"phase":      resource.Phase,
			"health":     resource.Health,
			"conditions": resource.Conditions,
			"observed":   resource.Observed,
		}
		if resource.Observed != nil {
			next["currentPrefix"] = resource.Observed["currentPrefix"]
			next["serverDUID"] = resource.Observed["serverDUID"]
		}
		return c.Store.SaveObjectStatus(resource.Resource.APIVersion, resource.Resource.Kind, resource.Resource.Name, next)
	}
	return fmt.Errorf("daemon status did not include DHCPv6PrefixDelegation/%s", event.Resource.Name)
}

func (c PrefixDelegationController) socketFor(resource string) string {
	if socket := c.DaemonSockets[resource]; socket != "" {
		return socket
	}
	return filepath.Join("/run/routerd/dhcpv6-client", resource+".sock")
}

type LANAddressController struct {
	Router         *api.Router
	Bus            *bus.Bus
	Store          Store
	DryRun         bool
	Logger         *slog.Logger
	Command        commandFunc
	AddressPresent func(context.Context, string, string) bool
}

type LinkController struct {
	Router *api.Router
	Store  Store
	Logger *slog.Logger
}

func (c LinkController) Reconcile(ctx context.Context) error {
	_ = ctx
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "Interface" {
			continue
		}
		spec, err := resource.InterfaceSpec()
		if err != nil {
			return err
		}
		ifname := spec.IfName
		status := map[string]any{
			"phase":   "Down",
			"ifname":  ifname,
			"managed": spec.Managed,
		}
		if ifname == "" {
			status["reason"] = "IfNameMissing"
		} else if ifi, err := net.InterfaceByName(ifname); err == nil {
			status["index"] = ifi.Index
			status["flags"] = ifi.Flags.String()
			addresses, ipv4, ipv6 := interfaceStatusAddresses(ifi)
			if len(addresses) > 0 {
				status["addresses"] = addresses
			}
			if len(ipv4) > 0 {
				status["ipv4Addresses"] = ipv4
				status["primaryIPv4"] = ipv4[0]
			}
			if len(ipv6) > 0 {
				status["ipv6Addresses"] = ipv6
				status["primaryIPv6"] = ipv6[0]
			}
			if ifi.Flags&net.FlagUp != 0 {
				status["phase"] = "Up"
			}
		} else {
			status["reason"] = "InterfaceNotFound"
			status["error"] = err.Error()
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "Interface", resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	return nil
}

func interfaceStatusAddresses(ifi *net.Interface) ([]string, []string, []string) {
	if ifi == nil {
		return nil, nil, nil
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return nil, nil, nil
	}
	var all []string
	var ipv4 []string
	var ipv6 []string
	for _, addr := range addrs {
		prefix, err := netip.ParsePrefix(addr.String())
		if err != nil {
			continue
		}
		if prefix.Addr().IsLinkLocalUnicast() {
			continue
		}
		all = append(all, prefix.String())
		if prefix.Addr().Is4() {
			ipv4 = append(ipv4, prefix.String())
		} else if prefix.Addr().Is6() {
			ipv6 = append(ipv6, prefix.String())
		}
	}
	sort.Strings(all)
	sort.Strings(ipv4)
	sort.Strings(ipv6)
	return all, ipv4, ipv6
}

type IPv4StaticAddressController struct {
	Router         *api.Router
	Bus            *bus.Bus
	Store          Store
	DryRun         bool
	Logger         *slog.Logger
	Command        commandFunc
	AddressPresent func(context.Context, string, string) bool
	DevicePresent  func(context.Context, string) bool
}

type DaemonStatusController struct {
	Router        *api.Router
	Bus           *bus.Bus
	Store         Store
	DaemonSockets map[string]string
	Logger        *slog.Logger
}

func (c DaemonStatusController) Start(ctx context.Context) {
	if c.Router == nil || c.Store == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			if err := c.Reconcile(ctx); err != nil && c.Logger != nil && ctx.Err() == nil {
				c.Logger.Warn("daemon status reconcile failed", "error", err)
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c DaemonStatusController) Reconcile(ctx context.Context) error {
	for _, socket := range c.daemonSockets() {
		statusCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		status, err := daemonStatus(statusCtx, socket)
		cancel()
		if err != nil {
			if c.Logger != nil && ctx.Err() == nil {
				c.Logger.Debug("daemon status snapshot skipped", "socket", socket, "error", err)
			}
			continue
		}
		for _, observed := range status.Resources {
			next := map[string]any{
				"phase":      observed.Phase,
				"health":     observed.Health,
				"conditions": observed.Conditions,
				"updatedAt":  time.Now().UTC().Format(time.RFC3339Nano),
			}
			for key, value := range observed.Observed {
				next[key] = value
			}
			if err := c.Store.SaveObjectStatus(observed.Resource.APIVersion, observed.Resource.Kind, observed.Resource.Name, next); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c DaemonStatusController) daemonSockets() []string {
	seen := map[string]bool{}
	var out []string
	add := func(socket string) {
		if socket == "" || seen[socket] {
			return
		}
		seen[socket] = true
		out = append(out, socket)
	}
	for _, resource := range c.Router.Spec.Resources {
		switch resource.Kind {
		case "DHCPv6PrefixDelegation":
			socket := c.DaemonSockets[resource.Metadata.Name]
			if socket == "" {
				defaults, _ := platform.Current()
				socket = filepath.Join(defaults.RuntimeDir, "dhcpv6-client", resource.Metadata.Name+".sock")
			}
			add(socket)
		case "DHCPv4Client":
			socket := c.DaemonSockets[resource.Metadata.Name]
			if socket == "" {
				defaults, _ := platform.Current()
				socket = filepath.Join(defaults.RuntimeDir, "dhcpv4-client", resource.Metadata.Name+".sock")
			}
			add(socket)
		case "HealthCheck":
			spec, err := resource.HealthCheckSpec()
			if err != nil || healthCheckDisabled(spec) {
				continue
			}
			defaults, _ := platform.Current()
			socket := filepath.Join(defaults.RuntimeDir, "healthcheck", resource.Metadata.Name+".sock")
			add(socket)
		case "PPPoESession":
			_, err := resource.PPPoESessionSpec()
			if err != nil {
				continue
			}
			socket := c.DaemonSockets[resource.Metadata.Name]
			if socket == "" {
				defaults, _ := platform.Current()
				socket = filepath.Join(defaults.RuntimeDir, "pppoe-client", resource.Metadata.Name+".sock")
			}
			add(socket)
		case "IPv6RouterAdvertisement":
			if _, err := resource.IPv6RouterAdvertisementSpec(); err != nil {
				continue
			}
			socket := c.DaemonSockets[resource.Metadata.Name]
			if socket == "" {
				defaults, _ := platform.Current()
				socket = filepath.Join(defaults.RuntimeDir, "ra-observer", resource.Metadata.Name+".sock")
			}
			add(socket)
		}
	}
	return out
}

func (c IPv4StaticAddressController) Reconcile(ctx context.Context) error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "IPv4StaticAddress" {
			continue
		}
		spec, err := resource.IPv4StaticAddressSpec()
		if err != nil {
			return err
		}
		ifname := interfaceIfName(c.Router, spec.Interface)
		if ifname == "" {
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", resource.Metadata.Name, map[string]any{
				"phase":     "Pending",
				"reason":    "InterfaceMissing",
				"interface": spec.Interface,
				"address":   spec.Address,
				"dryRun":    c.DryRun,
			}); err != nil {
				return err
			}
			continue
		}
		status := map[string]any{
			"phase":     "Applied",
			"interface": spec.Interface,
			"ifname":    ifname,
			"address":   spec.Address,
			"dryRun":    c.DryRun,
		}
		devicePresentFn := c.DevicePresent
		if devicePresentFn == nil {
			devicePresentFn = interfaceDevicePresent
		}
		if !c.DryRun && !devicePresentFn(ctx, ifname) {
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", resource.Metadata.Name, map[string]any{
				"phase":     "Pending",
				"reason":    "InterfaceNotPresent",
				"interface": spec.Interface,
				"ifname":    ifname,
				"address":   spec.Address,
				"dryRun":    c.DryRun,
			}); err != nil {
				return err
			}
			continue
		}
		changed := objectStatusChanged("IPv4StaticAddress", c.Store.ObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", resource.Metadata.Name), status)
		addressPresent := true
		if !c.DryRun {
			addressPresentFn := c.AddressPresent
			if addressPresentFn == nil {
				addressPresentFn = ipv4AddressPresent
			}
			addressPresent = addressPresentFn(ctx, ifname, spec.Address)
		}
		if !c.DryRun && (changed || !addressPresent) {
			command := c.Command
			if command == nil {
				command = runCommandContext
			}
			name, args := ipv4StaticAddressApplyCommand(platform.CurrentOS(), ifname, spec.Address)
			if err := command(ctx, name, args...); err != nil {
				if saveErr := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", resource.Metadata.Name, map[string]any{
					"phase":     "Error",
					"reason":    "ApplyFailed",
					"interface": spec.Interface,
					"ifname":    ifname,
					"address":   spec.Address,
					"error":     err.Error(),
					"dryRun":    c.DryRun,
				}); saveErr != nil {
					return saveErr
				}
				return err
			}
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", resource.Metadata.Name, status); err != nil {
			return err
		}
		if (changed || !addressPresent) && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.lan.ipv4_address.applied", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"address": spec.Address, "interface": spec.Interface, "ifname": ifname, "dryRun": fmt.Sprintf("%t", c.DryRun)}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func interfaceDevicePresent(ctx context.Context, ifname string) bool {
	if strings.TrimSpace(ifname) == "" {
		return false
	}
	if platform.CurrentOS() == platform.OSFreeBSD {
		return exec.CommandContext(ctx, "ifconfig", ifname).Run() == nil
	}
	return exec.CommandContext(ctx, "ip", "link", "show", "dev", ifname).Run() == nil
}

func ipv4AddressPresent(ctx context.Context, ifname, address string) bool {
	want := strings.TrimSpace(address)
	if host, _, ok := strings.Cut(want, "/"); ok {
		want = host
	}
	if platform.CurrentOS() == platform.OSFreeBSD {
		out, err := exec.CommandContext(ctx, "ifconfig", ifname).Output()
		if err != nil {
			return false
		}
		return ifconfigHasIPv4Address(out, address)
	}
	out, err := exec.CommandContext(ctx, "ip", "-4", "-o", "addr", "show", "dev", ifname).Output()
	if err != nil {
		return false
	}
	fields := strings.Fields(string(out))
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] != "inet" {
			continue
		}
		got := strings.TrimPrefix(fields[i+1], "addr:")
		if host, _, ok := strings.Cut(got, "/"); ok {
			got = host
		}
		if got == want {
			return true
		}
	}
	return false
}

func ipv4StaticAddressApplyCommand(osName platform.OS, ifname, address string) (string, []string) {
	if osName == platform.OSFreeBSD {
		return "ifconfig", []string{ifname, "inet", address, "alias"}
	}
	return "ip", []string{"-4", "addr", "replace", address, "dev", ifname}
}

func ifconfigHasIPv4Address(out []byte, address string) bool {
	want := strings.TrimSpace(address)
	if host, _, ok := strings.Cut(want, "/"); ok {
		want = host
	}
	fields := strings.Fields(string(out))
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "inet" && strings.TrimPrefix(fields[i+1], "addr:") == want {
			return true
		}
	}
	return false
}

func ipv6AddressPresent(ctx context.Context, ifname, address string) bool {
	if platform.CurrentOS() == platform.OSFreeBSD {
		out, err := exec.CommandContext(ctx, "ifconfig", ifname).Output()
		if err != nil {
			return false
		}
		return ifconfigHasIPv6Address(out, address)
	}
	out, err := exec.CommandContext(ctx, "ip", "-6", "-o", "addr", "show", "dev", ifname).Output()
	if err != nil {
		return false
	}
	return ipAddrShowHasIPv6Address(out, address)
}

func ipAddrShowHasIPv6Address(out []byte, address string) bool {
	want := localIPv6Address(address)
	if want == "" {
		return false
	}
	fields := strings.Fields(string(out))
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "inet6" && localIPv6Address(fields[i+1]) == want {
			return true
		}
	}
	return false
}

func ipv6StaticAddressApplyCommand(osName platform.OS, ifname, address string) (string, []string) {
	if osName == platform.OSFreeBSD {
		host := strings.TrimSpace(address)
		prefixLen := "64"
		if parsed, err := netip.ParsePrefix(host); err == nil {
			host = parsed.Addr().String()
			prefixLen = fmt.Sprintf("%d", parsed.Bits())
		} else if value, bits, ok := strings.Cut(host, "/"); ok {
			host = value
			prefixLen = bits
		}
		return "ifconfig", []string{ifname, "inet6", host, "prefixlen", prefixLen, "alias"}
	}
	return "ip", []string{"-6", "addr", "replace", address, "dev", ifname}
}

func runCommandContext(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

func (c LANAddressController) Start(ctx context.Context) {
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.dhcpv6.client.prefix.*"}}, 32)
	go func() {
		for event := range ch {
			if event.Resource == nil {
				continue
			}
			if err := c.reconcile(ctx, event.Resource.Name); err != nil && c.Logger != nil {
				c.Logger.Warn("lan address reconcile failed", "pd", event.Resource.Name, "error", err)
			}
		}
	}()
}

func (c LANAddressController) reconcile(ctx context.Context, pdName string) error {
	pdStatus := c.Store.ObjectStatus(api.NetAPIVersion, "DHCPv6PrefixDelegation", pdName)
	if pdStatus["phase"] != daemonapi.ResourcePhaseBound {
		return nil
	}
	prefix, _ := pdStatus["currentPrefix"].(string)
	if prefix == "" {
		return nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "IPv6DelegatedAddress" {
			continue
		}
		spec, err := resource.IPv6DelegatedAddressSpec()
		if err != nil {
			return err
		}
		if spec.PrefixDelegation != pdName {
			continue
		}
		linkUp := c.linkReady(spec.Interface)
		if !resourcequery.DependenciesReady(c.Store, spec.DependsOn) {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DependsOnFalse"})
			continue
		}
		if !linkUp {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", resource.Metadata.Name, map[string]any{"phase": "Pending"})
			continue
		}
		addr, err := DeriveIPv6Address(prefix, spec.SubnetID, spec.AddressSuffix)
		if err != nil {
			return err
		}
		status := map[string]any{
			"phase":        "Applied",
			"address":      addr,
			"interface":    spec.Interface,
			"prefixSource": pdName,
			"dryRun":       c.DryRun,
		}
		changed := objectStatusChanged("IPv6DelegatedAddress", c.Store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", resource.Metadata.Name), status)
		ifname := interfaceIfName(c.Router, spec.Interface)
		if ifname == "" {
			ifname = spec.Interface
		}
		addressPresent := true
		if !c.DryRun {
			addressPresentFn := c.AddressPresent
			if addressPresentFn == nil {
				addressPresentFn = ipv6AddressPresent
			}
			addressPresent = addressPresentFn(ctx, ifname, addr)
		}
		if !c.DryRun && (changed || !addressPresent) {
			command := c.Command
			if command == nil {
				command = runCommandContext
			}
			name, args := ipv6StaticAddressApplyCommand(platform.CurrentOS(), ifname, addr)
			if err := command(ctx, name, args...); err != nil {
				return err
			}
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", resource.Metadata.Name, status); err != nil {
			return err
		}
		if (changed || !addressPresent) && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.lan.address.applied", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"address": addr, "interface": spec.Interface, "dryRun": fmt.Sprintf("%t", c.DryRun)}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c LANAddressController) linkReady(name string) bool {
	if status := c.Store.ObjectStatus(api.NetAPIVersion, "Interface", name); status != nil {
		if phase := fmt.Sprint(status["phase"]); phase != "" && phase != "<nil>" {
			return phase == "Up"
		}
	}
	ifname := interfaceIfName(c.Router, name)
	if ifname == "" {
		ifname = name
	}
	ifi, err := net.InterfaceByName(ifname)
	if err == nil && ifi.Flags&net.FlagUp != 0 {
		_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "Interface", name, map[string]any{"phase": "Up", "ifname": ifname})
		return true
	}
	return false
}

func interfaceIfName(router *api.Router, name string) string {
	if router == nil {
		return name
	}
	for _, resource := range router.Spec.Resources {
		if resource.Metadata.Name != name {
			continue
		}
		switch resource.Kind {
		case "Interface":
			spec, err := resource.InterfaceSpec()
			if err == nil && spec.IfName != "" {
				return spec.IfName
			}
		case "Bridge":
			spec, err := resource.BridgeSpec()
			if err == nil && spec.IfName != "" {
				return spec.IfName
			}
		case "VXLANSegment":
			spec, err := resource.VXLANSegmentSpec()
			if err == nil && spec.IfName != "" {
				return spec.IfName
			}
		}
	}
	return name
}

func renderAndEnsureDnsmasq(ctx context.Context, router *api.Router, store Store, command, configPath, pidFile string, port int, listenAddresses []string) error {
	configPath = firstNonEmpty(configPath, "/run/routerd/dnsmasq-phase1.conf")
	pidFile = firstNonEmpty(pidFile, "/run/routerd/dnsmasq-phase1.pid")
	if port == 0 {
		port = 1053
	}
	changed, err := writeDnsmasqConfig(router, store, configPath, pidFile, port, listenAddresses)
	if err != nil {
		return err
	}
	return ensureDnsmasq(ctx, command, configPath, pidFile, changed)
}

func routerNeedsDnsmasq(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "DHCPv4Server", "DHCPv6Server", "IPv6RouterAdvertisement", "DHCPv4Relay":
			return true
		}
	}
	return false
}

func daemonStatus(ctx context.Context, socketPath string) (daemonapi.DaemonStatus, error) {
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socketPath)
	}}}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/status", nil)
	if err != nil {
		return daemonapi.DaemonStatus{}, err
	}
	req.Close = true
	resp, err := client.Do(req)
	if err != nil {
		return daemonapi.DaemonStatus{}, err
	}
	defer resp.Body.Close()
	var status daemonapi.DaemonStatus
	return status, json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&status)
}

func writeDnsmasqConfig(router *api.Router, store Store, path, pidFile string, port int, listenAddresses []string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(pidFile), 0755); err != nil {
		return false, err
	}
	leaseFile, err := dnsmasqLeaseFile(router, path, pidFile)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(leaseFile), 0755); err != nil {
		return false, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "port=0\nno-resolv\nno-hosts\nbind-interfaces\npid-file=%s\ndhcp-leasefile=%s\n", pidFile, leaseFile)
	lines, err := dnsmasqLANServiceLines(router, store)
	if err != nil {
		return false, err
	}
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	data := []byte(b.String())
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, data) {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, os.WriteFile(path, data, 0644)
}

func dnsmasqLeaseFile(router *api.Router, configPath, pidFile string) (string, error) {
	declared := ""
	if router == nil {
		router = &api.Router{}
	}
	for _, resource := range router.Spec.Resources {
		var leaseFile string
		switch resource.Kind {
		case "DHCPv4Server":
			spec, err := resource.DHCPv4ServerSpec()
			if err != nil {
				return "", err
			}
			leaseFile = strings.TrimSpace(spec.LeaseFile)
		case "DHCPv6Server":
			spec, err := resource.DHCPv6ServerSpec()
			if err != nil {
				return "", err
			}
			leaseFile = strings.TrimSpace(spec.LeaseFile)
		default:
			continue
		}
		if leaseFile == "" {
			continue
		}
		if declared != "" && declared != leaseFile {
			return "", fmt.Errorf("dnsmasq leaseFile mismatch: %s and %s", declared, leaseFile)
		}
		declared = leaseFile
	}
	if declared != "" {
		return declared, nil
	}
	leaseDir := filepath.Dir(configPath)
	if strings.TrimSpace(leaseDir) == "" {
		leaseDir = filepath.Dir(pidFile)
	}
	leaseFile := filepath.Join(leaseDir, "dnsmasq.leases")
	defaults, features := platform.Current()
	if features.HasRCD {
		leaseFile = filepath.Join(defaults.StateDir, "dnsmasq", "dnsmasq.leases")
	}
	return leaseFile, nil
}

func dnsmasqListenAddresses(addresses []string) []string {
	var out []string
	for _, address := range addresses {
		address = strings.TrimSpace(address)
		if address != "" {
			out = append(out, address)
		}
	}
	if len(out) == 0 {
		return []string{"127.0.0.1"}
	}
	return out
}

func dnsmasqLANServiceLines(router *api.Router, store Store) ([]string, error) {
	aliases := chainInterfaceAliases(router)
	raMTUByScope, err := render.PathMTURAByScope(router)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DHCPv4Server" {
			continue
		}
		spec, err := resource.DHCPv4ServerSpec()
		if err != nil || spec.Interface == "" {
			continue
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			continue
		}
		if pending := dhcpv4ServerPending(router, store, spec); pending != "" {
			continue
		}
		tag := sanitizeChainTag(resource.Metadata.Name)
		lines = append(lines, "interface="+ifname)
		lines = append(lines, "dhcp-script=/usr/local/libexec/routerd/dhcp-event-relay")
		leaseTime := firstNonEmpty(spec.AddressPool.LeaseTime, "12h")
		lines = append(lines, fmt.Sprintf("dhcp-range=set:%s,%s,%s,%s", tag, spec.AddressPool.Start, spec.AddressPool.End, leaseTime))
		gateway := firstNonEmpty(statusAddressValue(resourcequery.Value(store, spec.GatewayFrom)), spec.Gateway)
		dnsServerSources, _ := expandIPv4DHCPServerSources(store, spec.DNSServerFrom, "DNSServerFrom")
		ntpServerSources, _ := expandIPv4DHCPServerSources(store, spec.NTPServerFrom, "NTPServerFrom")
		dnsServers := append(expandIPv4DHCPServers(spec.DNSServers), dnsServerSources...)
		ntpServers := append(expandIPv4DHCPServers(spec.NTPServers), ntpServerSources...)
		if gateway != "" {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:router,%s", tag, gateway))
		}
		if len(dnsServers) > 0 {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:dns-server,%s", tag, strings.Join(dnsServers, ",")))
		}
		if len(ntpServers) > 0 {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:ntp-server,%s", tag, strings.Join(ntpServers, ",")))
		}
		domains, _ := expandDomainValues(router, store, []string{spec.Domain}, []api.StatusValueSourceSpec{spec.DomainFrom}, "DomainFrom")
		if len(domains) > 0 {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:domain-name,%s", tag, domains[0]))
			if !hasDHCPv4Option(spec.Options, "domain-search", 119) {
				lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:domain-search,%s", tag, domains[0]))
			}
		}
		for _, option := range spec.Options {
			lines = append(lines, "dhcp-option=tag:"+tag+","+dnsmasqDHCPv4Option(option))
		}
		for _, reservation := range router.Spec.Resources {
			if reservation.Kind != "DHCPv4Reservation" {
				continue
			}
			reservationSpec, err := reservation.DHCPv4ReservationSpec()
			if err != nil {
				continue
			}
			if reservationSpec.Scope != "" || (reservationSpec.Server != "" && reservationSpec.Server != resource.Metadata.Name) {
				continue
			}
			reservationTag := sanitizeChainTag(reservation.Metadata.Name)
			lines = append(lines, "dhcp-host="+dnsmasqIPv4Reservation(reservationSpec, reservationTag))
			for _, option := range reservationSpec.Options {
				lines = append(lines, "dhcp-option=tag:"+reservationTag+","+dnsmasqDHCPv4Option(option))
			}
		}
		lines = append(lines, dnsmasqStickyHostLines("ipv4", leaseTime)...)
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DHCPv6Server" {
			continue
		}
		spec, err := resource.DHCPv6ServerSpec()
		if err != nil || spec.Interface == "" {
			continue
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			ifname = spec.Interface
		}
		if pending := dhcpv6ServerPending(router, store, spec); pending != "" {
			continue
		}
		tag := sanitizeChainTag(resource.Metadata.Name)
		lines = append(lines, "interface="+ifname, "enable-ra")
		leaseTime := firstNonEmpty(spec.AddressPool.LeaseTime, spec.LeaseTime, "12h")
		switch firstNonEmpty(spec.Mode, "stateless") {
		case "stateful":
			lines = append(lines, fmt.Sprintf("dhcp-range=set:%s,%s,%s,constructor:%s,64,%s", tag, spec.AddressPool.Start, spec.AddressPool.End, ifname, leaseTime))
		case "both":
			lines = append(lines, fmt.Sprintf("dhcp-range=set:%s,%s,%s,constructor:%s,slaac,64,%s", tag, spec.AddressPool.Start, spec.AddressPool.End, ifname, leaseTime))
		default:
			lines = append(lines, fmt.Sprintf("dhcp-range=set:%s,::,constructor:%s,ra-stateless,64,%s", tag, ifname, leaseTime))
		}
		dnsServerSources, _ := expandServerSources(store, spec.DNSServerFrom, "DNSServerFrom")
		for _, server := range append(expandServers(store, spec.DNSServers), dnsServerSources...) {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option6:dns-server,[%s]", tag, dnsmasqIPv6Address(server)))
		}
		searchDomains, _ := expandDomainValues(router, store, spec.DomainSearch, spec.DomainSearchFrom, "DomainSearchFrom")
		if len(searchDomains) > 0 {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option6:domain-search,%s", tag, strings.Join(searchDomains, ",")))
		}
		sntpServerSources, _ := expandServerSources(store, spec.SNTPServerFrom, "SNTPServerFrom")
		for _, server := range append(expandServers(store, spec.SNTPServers), sntpServerSources...) {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option6:sntp-server,[%s]", tag, dnsmasqIPv6Address(server)))
		}
		if spec.RapidCommit {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option6:rapid-commit", tag))
		}
		lines = append(lines, dnsmasqStickyHostLines("ipv6", leaseTime)...)
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "IPv6RouterAdvertisement" {
			continue
		}
		spec, err := resource.IPv6RouterAdvertisementSpec()
		if err != nil {
			continue
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			ifname = spec.Interface
		}
		lines = append(lines, "interface="+ifname, "enable-ra")
		var params []string
		mtu := chainFirstNonZero(raMTUByScope[resource.Metadata.Name], spec.MTU)
		if mtu != 0 {
			params = append(params, fmt.Sprintf("mtu:%d", mtu))
		}
		switch spec.PRFPreference {
		case "high", "low":
			params = append(params, spec.PRFPreference)
		}
		if spec.ValidLifetime != "" {
			params = append(params, "0", spec.ValidLifetime)
		} else if mtu != 0 && (spec.PRFPreference == "high" || spec.PRFPreference == "low") {
			params = append(params, "0")
		}
		if len(params) > 0 {
			lines = append(lines, fmt.Sprintf("ra-param=%s,%s", ifname, strings.Join(params, ",")))
		}
		rdnssFrom, _ := expandServerSources(store, spec.RDNSSFrom, "RDNSSFrom")
		for _, server := range append(expandServers(store, spec.RDNSS), rdnssFrom...) {
			lines = append(lines, fmt.Sprintf("dhcp-option=option6:dns-server,[%s]", dnsmasqIPv6Address(server)))
		}
		dnssl, _ := expandDomainValues(router, store, spec.DNSSL, spec.DNSSLFrom, "DNSSLFrom")
		if len(dnssl) > 0 {
			lines = append(lines, "dhcp-option=option6:domain-search,"+strings.Join(dnssl, ","))
		}
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DHCPv4Relay" {
			continue
		}
		spec, err := resource.DHCPv4RelaySpec()
		if err != nil {
			continue
		}
		for _, iface := range spec.Interfaces {
			ifname := aliases[iface]
			if ifname == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("dhcp-relay=0.0.0.0,%s,%s", spec.Upstream, ifname))
		}
	}
	return lines, nil
}

func chainInterfaceAliases(router *api.Router) map[string]string {
	aliases := map[string]string{}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "Interface":
			spec, err := resource.InterfaceSpec()
			if err == nil {
				aliases[resource.Metadata.Name] = spec.IfName
			}
		case "Bridge", "VXLANTunnel", "VRF":
			aliases[resource.Metadata.Name] = resource.Metadata.Name
		}
	}
	return aliases
}

func dnsmasqDHCPv4Option(option api.DHCPv4OptionSpec) string {
	key := option.Name
	if key == "" {
		key = fmt.Sprintf("%d", option.Code)
	} else {
		key = "option:" + key
	}
	return key + "," + option.Value
}

func hasDHCPv4Option(options []api.DHCPv4OptionSpec, name string, code int) bool {
	for _, option := range options {
		if option.Code == code || strings.EqualFold(strings.TrimSpace(option.Name), name) {
			return true
		}
	}
	return false
}

func dnsmasqIPv4Reservation(spec api.DHCPv4ReservationSpec, tag string) string {
	parts := []string{strings.ToLower(spec.MACAddress)}
	if tag != "" {
		parts = append(parts, "set:"+tag)
	}
	if spec.Hostname != "" {
		parts = append(parts, spec.Hostname)
	}
	parts = append(parts, spec.IPAddress)
	return strings.Join(parts, ",")
}

func dnsmasqStickyHostLines(family, leaseTime string) []string {
	defaults, _ := platform.Current()
	path := strings.TrimRight(defaults.StateDir, "/") + "/dhcp-sticky.db"
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	log, err := logstore.OpenDHCPStickyLogReadOnly(path)
	if err != nil {
		return nil
	}
	defer log.Close()
	rows, err := log.List(context.Background(), logstore.DHCPStickyFilter{HeldOnly: true, Limit: 10000})
	if err != nil {
		return nil
	}
	var lines []string
	for _, row := range rows {
		rowFamily := strings.ToLower(strings.TrimSpace(row.Family))
		if rowFamily == "" {
			if strings.Contains(row.IP, ":") {
				rowFamily = "ipv6"
			} else {
				rowFamily = "ipv4"
			}
		}
		if rowFamily != family || row.MAC == "" || row.IP == "" {
			continue
		}
		parts := []string{strings.ToLower(row.MAC), row.IP}
		if row.Hostname != "" {
			parts = append(parts, row.Hostname)
		}
		if leaseTime != "" {
			parts = append(parts, leaseTime)
		}
		lines = append(lines, "dhcp-host="+strings.Join(parts, ","))
	}
	sort.Strings(lines)
	return lines
}

func expandIPv4DHCPServers(values []string) []string {
	var out []string
	for _, value := range values {
		if address := statusAddressValue(value); address != "" {
			out = append(out, address)
		}
	}
	return out
}

func expandIPv4DHCPServerSources(store Store, sources []api.StatusValueSourceSpec, label string) ([]string, string) {
	var out []string
	for _, source := range sources {
		before := len(out)
		for _, value := range resourcequery.Values(store, source) {
			if address := statusAddressValue(value); address != "" {
				out = append(out, address)
			}
		}
		if len(out) == before {
			if pending := unresolvedStatusSourceReason(label, source); pending != "" {
				return out, pending
			}
		}
	}
	return out, ""
}

func statusAddressValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Addr().String()
	}
	return value
}

func dnsmasqIPv6Address(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "[]")
	if addr, _, ok := strings.Cut(value, "/"); ok {
		return addr
	}
	return value
}

func sanitizeChainTag(value string) string {
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, ".", "-")
	return value
}

func expandServers(store Store, values []string) []string {
	var out []string
	for _, value := range values {
		resolved := valueFromStatusRef(store, value)
		if list := decodeStringList(resolved); len(list) > 0 {
			out = append(out, list...)
			continue
		}
		if strings.TrimSpace(resolved) != "" {
			out = append(out, strings.TrimSpace(resolved))
		}
	}
	return out
}

func expandServerSources(store Store, sources []api.StatusValueSourceSpec, label string) ([]string, string) {
	var out []string
	for _, source := range sources {
		values := compactNonEmptyStrings(resourcequery.Values(store, source))
		if len(values) == 0 {
			if pending := unresolvedStatusSourceReason(label, source); pending != "" {
				return out, pending
			}
			continue
		}
		out = append(out, values...)
	}
	return out, ""
}

func expandDomainValues(router *api.Router, store Store, values []string, sources []api.StatusValueSourceSpec, label string) ([]string, string) {
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	for _, source := range sources {
		if strings.TrimSpace(source.Resource) == "" {
			continue
		}
		resolved := compactNonEmptyStrings(resourcequery.ValuesFromStoreOrRouter(store, router, source))
		if len(resolved) == 0 {
			if pending := unresolvedStatusSourceReason(label, source); pending != "" {
				return compactNonEmptyStrings(out), pending
			}
			continue
		}
		out = append(out, resolved...)
	}
	return compactNonEmptyStrings(out), ""
}

func dhcpv4ServerPending(router *api.Router, store Store, spec api.DHCPv4ServerSpec) string {
	if strings.TrimSpace(spec.GatewayFrom.Resource) != "" {
		if address := statusAddressValue(resourcequery.Value(store, spec.GatewayFrom)); address == "" {
			if pending := unresolvedStatusSourceReason("GatewayFrom", spec.GatewayFrom); pending != "" {
				return pending
			}
		}
	}
	if _, pending := expandIPv4DHCPServerSources(store, spec.DNSServerFrom, "DNSServerFrom"); pending != "" {
		return pending
	}
	if _, pending := expandIPv4DHCPServerSources(store, spec.NTPServerFrom, "NTPServerFrom"); pending != "" {
		return pending
	}
	if _, pending := expandDomainValues(router, store, nil, []api.StatusValueSourceSpec{spec.DomainFrom}, "DomainFrom"); pending != "" {
		return pending
	}
	return ""
}

func dhcpv6ServerPending(router *api.Router, store Store, spec api.DHCPv6ServerSpec) string {
	if _, pending := expandServerSources(store, spec.DNSServerFrom, "DNSServerFrom"); pending != "" {
		return pending
	}
	if _, pending := expandServerSources(store, spec.SNTPServerFrom, "SNTPServerFrom"); pending != "" {
		return pending
	}
	if _, pending := expandDomainValues(router, store, nil, spec.DomainSearchFrom, "DomainSearchFrom"); pending != "" {
		return pending
	}
	return ""
}

func unresolvedStatusSourceReason(label string, source api.StatusValueSourceSpec) string {
	if strings.TrimSpace(source.Resource) == "" || source.Optional {
		return ""
	}
	return label + "Unresolved: " + source.Resource
}

func compactNonEmptyStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func ensureDnsmasq(ctx context.Context, command, configPath, pidFile string, changed bool) error {
	dnsmasqMu.Lock()
	defer dnsmasqMu.Unlock()

	command = firstNonEmpty(command, "dnsmasq")
	if err := testDnsmasqConfig(ctx, command, configPath); err != nil {
		return err
	}
	if platform.IsNixOSHost() {
		return ensureNixOSDnsmasqService(ctx, changed)
	}
	defaults, features := platform.Current()
	if features.HasSystemd {
		return ensureSystemdDnsmasqService(ctx, defaults.SystemdSystemDir, command, configPath, pidFile, changed)
	}
	proc, alive := dnsmasqProcess(pidFile)
	if alive && changed {
		return proc.Signal(syscall.SIGHUP)
	}
	if alive {
		return nil
	}
	return startDnsmasq(ctx, command, configPath, pidFile)
}

func ensureSystemdDnsmasqService(ctx context.Context, systemdDir, command, configPath, pidFile string, changed bool) error {
	const service = "routerd-dnsmasq.service"
	if systemdDir == "" {
		systemdDir = "/etc/systemd/system"
	}
	command = dnsmasqCommandPath(command)
	servicePath := filepath.Join(systemdDir, service)
	unitChanged, err := writeFileIfChanged(servicePath, render.DnsmasqServiceUnitWithPID(configPath, pidFile, command), 0644, false)
	if err != nil {
		return err
	}
	if unitChanged {
		if err := runSystemctl(ctx, "daemon-reload"); err != nil {
			return err
		}
	}
	if exec.CommandContext(ctx, "systemctl", "is-enabled", "--quiet", service).Run() != nil {
		if err := runSystemctl(ctx, "enable", service); err != nil {
			return err
		}
	}
	active := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", service).Run() == nil
	if changed || unitChanged || !active || !dnsmasqProcessUsesConfig(pidFile, configPath) {
		_ = exec.CommandContext(ctx, "systemctl", "reset-failed", service).Run()
		if err := runSystemctl(ctx, "restart", service); err != nil {
			return err
		}
	}
	return nil
}

func ensureNixOSDnsmasqService(ctx context.Context, changed bool) error {
	const service = "routerd-dnsmasq.service"
	if err := removeStaleRuntimeDnsmasqUnit(ctx); err != nil {
		return err
	}
	if changed {
		_ = exec.CommandContext(ctx, "systemctl", "reset-failed", service).Run()
		out, err := exec.CommandContext(ctx, "systemctl", "restart", service).CombinedOutput()
		if err != nil {
			return fmt.Errorf("systemctl restart %s: %w: %s", service, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if err := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", service).Run(); err == nil {
		return nil
	}
	_ = exec.CommandContext(ctx, "systemctl", "reset-failed", service).Run()
	out, err := exec.CommandContext(ctx, "systemctl", "restart", service).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl restart %s: %w: %s", service, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func removeStaleRuntimeDnsmasqUnit(ctx context.Context) error {
	const service = "/run/systemd/system/routerd-dnsmasq.service"
	if _, err := os.Stat(service); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.Remove(service); err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, "systemctl", "daemon-reload").CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func testDnsmasqConfig(ctx context.Context, command, configPath string) error {
	out, err := exec.CommandContext(ctx, command, "--test", "--conf-file="+configPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s --test --conf-file=%s: %w: %s", command, configPath, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func dnsmasqProcess(pidFile string) (*os.Process, bool) {
	pid, err := os.ReadFile(pidFile)
	if err != nil {
		return nil, false
	}
	proc, err := os.FindProcess(atoi(strings.TrimSpace(string(pid))))
	if err != nil {
		return nil, false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		if err == syscall.EPERM {
			return proc, true
		}
		return nil, false
	}
	return proc, true
}

func dnsmasqProcessUsesConfig(pidFile, configPath string) bool {
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}
	pid := strings.TrimSpace(string(pidData))
	if pid == "" {
		return false
	}
	cmdline, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
	if err != nil {
		return false
	}
	return dnsmasqCmdlineUsesConfig(strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00"), configPath)
}

func dnsmasqCmdlineUsesConfig(fields []string, configPath string) bool {
	for i, field := range fields {
		if field == "--conf-file="+configPath {
			return true
		}
		if field == "--conf-file" && i+1 < len(fields) && fields[i+1] == configPath {
			return true
		}
	}
	return false
}

func dnsmasqCommandPath(command string) string {
	command = strings.TrimSpace(firstNonEmpty(command, "dnsmasq"))
	if strings.ContainsRune(command, os.PathSeparator) {
		return command
	}
	if path, err := exec.LookPath(command); err == nil {
		return path
	}
	return command
}

func runSystemctl(ctx context.Context, args ...string) error {
	out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func startDnsmasq(ctx context.Context, command, configPath, pidFile string) error {
	_ = os.Remove(pidFile)
	cmd := exec.CommandContext(ctx, firstNonEmpty(command, "dnsmasq"), "--keep-in-foreground", "--conf-file="+configPath, "--pid-file="+pidFile)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		_ = os.Remove(pidFile)
		if err != nil {
			return err
		}
		return fmt.Errorf("dnsmasq exited during startup")
	case <-time.After(300 * time.Millisecond):
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil && err != syscall.EPERM {
		return fmt.Errorf("dnsmasq is not alive")
	}
	_ = os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", cmd.Process.Pid)), 0o644)
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func chainFirstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func atoi(value string) int {
	var out int
	_, _ = fmt.Sscanf(value, "%d", &out)
	return out
}
