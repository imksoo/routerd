// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
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
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
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
	plugincontroller "github.com/imksoo/routerd/pkg/controller/plugin"
	"github.com/imksoo/routerd/pkg/controller/pppoesession"
	provideractioncontroller "github.com/imksoo/routerd/pkg/controller/provideraction"
	vrrpcontroller "github.com/imksoo/routerd/pkg/controller/vrrp"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/derived"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/egressroute"
	"github.com/imksoo/routerd/pkg/eventconsumer"
	"github.com/imksoo/routerd/pkg/eventrule"
	"github.com/imksoo/routerd/pkg/ha"
	"github.com/imksoo/routerd/pkg/healthcheck"
	"github.com/imksoo/routerd/pkg/lifecycle"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observabilitypipeline"
	"github.com/imksoo/routerd/pkg/observe"
	"github.com/imksoo/routerd/pkg/platform"
	provideraction "github.com/imksoo/routerd/pkg/provideraction"
	"github.com/imksoo/routerd/pkg/providerinventory"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

var dnsmasqMu sync.Mutex

type Store interface {
	HealthObservation(string) (api.HealthObservation, error)
	ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error)
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type objectStatusMerger interface {
	MergeObjectStatus(apiVersion, kind, name string, updates map[string]any) error
}

type eventedStore struct {
	Store  Store
	Bus    *bus.Bus
	Router *api.Router
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
	ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error)
	ListActions(routerstate.ActionExecutionFilter) ([]routerstate.ActionExecutionRecord, error)
}

type mobilityStore struct {
	evented eventedStore
	data    mobilityDataStore
}

func (s mobilityStore) withRouter(router *api.Router) mobilityStore {
	s.evented = s.evented.withRouter(router)
	return s
}
func (s mobilityStore) ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error) {
	return s.evented.ReadObjectStatus(apiVersion, kind, name)
}
func (s mobilityStore) Get(name string) routerstate.Value { return s.evented.Get(name) }
func (s mobilityStore) ReadVariable(name string) (routerstate.Value, error) {
	return s.evented.ReadVariable(name)
}
func (s mobilityStore) Age(name string) time.Duration { return s.evented.Age(name) }
func (s mobilityStore) Now() time.Time                { return s.evented.Now() }

func (s mobilityStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	return s.evented.SaveObjectStatus(apiVersion, kind, name, status)
}

func (s mobilityStore) MergeObjectStatus(apiVersion, kind, name string, updates map[string]any) error {
	return s.evented.MergeObjectStatus(apiVersion, kind, name, updates)
}

func (s mobilityStore) HealthObservation(name string) (api.HealthObservation, error) {
	return s.evented.HealthObservation(name)
}
func (s eventSubscriptionStore) HealthObservation(name string) (api.HealthObservation, error) {
	return s.evented.HealthObservation(name)
}
func (s mobilityStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	return s.evented.ObjectStatus(apiVersion, kind, name)
}

func (s mobilityStore) RecordBusEvent(ctx context.Context, event daemonapi.DaemonEvent) (string, error) {
	if s.evented.Bus != nil {
		return "", s.evented.Bus.Publish(ctx, event)
	}
	if store, ok := s.data.(bus.EventStore); ok {
		return store.RecordBusEvent(ctx, event)
	}
	return "", nil
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

func (s mobilityStore) ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error) {
	return s.data.ListDynamicConfigParts()
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

func (s eventSubscriptionStore) CommitSubscriptionResult(ctx context.Context, subscription string, eventIDs []string, part routerstate.DynamicConfigPartRecord) error {
	return s.data.CommitSubscriptionResult(ctx, subscription, eventIDs, part)
}

func (s eventSubscriptionStore) GetDynamicConfigPartsBySource(source string) ([]routerstate.DynamicConfigPartRecord, error) {
	return s.data.GetDynamicConfigPartsBySource(source)
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
	status = s.statusWithLifecycle(apiVersion, kind, name, status)
	if statusEventHistoryRequired(s.Router) {
		return s.saveStatusWithHistory(apiVersion, kind, name, status, false)
	}
	current := s.Store.ObjectStatus(apiVersion, kind, name)
	if newerStatus(current, status) {
		return nil
	}
	publishChanged := shouldPublishStatusChangedEvent(apiVersion, kind, current, status)
	if err := s.Store.SaveObjectStatus(apiVersion, kind, name, status); err != nil {
		return err
	}
	if publishChanged && s.Bus != nil {
		changedFields := statusChangedFieldsForEvent(apiVersion, kind, current, status)
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "store"}, "routerd.resource.status.changed", statusChangedEventSeverity(apiVersion, kind, current, status, changedFields))
		event.Resource = &daemonapi.ResourceRef{APIVersion: apiVersion, Kind: kind, Name: name}
		event.Attributes = map[string]string{
			"phase":         fmt.Sprint(status["phase"]),
			"previousPhase": fmt.Sprint(current["phase"]),
			"changedFields": strings.Join(changedFields, ","),
		}
		return s.Bus.Publish(context.Background(), event)
	}
	return nil
}

func (s eventedStore) MergeObjectStatus(apiVersion, kind, name string, updates map[string]any) error {
	if s.Store == nil {
		return nil
	}
	status := s.statusWithLifecycle(apiVersion, kind, name, copyStatusMap(updates))
	if statusEventHistoryRequired(s.Router) {
		return s.saveStatusWithHistory(apiVersion, kind, name, status, true)
	}
	current := s.Store.ObjectStatus(apiVersion, kind, name)
	next := copyStatusMap(current)
	for key, value := range status {
		next[key] = value
	}
	if newerStatus(current, next) {
		return nil
	}
	publishChanged := shouldPublishStatusChangedEvent(apiVersion, kind, current, next)
	if store, ok := s.Store.(objectStatusMerger); ok {
		if err := store.MergeObjectStatus(apiVersion, kind, name, status); err != nil {
			return err
		}
	} else if err := s.Store.SaveObjectStatus(apiVersion, kind, name, next); err != nil {
		return err
	}
	if publishChanged && s.Bus != nil {
		changedFields := statusChangedFieldsForEvent(apiVersion, kind, current, next)
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "store"}, "routerd.resource.status.changed", statusChangedEventSeverity(apiVersion, kind, current, next, changedFields))
		event.Resource = &daemonapi.ResourceRef{APIVersion: apiVersion, Kind: kind, Name: name}
		event.Attributes = map[string]string{
			"phase":         fmt.Sprint(next["phase"]),
			"previousPhase": fmt.Sprint(current["phase"]),
			"changedFields": strings.Join(changedFields, ","),
		}
		return s.Bus.Publish(context.Background(), event)
	}
	return nil
}

func (s eventedStore) withRouter(router *api.Router) eventedStore {
	s.Router = router
	return s
}

func (s eventedStore) statusWithLifecycle(apiVersion, kind, name string, status map[string]any) map[string]any {
	resource, found := s.resourceForStatus(apiVersion, kind, name)
	return statusWithLifecycle(apiVersion, kind, name, resource, found, status)
}

func statusWithOwnership(apiVersion, kind string, status map[string]any) map[string]any {
	return statusWithLifecycle(apiVersion, kind, "", api.Resource{}, false, status)
}

func copyStatusMap(status map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range status {
		out[key] = value
	}
	return out
}

// statusValueMap normalizes a persisted status value for the generic
// controller paths that consume nested observed state. It is deliberately
// unrelated to MobilityPool/ARP observation handling.
func statusValueMap(value any) map[string]any {
	out := map[string]any{}
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			out[key] = item
		}
	case map[string]string:
		for key, item := range typed {
			out[key] = item
		}
	case map[string]map[string]any:
		for key, item := range typed {
			out[key] = item
		}
	case map[any]any:
		for key, item := range typed {
			out[fmt.Sprint(key)] = item
		}
	}
	return out
}

func preserveStatusFields(status, current map[string]any, fields ...string) map[string]any {
	if len(current) == 0 || len(fields) == 0 {
		return status
	}
	out := copyStatusMap(status)
	for _, field := range fields {
		if _, exists := out[field]; exists {
			continue
		}
		if value, ok := current[field]; ok {
			out[field] = value
		}
	}
	return out
}

func statusWithLifecycle(apiVersion, kind, name string, resource api.Resource, found bool, status map[string]any) map[string]any {
	out := make(map[string]any, len(status)+8)
	for key, value := range status {
		out[key] = value
	}
	if _, ok := out["owner"]; !ok {
		if owner := resourceOwnerController(kind); owner != "" {
			out["owner"] = owner
		}
	}
	if _, ok := out["managedBy"]; !ok {
		if managed, ok := statusvalue.ExtendedBool(out["managed"]); ok && !managed {
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
			if managed, ok := statusvalue.ExtendedBool(out["managed"]); ok && !managed {
				out["management"] = "adopted"
			} else {
				out["management"] = "managed"
			}
		}
	}
	if !syntheticLifecycleStatus(apiVersion, kind, name) {
		if _, ok := out["ownerRef"]; !ok && strings.TrimSpace(name) != "" {
			out["ownerRef"] = lifecycle.OwnerRefStatusMap(lifecycle.SelfOwnerRef(apiVersion, kind, name))
		}
		if _, ok := out["ownerKey"]; !ok && strings.TrimSpace(name) != "" {
			out["ownerKey"] = lifecycle.OwnerKey(apiVersion, kind, name)
		}
		if found {
			if refs := lifecycle.OwnerRefsStatusList(resource.Metadata.OwnerRefs); len(refs) > 0 {
				if _, ok := out["ownerRefs"]; !ok {
					out["ownerRefs"] = refs
				}
			}
			if declaration, ok := lifecycle.DeclarationForResource(resource); ok {
				if _, ok := out["lifecycleClass"]; !ok {
					out["lifecycleClass"] = string(declaration.Class)
				}
			}
		} else if declaration, ok := lifecycle.Lookup(apiVersion, kind); ok {
			if _, ok := out["lifecycleClass"]; !ok {
				out["lifecycleClass"] = string(declaration.Class)
			}
		}
	}
	return out
}

func (s eventedStore) resourceForStatus(apiVersion, kind, name string) (api.Resource, bool) {
	if s.Router == nil || strings.TrimSpace(kind) == "" || strings.TrimSpace(name) == "" {
		return api.Resource{}, false
	}
	for _, resource := range s.Router.Spec.Resources {
		resourceAPIVersion := resource.APIVersion
		if resourceAPIVersion == "" {
			resourceAPIVersion = lifecycle.APIVersionForKind(resource.Kind)
		}
		if resourceAPIVersion == apiVersion && resource.Kind == kind && resource.Metadata.Name == name {
			return resource, true
		}
	}
	return api.Resource{}, false
}

func syntheticLifecycleStatus(apiVersion, kind, _ string) bool {
	if apiVersion == api.RouterAPIVersion {
		return true
	}
	switch kind {
	case "ConntrackObserver", "ConntrackTuning", "Router":
		return true
	default:
		return false
	}
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
	case "VRF":
		return "vrf"
	case "FirewallZone", "FirewallPolicy", "FirewallRule", "FirewallFlowPinhole", "ClientPolicy":
		return "firewall"
	case "NAT44Rule", "NAT44FlowDNATPinhole":
		return "nat"
	case "IPAddressSet", "LocalServiceRedirect":
		return "ip-address-set"
	case "NetworkAdoption":
		return "network-adoption"
	case "Package", "KernelModule":
		return "package"
	case "PPPoESession":
		return "pppoesession"
	case "IPv4Route", "IPv4StaticRoute", "IPv6StaticRoute", "EgressRoutePolicy", "HybridRoute", "SAMTransportProfile":
		return "route"
	case "Hostname":
		return "hostname"
	case "ServiceUnit", "TailscaleNode", "HealthCheck", "NTPClient", "NTPServer", "SysctlProfile", "Sysctl", "LogRetention", "ConntrackTuning":
		return "service-unit"
	case "ConntrackObserver", "TrafficFlowLog":
		return "conntrack"
	default:
		return ""
	}
}

func (s eventedStore) HealthObservation(name string) (api.HealthObservation, error) {
	return s.Store.HealthObservation(name)
}

func (s eventedStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if s.Store == nil {
		return nil
	}
	return s.Store.ObjectStatus(apiVersion, kind, name)
}

func (s eventedStore) ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error) {
	reader, ok := s.Store.(interface {
		ReadObjectStatus(string, string, string) (map[string]any, error)
	})
	if !ok {
		return nil, errors.New("object-status reading is unavailable")
	}
	return reader.ReadObjectStatus(apiVersion, kind, name)
}

func (s eventedStore) ReadBGPFIB(ctx context.Context, scope string) (bgpstate.FIBJournal, bool, error) {
	reader, ok := s.Store.(bgpcontroller.FIBStore)
	if !ok {
		return bgpstate.FIBJournal{}, false, errors.New("BGP FIB journal is unavailable")
	}
	return reader.ReadBGPFIB(ctx, scope)
}
func (s eventedStore) SaveBGPFIB(ctx context.Context, journal bgpstate.FIBJournal) error {
	writer, ok := s.Store.(bgpcontroller.FIBStore)
	if !ok {
		return errors.New("BGP FIB journal is unavailable")
	}
	return writer.SaveBGPFIB(ctx, journal)
}

func (s eventedStore) ReadVariable(name string) (routerstate.Value, error) {
	reader, ok := s.Store.(interface {
		ReadVariable(string) (routerstate.Value, error)
	})
	if !ok {
		return routerstate.Value{}, errors.New("state-variable reading is unavailable")
	}
	return reader.ReadVariable(name)
}

func (s eventedStore) Get(name string) routerstate.Value {
	store, ok := s.Store.(routerstate.Reader)
	if !ok {
		now := time.Now().UTC()
		return routerstate.Value{Status: routerstate.StatusUnknown, Since: now, UpdatedAt: now}
	}
	return store.Get(name)
}

func (s eventedStore) Set(name, value, reason string) (routerstate.Value, error) {
	store, ok := s.Store.(routerstate.Store)
	if !ok {
		return routerstate.Value{}, errors.New("state-variable persistence is unavailable")
	}
	return store.Set(name, value, reason)
}
func (s eventedStore) Unset(name, reason string) (routerstate.Value, error) {
	store, ok := s.Store.(routerstate.Store)
	if !ok {
		return routerstate.Value{}, errors.New("state-variable persistence is unavailable")
	}
	return store.Unset(name, reason)
}
func (s eventedStore) Forget(name, reason string) (routerstate.Value, error) {
	store, ok := s.Store.(routerstate.Store)
	if !ok {
		return routerstate.Value{}, errors.New("state-variable persistence is unavailable")
	}
	return store.Forget(name, reason)
}
func (s eventedStore) Delete(name string) error {
	store, ok := s.Store.(routerstate.Store)
	if !ok {
		return errors.New("state-variable persistence is unavailable")
	}
	return store.Delete(name)
}

func (s eventedStore) Age(name string) time.Duration {
	store, ok := s.Store.(routerstate.Reader)
	if !ok {
		return 0
	}
	return store.Age(name)
}

func (s eventedStore) Now() time.Time {
	store, ok := s.Store.(routerstate.Reader)
	if !ok {
		return time.Now().UTC()
	}
	return store.Now()
}

func (s eventedStore) Variables() map[string]routerstate.Value {
	store, ok := s.Store.(routerstate.Reader)
	if !ok {
		return nil
	}
	return store.Variables()
}

func (s eventedStore) ListObjectStatuses() ([]routerstate.ObjectStatus, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("object status enumeration is unavailable")
	}
	lister, ok := s.Store.(interface {
		ListObjectStatuses() ([]routerstate.ObjectStatus, error)
	})
	if !ok {
		return nil, fmt.Errorf("object status enumeration is unavailable")
	}
	return lister.ListObjectStatuses()
}

func (s eventedStore) ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error) {
	if s.Store == nil {
		return nil, nil
	}
	lister, ok := s.Store.(interface {
		ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error)
	})
	if !ok {
		return nil, nil
	}
	return lister.ListDynamicConfigParts()
}

func (s eventedStore) DeleteObject(apiVersion, kind, name string) error {
	if s.Store == nil {
		return fmt.Errorf("object deletion is unavailable")
	}
	deleter, ok := s.Store.(interface {
		DeleteObject(apiVersion, kind, name string) error
	})
	if !ok {
		return fmt.Errorf("object deletion is unavailable")
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

func shouldPublishStatusChangedEvent(apiVersion, kind string, current, next map[string]any) bool {
	if !statusChangedForEvent(apiVersion, kind, current, next) {
		return false
	}
	fields := statusChangedFieldsForEvent(apiVersion, kind, current, next)
	return !suppressStatusChangedEvent(apiVersion, kind, current, next, fields)
}

func suppressStatusChangedEvent(apiVersion, kind string, current, next map[string]any, fields []string) bool {
	if apiVersion != api.MobilityAPIVersion || kind != "MobilityPool" {
		return false
	}
	return mobilitycontroller.PoolObservationRefreshOnly(current, next, fields)
}

func statusChangedEventSeverity(apiVersion, kind string, current, next map[string]any, fields []string) string {
	if eventStateAbnormal(fmt.Sprint(next["phase"])) || eventStateAbnormal(fmt.Sprint(next["health"])) {
		return daemonapi.SeverityInfo
	}
	if statusChangedFieldsOnly(fields, chattyStatusChangedFields()) {
		return daemonapi.SeverityDebug
	}
	previousPhase := strings.TrimSpace(fmt.Sprint(current["phase"]))
	phase := strings.TrimSpace(fmt.Sprint(next["phase"]))
	if routinePhaseTransition(kind, previousPhase, phase) {
		allowed := chattyStatusChangedFields()
		allowed["phase"] = true
		if statusChangedFieldsOnly(fields, allowed) {
			return daemonapi.SeverityDebug
		}
	}
	if kind == "DHCPv4Client" && previousPhase == daemonapi.ResourcePhaseBound && phase == daemonapi.ResourcePhaseBound && !statusChangedFieldsInclude(fields, dhcpv4ClientInfoFields()) {
		return daemonapi.SeverityDebug
	}
	return daemonapi.SeverityInfo
}

func chattyStatusChangedFields() map[string]bool {
	return map[string]bool{
		"observedSelfStaleCaptures":               true,
		"ownershipResolverControlPlaneOwnerTable": true,
		"discoveryObserved":                       true,
	}
}

func dhcpv4ClientInfoFields() map[string]bool {
	return map[string]bool{
		"currentAddress": true,
		"defaultGateway": true,
		"gateway":        true,
		"dnsServers":     true,
		"prefixLength":   true,
	}
}

func statusChangedFieldsOnly(fields []string, allowed map[string]bool) bool {
	if len(fields) == 0 {
		return false
	}
	for _, field := range fields {
		if !allowed[strings.TrimSpace(field)] {
			return false
		}
	}
	return true
}

func statusChangedFieldsInclude(fields []string, candidates map[string]bool) bool {
	for _, field := range fields {
		if candidates[strings.TrimSpace(field)] {
			return true
		}
	}
	return false
}

func eventStateAbnormal(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "error", "failed", "failure", "degraded":
		return true
	default:
		return false
	}
}

func routinePhaseTransition(kind, previousPhase, phase string) bool {
	if (previousPhase == "Watching" && phase == "Ready") || (previousPhase == "Ready" && phase == "Watching") {
		return true
	}
	return kind == "DHCPv4Client" && previousPhase == daemonapi.ResourcePhaseBound && phase == daemonapi.ResourcePhaseBound
}

func statusForEvent(apiVersion, kind string, status map[string]any) map[string]any {
	if apiVersion == api.NetAPIVersion && kind == "DHCPv6PrefixDelegation" {
		status = copyStatusMap(status)
		delete(status, "observation")
		delete(status, "lastObservedAt")
		delete(status, "snapshotUpdatedAt")
	}
	stable := stableStatus(status)
	for key := range stable {
		if volatileStatusEventField(apiVersion, kind, key) {
			delete(stable, key)
		}
	}
	if apiVersion == api.MobilityAPIVersion && kind == "ARPObserver" {
		// The daemon keeps its client diagnostics as JSON text. Re-observing
		// the same client refreshes seenAt without changing its identity.
		// Keep all other fields, including unknown ones, and retain malformed
		// input unchanged so a decoding failure cannot hide a status change.
		if raw, ok := stable["observedClients"].(string); ok {
			var clients []map[string]json.RawMessage
			if err := json.Unmarshal([]byte(raw), &clients); err == nil {
				for _, client := range clients {
					delete(client, "seenAt")
				}
				stable["observedClients"] = clients
			}
		}
	}
	if apiVersion != api.MobilityAPIVersion || kind != "MobilityPool" {
		return stable
	}
	return mobilitycontroller.PoolStatusEventProjection(stable)
}

func volatileStatusEventField(apiVersion, kind, key string) bool {
	switch key {
	case "syncedAt", "resolvedAt", "nextRefreshAt":
		return true
	}
	switch kind {
	case "ARPObserver":
		if apiVersion != api.MobilityAPIVersion {
			return false
		}
		// Preserve these diagnostics in status, but do not journal or fan out
		// another resource transition for each packet, probe, or scan.
		switch key {
		case "lastPacketAt", "lastEventAt", "lastScanAt", "packetsSeen", "observedCount", "probeCount", "probeHitCount", "proactiveCount", "requestObservedCount", "commandProbeCount", "lastCommandProbe", "scanCount", "ignoredSenderMACObservationCount":
			return true
		}
	case "HealthCheck":
		return key == "lastSuccessTime"
	case "NAT44SessionSync":
		switch key {
		case "deleteFailed", "deleteMissing", "deleteOK", "insertExisting", "insertFailed", "insertOK", "lastBatchAt", "lastBatchEvents", "lastEventAt", "pruneCandidateCount", "pruneFailed", "pruneMissing", "pruneOK", "queuedEventCount", "remoteSessionCount", "scriptBytes", "sessionCount", "syncedAt", "targets":
			return true
		}
	case "DHCPv4ServerLeaseSync", "DHCPv6ServerLeaseSync", "DHCPv6PrefixDelegationLeaseSync":
		switch key {
		case "sources", "syncedAt", "targets":
			return true
		}
	case "IPAddressSet":
		return key == "minTTLSeconds" || key == "nftVerifiedAt" || key == "resolvedAt" || key == "nextRefreshAt"
	}
	_ = apiVersion
	return false
}

func stableStatus(status map[string]any) map[string]any {
	if status == nil {
		return nil
	}
	out := map[string]any{}
	for key, value := range status {
		switch key {
		case "updatedAt", "observedAt", "installedAt", "lastCheckedAt", "lastTransitionAt", "lastLeaseAt", "lastRenewAt", "lastAppliedAt", "renewAt", "rebindAt", "expiresAt", "consecutivePassed", "consecutiveFailed", "createdHint", "packetRing", "conditions", "mtuObservedAt":
			continue
		case "observed":
			continue
		case "ownerKey", "ownerRef", "ownerRefs", "lifecycleClass":
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
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if volatileNestedStatusField(key) {
				continue
			}
			out[key] = item
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
	case "renewedAt", "leaseUntil", "pendingUntil":
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
	return statusSubscriptionsWithWhen(nil, nil, kinds...)
}

func statusSubscriptionsWithWhen(router *api.Router, controlledKinds []string, kinds ...string) []bus.Subscription {
	allowed := map[string]bool{}
	for _, kind := range kinds {
		allowed[kind] = true
	}
	deps := whenStatusDependencyRefs(router, controlledKinds...)
	return []bus.Subscription{{
		Topics: []string{"routerd.resource.status.changed", "routerd.controller.bootstrap"},
		Filter: func(event daemonapi.DaemonEvent) bool {
			if event.Type == "routerd.controller.bootstrap" {
				return true
			}
			if event.Resource == nil {
				return false
			}
			if allowed[event.Resource.Kind] {
				return true
			}
			names := deps[event.Resource.Kind]
			if len(names) == 0 {
				return false
			}
			return names[""] || names[event.Resource.Name]
		},
	}}
}

func whenStatusSubscriptions(router *api.Router, controlledKinds ...string) []bus.Subscription {
	return statusSubscriptionsWithWhen(router, controlledKinds)
}

func vrrpStatusSubscriptions(router *api.Router) []bus.Subscription {
	return append(statusSubscriptionsWithWhen(router, []string{"VirtualAddress"}, "BGPRouter", "BGPPeer", "IngressService"), bus.Subscription{
		Topics: []string{daemonapi.EventVRRPRoleTransition},
	})
}

func serviceUnitStatusSubscriptions(router *api.Router) []bus.Subscription {
	// ServiceUnit results are outputs of this worker. Explicit When references
	// below still wake dependent units without feeding failed removals back.
	allowed := map[string]bool{
		"TailscaleNode":           true,
		"DHCPv4Client":            true,
		"DHCPv6PrefixDelegation":  true,
		"IPv6RouterAdvertisement": true,
		"DNSResolver":             true,
		"EventGroup":              true,
	}
	deps := whenStatusDependencyRefs(router, "ServiceUnit", "TailscaleNode", "DHCPv4Client", "DHCPv6PrefixDelegation", "IPv6RouterAdvertisement", "DNSResolver", "EventGroup", "HealthCheck")
	return []bus.Subscription{{
		Topics: []string{"routerd.resource.status.changed", "routerd.controller.bootstrap"},
		Filter: func(event daemonapi.DaemonEvent) bool {
			if event.Type == "routerd.controller.bootstrap" {
				return true
			}
			if event.Resource == nil {
				return false
			}
			if event.Resource.Kind == "HealthCheck" {
				return eventChangedField(event, "phase") || eventChangedField(event, "health")
			}
			if allowed[event.Resource.Kind] {
				return true
			}
			names := deps[event.Resource.Kind]
			if len(names) == 0 {
				return false
			}
			return names[""] || names[event.Resource.Name]
		},
	}}
}

func observabilityPipelineStatusSubscriptions(router *api.Router) []bus.Subscription {
	return whenStatusSubscriptions(router, "ObservabilityPipeline")
}

func whenStatusDependencyRefs(router *api.Router, controlledKinds ...string) map[string]map[string]bool {
	controlled := map[string]bool{}
	for _, kind := range controlledKinds {
		controlled[kind] = true
	}
	out := map[string]map[string]bool{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if !controlled[resource.Kind] {
			continue
		}
		addResourceWhenStatusDependencyRefs(out, resource)
	}
	return out
}

func addResourceWhenStatusDependencyRefs(out map[string]map[string]bool, resource api.Resource) {
	addWhenStatusDependencyRefs(out, resourcequery.ResourceWhen(resource))
	if resource.Kind == "VirtualAddress" {
		spec, err := resource.VirtualAddressSpec()
		if err == nil && spec.VRRP.GracefulActivation != nil {
			addWhenStatusDependencyRefs(out, spec.VRRP.GracefulActivation.ReadyWhen)
		}
	}
	if resource.Kind != "EgressRoutePolicy" {
		return
	}
	spec, err := resource.EgressRoutePolicySpec()
	if err != nil {
		return
	}
	for _, candidate := range spec.Candidates {
		addWhenStatusDependencyRefs(out, candidate.When)
	}
}

func addWhenStatusDependencyRefs(out map[string]map[string]bool, when api.ResourceWhenSpec) {
	for _, child := range when.All {
		addWhenStatusDependencyRefs(out, child)
	}
	for _, child := range when.Any {
		addWhenStatusDependencyRefs(out, child)
	}
	for ref := range when.State {
		kind, name, ok := whenStatusDependencyRef(ref)
		if !ok {
			continue
		}
		if out[kind] == nil {
			out[kind] = map[string]bool{}
		}
		out[kind][name] = true
	}
}

func whenStatusDependencyRef(ref string) (string, string, bool) {
	ref = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(ref, "${"), "}"))
	left := ref
	if before, _, ok := strings.Cut(ref, ".status."); ok {
		left = before
	} else if before, _, ok := strings.Cut(ref, "."); ok {
		left = before
	}
	kind, name, ok := resourcequery.SplitResource(left)
	return kind, name, ok
}

func bootstrapSubscriptions() []bus.Subscription {
	return []bus.Subscription{{Topics: []string{"routerd.controller.bootstrap"}}}
}

func dnsResolverStatusSubscriptions(router *api.Router) []bus.Subscription {
	subs := statusSubscriptionsWithWhen(router, []string{"DNSResolver", "DNSForwarder", "DNSUpstream", "DNSZone"},
		"DNSResolver", "DNSForwarder", "DNSUpstream", "DNSZone", "IPv6DelegatedAddress", "DHCPv6Information", "VirtualAddress", "Interface", "IngressService")
	subs = append(subs, bus.Subscription{Topics: []string{daemonapi.EventDHCPLeaseAdded, daemonapi.EventDHCPLeaseRenewed, daemonapi.EventDHCPLeaseRemoved}})
	return subs
}

func firewallStatusSubscriptions(router *api.Router) []bus.Subscription {
	subs := statusSubscriptionsWithWhen(router,
		[]string{"FirewallZone", "FirewallPolicy", "FirewallRule", "ClientPolicy", "PortForward", "IngressService", "LocalServiceRedirect", "FirewallFlowPinhole"},
		"FirewallZone", "FirewallPolicy", "FirewallRule", "ClientPolicy", "PortForward", "IngressService", "LocalServiceRedirect", "FirewallFlowPinhole",
		"IPAddressSet", "Interface", "DSLiteTunnel", "EgressRoutePolicy", "NAT44Rule", "VirtualAddress", "IPv4StaticAddress", "DHCPv4Client")
	subs = append(subs, bus.Subscription{Topics: []string{"routerd.firewall.**"}})
	return withMobilityPlanSubscriptions(subs)
}

func ipv4RouteStatusSubscriptions() []bus.Subscription {
	return withDynamicConfigPartSubscriptions(withMobilityPlanSubscriptions(statusSubscriptions("DSLiteTunnel", "TunnelInterface", "VRF", "EgressRoutePolicy", "VirtualAddress", "DHCPv4Client", "SAMTransportProfile")))
}

func ipv4StaticAddressStatusSubscriptions() []bus.Subscription {
	return withDynamicConfigPartSubscriptions(withMobilityPlanSubscriptions(statusSubscriptions("WireGuardInterface", "TunnelInterface", "VRF")))
}

func ipv4RouteControllerStatusSubscriptions(router *api.Router) []bus.Subscription {
	return withDynamicConfigPartSubscriptions(withMobilityPlanSubscriptions(statusSubscriptionsWithWhen(router, []string{"ClusterNetworkRoute"}, "DSLiteTunnel", "TunnelInterface", "VRF", "EgressRoutePolicy", "VirtualAddress", "DHCPv4Client", "SAMTransportProfile")))
}

func pathMTUStatusSubscriptions(router *api.Router) []bus.Subscription {
	return withDynamicConfigPartSubscriptions(withMobilityPlanSubscriptions(statusSubscriptionsWithWhen(router, []string{"VXLANTunnel"}, "DSLiteTunnel", "PPPoESession", "WireGuardInterface", "TunnelInterface", "Interface", "Bridge", "VXLANTunnel", "FirewallZone", "DHCPv6Server", "IPv6RouterAdvertisement")))
}

func hybridRouteStatusSubscriptions() []bus.Subscription {
	return withDynamicConfigPartSubscriptions(statusSubscriptions("IPv4Route", "IPv4StaticAddress", "BGPPeer", "HealthCheck", "WireGuardInterface", "WireGuardPeer", "TunnelInterface", "Interface", "VirtualAddress", "SAMTransportProfile"))
}

func samStatusSubscriptions() []bus.Subscription {
	return withDynamicConfigPartSubscriptions(withMobilityPlanSubscriptions(statusSubscriptions("IPv4Route", "Sysctl", "WireGuardInterface", "TunnelInterface", "Interface", "VirtualAddress", "DHCPv4Client", "SAMTransportProfile")))
}

func mobilityPlanSubscriptions() []bus.Subscription {
	return []bus.Subscription{{
		Topics: []string{mobilitycontroller.PoolPlanChangedEvent},
		Filter: func(event daemonapi.DaemonEvent) bool {
			if event.Resource == nil || event.Resource.APIVersion != api.MobilityAPIVersion || event.Resource.Kind != "MobilityPool" {
				return false
			}
			return strings.TrimSpace(event.Attributes["source"]) != "" && strings.TrimSpace(event.Attributes["digest"]) != ""
		},
	}}
}

func withMobilityPlanSubscriptions(subscriptions []bus.Subscription) []bus.Subscription {
	return append(subscriptions, mobilityPlanSubscriptions()...)
}

func dynamicConfigPartSubscriptions() []bus.Subscription {
	return []bus.Subscription{{
		Topics: []string{dynamicconfig.PartChangedEvent},
		Filter: func(event daemonapi.DaemonEvent) bool {
			return strings.TrimSpace(event.Attributes["source"]) != "" && strings.TrimSpace(event.Attributes["digest"]) != ""
		},
	}}
}

func withDynamicConfigPartSubscriptions(subscriptions []bus.Subscription) []bus.Subscription {
	return append(subscriptions, dynamicConfigPartSubscriptions()...)
}

func bgpStatusSubscriptions(router *api.Router) []bus.Subscription {
	// SAMTransportProfile status is written only after its DynamicConfigPart is
	// durable.  Waking BGP here applies a newly agreed direct peer group without
	// waiting for its ordinary polling interval.
	return withDynamicConfigPartSubscriptions(withMobilityPlanSubscriptions(statusSubscriptionsWithWhen(router, []string{"BGPRouter", "BGPPeer", "BGPDynamicPeer"}, "BFD", "BGPRouter", "BGPPeer", "SAMTransportProfile")))
}

func samTransportStatusSubscriptions() []bus.Subscription {
	// SAMEnrollmentClient saves its status after atomically persisting an RR
	// snapshot/direct peer group.  Treat that status write as the handoff to the
	// transport controller so direct recovery is not delayed by its 30s poll.
	return withDynamicConfigPartSubscriptions(statusSubscriptions("SAMTransportProfile", "SAMNodeSet", "SAMPeerGroup", "SAMEnrollmentClient", "Interface", "IPv4StaticAddress", "DHCPv4Client", "WireGuardInterface", "WireGuardPeer"))
}

func eventChangedField(event daemonapi.DaemonEvent, field string) bool {
	for _, changed := range strings.Split(event.Attributes["changedFields"], ",") {
		if strings.TrimSpace(changed) == field {
			return true
		}
	}
	return false
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
	DryRunPlugin            bool
	DryRunLeaseSync         bool
	DryRunNAT44SessionSync  bool
	DryRunProviderAction    bool
	DryRunNAT               bool
	DryRunIngress           bool
	DryRunFirewall          bool
	DryRunBGP               bool
	DryRunVRRP              bool
	DryRunPackage           bool
	DryRunSysctl            bool
	DryRunHostname          bool
	DryRunNetworkAdoption   bool
	DryRunBridge            bool
	DryRunVXLANTunnel       bool
	DryRunServiceUnit       bool
	DryRunLogRetention      bool
	DryRunNTP               bool
	SuperviseClientDaemons  bool
	// SkipLegacyClientUnits keeps a one-shot reconciliation from creating
	// per-resource service units when client lifecycle belongs to routerd serve.
	SkipLegacyClientUnits   bool
	SuperviseDNSResolvers   bool
	FirewallDisabled        bool
	DnsmasqCommand          string
	DnsmasqConfig           string
	DnsmasqPID              string
	DnsmasqPort             int
	DnsmasqListen           []string
	NftablesPath            string
	FirewallPath            string
	PathMTUPath             string
	ForceFragmentPath       string
	LedgerPath              string
	NftCommand              string
	BGPDaemonStop           func(context.Context) error
	BGPSocketPath           string
	BGPControlSocketPath    string
	BGPStatePath            string
	ConntrackInterval       time.Duration
	Logger                  *slog.Logger
	ControllerObserver      framework.Observer
	EnabledControllers      []string
	ProviderActionRunner    provideraction.ExecutorRunner
	ProviderInventoryRunner providerinventory.Runner
	PeerGroupSyncClient     *mobilitycontroller.PeerGroupSyncClient
	MutationGate            *sync.RWMutex
	ReconcileHTTPServers    func(context.Context, RuntimeRouterObservation) error
	ReconcileIPsec          func(context.Context, *api.Router) error
}

func ntpControllersForOptions(router *api.Router, eventBus *bus.Bus, store Store, opts Options) (NTPClientController, NTPServerController) {
	return NTPClientController{Router: router, DeclaredRouter: router, Bus: eventBus, Store: store, DryRun: opts.DryRunNTP},
		NTPServerController{Router: router, DeclaredRouter: router, Bus: eventBus, Store: store, DryRun: opts.DryRunNTP}
}

type Runner struct {
	Router              *api.Router
	Bus                 *bus.Bus
	Store               Store
	Opts                Options
	HADecision          *ha.Decision
	CancelServe         context.CancelFunc
	ARPObserverCommands arpObserverCommandPusher

	supervisedMu           sync.Mutex
	clientDaemonContext    context.Context // serve lifetime, not a controller generation
	clientDaemonStates     map[string]supervisedDaemonState
	daemonSourceMu         sync.Mutex
	daemonSourcesEnabled   bool
	daemonSources          map[string]daemonEventSourceState
	arpObserverReadySet    map[string]bool
	generationBuilder      func(context.Context, *slog.Logger, eventedStore, bool, ha.Decision) ([]framework.Controller, DaemonStatusController, controllerLifetimes, error)
	reloadMu               sync.Mutex
	reloadCh               chan generationReload
	routerMu               sync.RWMutex
	preparedRouter         *preparedRouterConfig
	runtimeSnapshot        RuntimeSnapshot
	runtimeOperation       uint64
	runtimeReady           chan struct{}
	runtimeReadyClosed     bool
	runtimeStop            chan struct{}
	runtimeFenced          bool
	runtimeCancelRequested bool
	runtimeDone            chan struct{}
	runtimeExitErr         error
	runtimeCleanupDone     <-chan struct{}
	runtimeRetainedLeases  []*ha.AcquiredLease
	reloadCancelGrace      time.Duration
	startedMu              sync.Mutex
	processStartedAt       time.Time
}

func (r *Runner) startedAt() time.Time {
	r.startedMu.Lock()
	defer r.startedMu.Unlock()
	if r.processStartedAt.IsZero() {
		r.processStartedAt = time.Now().UTC()
	}
	return r.processStartedAt
}

type supervisedDaemonSpec struct {
	ResourceName string
	Binary       string
	Args         []string
	OwnerToken   string
}

type supervisedDaemonState struct {
	Spec   supervisedDaemonSpec
	Cancel context.CancelFunc
	// Done acknowledges supervisor exit, including ownership-marker writes.
	// Cancel remains asynchronous and does not wait for this acknowledgement.
	Done <-chan struct{}
}

// supervisedDaemonMarker is runtime-scoped supervisor provenance. It
// deliberately carries only a digest of argv: PPPoE argv can include a
// password and must never be copied into a marker. RuntimeDir makes this a
// crash-recovery record, not a reboot-surviving adoption claim.
type supervisedDaemonMarker struct {
	Version    int    `json:"version"`
	Binary     string `json:"binary"`
	Resource   string `json:"resource"`
	SpecHash   string `json:"specHash"`
	OwnerToken string `json:"ownerToken"`
	PID        int    `json:"pid"`
}

var supervisedDaemonMarkerRoot = func() string {
	defaults, _ := platform.Current()
	return filepath.Join(defaults.RuntimeDir, "supervised-daemons")
}

type supervisedDaemonProcess struct {
	PID     int
	Command string
}

var supervisedDaemonProcesses = listSupervisedDaemonProcesses
var supervisedDaemonSocketReady = clientSocketReady
var supervisedDaemonSignal = syscall.Kill

func supervisedDaemonSpecHash(spec supervisedDaemonSpec) string {
	h := sha256.New()
	_, _ = h.Write([]byte(spec.Binary))
	for _, arg := range spec.Args {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(arg))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func supervisedDaemonMarkerPath(binary, resource string) string {
	sum := sha256.Sum256([]byte(binary + "\x00" + resource))
	return filepath.Join(supervisedDaemonMarkerRoot(), fmt.Sprintf("%x.json", sum[:]))
}

func readSupervisedDaemonMarkers() (map[string]supervisedDaemonMarker, error) {
	entries, err := os.ReadDir(supervisedDaemonMarkerRoot())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]supervisedDaemonMarker{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]supervisedDaemonMarker{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(supervisedDaemonMarkerRoot(), entry.Name()))
		if err != nil {
			return nil, err
		}
		var marker supervisedDaemonMarker
		if err := json.Unmarshal(data, &marker); err != nil || marker.Version != 1 || marker.Binary == "" || marker.Resource == "" || marker.SpecHash == "" || marker.OwnerToken == "" {
			return nil, fmt.Errorf("read supervised daemon ownership marker %s: invalid record", entry.Name())
		}
		out[supervisedDaemonKey(marker.Binary, marker.Resource)] = marker
	}
	return out, nil
}

func writeSupervisedDaemonMarker(marker supervisedDaemonMarker) error {
	if marker.Version != 1 || marker.Binary == "" || marker.Resource == "" || marker.SpecHash == "" || marker.OwnerToken == "" {
		return errors.New("refusing incomplete supervised daemon ownership marker")
	}
	if err := os.MkdirAll(supervisedDaemonMarkerRoot(), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(supervisedDaemonMarkerRoot(), ".supervised-daemon-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, supervisedDaemonMarkerPath(marker.Binary, marker.Resource))
}

func removeSupervisedDaemonMarker(marker supervisedDaemonMarker) error {
	err := os.Remove(supervisedDaemonMarkerPath(marker.Binary, marker.Resource))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func newSupervisedDaemonOwnerToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", buf), nil
}

type arpObserverCommandPusher interface {
	Status(context.Context, string) (daemonapi.DaemonStatus, error)
	SetIgnoredSenderMACs(context.Context, string, []string) error
	ProbeTarget(context.Context, string, string) error
}

func (r *Runner) effectiveRouter(store eventedStore) (*api.Router, error) {
	prepared, err := r.currentPreparedRouter()
	if err != nil {
		return nil, err
	}
	return resourcequery.FilterRouterByWhen(prepared.startup.Router(), store)
}

func (r *Runner) saveWhenFalseStatuses(store eventedStore) error {
	declared := store.Router
	if declared == nil {
		declared = r.currentRouter()
	}
	if declared == nil {
		return nil
	}
	now := time.Now().UTC()
	for _, res := range declared.Spec.Resources {
		when := resourcequery.ResourceWhen(res)
		if !resourcequery.ResourceWhenPresent(when) {
			continue
		}
		apiVersion := res.APIVersion
		if apiVersion == "" {
			apiVersion = resourcequery.APIVersionForKind(res.Kind)
		}
		matches, err := resourcequery.ResourceWhenMatches(when, store)
		if err != nil {
			return fmt.Errorf("evaluate %s when: %w", res.ID(), err)
		}
		if matches {
			if err := r.clearWhenFalseStatus(apiVersion, res.Kind, res.Metadata.Name, store); err != nil {
				return err
			}
			continue
		}
		indeterminate, err := resourcequery.ResourceWhenIndeterminate(when, store)
		if err != nil {
			return fmt.Errorf("evaluate %s when: %w", res.ID(), err)
		}
		if indeterminate {
			continue
		}
		if preserved, err := r.preserveFreshDaemonStatusWhenFalse(res, apiVersion, store, now); err != nil {
			return err
		} else if preserved {
			continue
		}
		current := store.ObjectStatus(apiVersion, res.Kind, res.Metadata.Name)
		if statusIsWhenFalse(current) {
			next := copyStatusMap(current)
			changed := preserveStaticVirtualAddressCleanupStatus(res, current, next)
			changed = preserveIPv4StaticAddressCleanupStatus(declared, res, current, next) || changed
			changed = preserveIPv6DelegatedAddressCleanupStatus(res, current, next) || changed
			if changed {
				if err := store.SaveObjectStatus(apiVersion, res.Kind, res.Metadata.Name, next); err != nil {
					return err
				}
			}
			continue
		}
		status := map[string]any{
			"phase":      "Pending",
			"reason":     "WhenFalse",
			"observedAt": now.Format(time.RFC3339Nano),
		}
		if res.Kind == "HealthCheck" {
			for _, key := range []string{"routing", "observed"} {
				if value, ok := current[key]; ok {
					status[key] = value
				}
			}
		}
		preserveStaticVirtualAddressCleanupStatus(res, current, status)
		preserveIPv4StaticAddressCleanupStatus(declared, res, current, status)
		preserveIPv6DelegatedAddressCleanupStatus(res, current, status)
		if err := store.SaveObjectStatus(apiVersion, res.Kind, res.Metadata.Name, status); err != nil {
			return err
		}
	}
	return nil
}

// preserveStaticVirtualAddressCleanupStatus retains only the ownership data
// needed by the VRRP controller to remove a static address after its when
// condition becomes false. The resource is filtered out of the effective
// router before VRRP reconciliation, so dropping these fields would leave the
// address configured on the host indefinitely.
func preserveStaticVirtualAddressCleanupStatus(res api.Resource, current, status map[string]any) bool {
	if res.Kind != "VirtualAddress" {
		return false
	}
	spec, err := res.VirtualAddressSpec()
	if err != nil || (strings.TrimSpace(spec.Mode) != "" && spec.Mode != "static") {
		return false
	}
	changed := false
	for _, key := range []string{"backend", "ifname", "appliedAddress"} {
		if value := statusvalue.Field(current, key); value != "" {
			if statusvalue.Field(status, key) == "" {
				status[key] = value
				changed = true
			}
		}
	}
	// A status written by older routerd versions may already be
	// Pending/WhenFalse and have lost the ownership fields above. Keep the
	// declared address and logical interface so the VRRP controller can perform
	// an idempotent one-time cleanup of that legacy address.
	for key, value := range map[string]string{"address": spec.Address, "interface": spec.Interface} {
		if strings.TrimSpace(value) != "" && statusvalue.Field(status, key) == "" {
			status[key] = value
			changed = true
		}
	}
	return changed
}

// Display metadata is not native authority. Preserve only the receipt written
// by the actual address owner; a declaration cannot manufacture ownership.
func preserveIPv4StaticAddressCleanupStatus(_ *api.Router, res api.Resource, current, status map[string]any) bool {
	if res.Kind != "IPv4StaticAddress" {
		return false
	}
	changed := false
	for _, key := range []string{"ifname", "nativeReceipt"} {
		if value, present := current[key]; present {
			if _, exists := status[key]; !exists {
				status[key] = value
				changed = true
			}
		}
	}
	return changed
}

// preserveIPv6DelegatedAddressCleanupStatus keeps only the ownership data
// needed by the LAN address controller after the resource is filtered out of
// the effective router. In particular, an address staged while the node was
// BACKUP must not remain Applied once its own when condition resolves false.
func preserveIPv6DelegatedAddressCleanupStatus(res api.Resource, current, status map[string]any) bool {
	if res.Kind != "IPv6DelegatedAddress" {
		return false
	}
	spec, err := res.IPv6DelegatedAddressSpec()
	if err != nil {
		return false
	}
	changed := false
	for _, key := range []string{"address", "previousAddresses", "nativeReceipt"} {
		value, exists := current[key]
		if !exists || value == nil {
			continue
		}
		if _, exists := status[key]; !exists {
			status[key] = value
			changed = true
		}
	}
	for key, value := range map[string]string{"interface": spec.Interface, "prefixSource": spec.PrefixDelegation} {
		if strings.TrimSpace(value) != "" && statusvalue.Field(status, key) == "" {
			status[key] = value
			changed = true
		}
	}
	return changed
}

func (r *Runner) clearWhenFalseStatus(apiVersion, kind, name string, store eventedStore) error {
	current := store.ObjectStatus(apiVersion, kind, name)
	reason := strings.TrimSpace(fmt.Sprint(current["reason"]))
	if reason != "WhenFalse" {
		return nil
	}
	if kind == "DHCPv6PrefixDelegation" {
		reader, ok := store.Store.(routerstate.PDLeaseReader)
		if !ok {
			return fmt.Errorf("PD observation reader unavailable")
		}
		if _, found, err := reader.ReadPDLease(name); err != nil {
			return err
		} else if !found {
			return nil
		}
		next := copyStatusMap(current)
		delete(next, "reason")
		for _, key := range []string{"phase", "health", "conditions"} {
			delete(next, key)
		}
		return store.SaveObjectStatus(apiVersion, kind, name, next)
	}

	if kind == "HealthCheck" {
		// A condition becoming true permits a new probe. Retained history
		// cannot establish current reachability or the current probe path.
		return nil
	}

	observed := statusValueMap(current["observed"])
	if len(observed) == 0 || strings.TrimSpace(fmt.Sprint(observed["phase"])) == "" {
		return nil
	}
	next := copyStatusMap(current)
	for key, value := range observed {
		next[key] = value
	}
	delete(next, "reason")
	next["observed"] = observed
	return store.SaveObjectStatus(apiVersion, kind, name, next)
}

func (r *Runner) preserveFreshDaemonStatusWhenFalse(res api.Resource, apiVersion string, store eventedStore, now time.Time) (bool, error) {
	if res.Kind != "HealthCheck" {
		return false, nil
	}
	status, err := store.ReadObjectStatus(apiVersion, res.Kind, res.Metadata.Name)
	if err != nil {
		return false, err
	}
	switch statusvalue.Field(status, "phase") {
	case healthcheck.PhaseHealthy, healthcheck.PhasePassing, healthcheck.PhaseFailing, healthcheck.PhaseUnhealthy:
	default:
		return false, nil
	}
	observation, err := store.HealthObservation(res.Metadata.Name)
	if err != nil {
		return false, err
	}
	spec, err := res.HealthCheckSpec()
	if err != nil {
		return false, err
	}
	router := store.Router
	if router == nil {
		router = r.currentRouter()
	}
	resolved, err := healthcheck.ResolveSpecWithStoreForResource(router, store, res.Metadata.Name, spec)
	if err != nil {
		return false, err
	}
	return observation.Phase == statusvalue.Field(status, "phase") && observation.Probe == resolved.Probe() && observation.Current(now), nil
}

func statusIsWhenFalse(status map[string]any) bool {
	return strings.TrimSpace(fmt.Sprint(status["reason"])) == "WhenFalse"
}

func (r *Runner) effectiveRouterForReconcile(store eventedStore) (routerObservation, error) {
	prepared, err := r.currentPreparedRouter()
	if err != nil {
		return routerObservation{}, err
	}
	store = store.withRouter(prepared.declared)
	if err := r.saveWhenFalseStatuses(store); err != nil {
		return routerObservation{}, err
	}
	effective, err := resourcequery.FilterRouterByWhen(prepared.staticSAM, store)
	if err != nil {
		return routerObservation{}, err
	}
	if prepared.timedSAM {
		effective, err = resolveWireGuardSAMResources(effective, time.Now().UTC())
		if err != nil {
			return routerObservation{}, err
		}
	}
	return routerObservation{DeclaredRouter: prepared.declared, EffectiveRouter: effective}, nil
}

func (r *Runner) effectiveDynamicRouterForReconcile(store eventedStore, now time.Time, targetOS platform.OS) (*api.Router, error) {
	view, err := r.effectiveDynamicRouteSAMViewForReconcile(store, now, targetOS)
	if err != nil {
		return nil, err
	}
	return view.EffectiveRouter, nil
}

func (r *Runner) Start(ctx context.Context) error {
	if err := r.validatePFNATOptions(platform.CurrentOS(), r.Opts); err != nil {
		return err
	}
	r.clientDaemonContext = ctx
	if r.currentRouter() == nil || r.Bus == nil || r.Store == nil {
		return fmt.Errorf("router, bus, and store are required")
	}
	if _, err := r.currentPreparedRouter(); err != nil {
		return err
	}
	logger := r.Opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	store := eventedStore{Store: r.Store, Bus: r.Bus, Router: r.currentRouter()}

	first, err := r.prepareControllerGeneration(ctx, logger, store)
	if err != nil {
		return fmt.Errorf("start initial controller generation: %w", err)
	}
	r.daemonSourceMu.Lock()
	r.daemonSourcesEnabled = true
	r.daemonSourceMu.Unlock()
	if err := r.refreshDaemonEventSources(ctx); err != nil {
		r.stopDaemonEventSources()
		return errors.Join(err, r.closeControllerGeneration(first))
	}
	r.reloadMu.Lock()
	r.runtimeDone = make(chan struct{})
	r.reloadMu.Unlock()
	go func() {
		err := r.runControllerGenerations(ctx, logger, store, first)
		r.stopDaemonEventSources()
		r.waitControllerCleanup()
		if err != nil && ctx.Err() == nil {
			// A running routerd without its controller generation is unsafe.
			// Cancel the serve runtime so the service manager can restart it.
			logger.Error("controller generation supervisor stopped; fencing serve runtime", "error", err)
			r.fenceRuntime(true)
		}
		r.reloadMu.Lock()
		if err != nil && !errors.Is(err, context.Canceled) {
			r.runtimeExitErr = errors.Join(r.runtimeExitErr, err)
		}
		close(r.runtimeDone)
		r.reloadMu.Unlock()
	}()
	select {
	case <-r.runtimeReadyChannel():
		return r.RuntimeMutationError()
	case <-ctx.Done():
		return ctx.Err()
	}
}

type controllerGeneration struct {
	router           *api.Router
	ctx              context.Context
	cancel           context.CancelFunc
	decision         ha.Decision
	controllers      []framework.Controller
	daemonStatusSync DaemonStatusController
	heartbeatStarted bool
	activated        bool
	lifetimes        controllerLifetimes
}

// These auxiliary consumers have timer callbacks that can publish
// effects. A replacement generation waits for their actual exit after cancel.
type controllerLifetimes struct {
	eventRule         *eventrule.Controller
	derivedEvent      *derived.Controller
	eventRuleDone     <-chan struct{}
	derivedDone       <-chan struct{}
	eventSubscription *eventsubscriptioncontroller.Controller
	pipeline          *observabilitypipeline.Controller
	bgp               *bgpcontroller.Controller
	advertisements    *RouterAdvertisementService
}

// Activate only after the generation is the selected runtime. Preparation and
// bootstrap must not let a candidate auxiliary publisher emit its own effects.
func (l *controllerLifetimes) activate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if l.eventRule != nil {
		var err error
		l.eventRuleDone, err = l.eventRule.Start(ctx)
		if err != nil {
			return err
		}
	}
	if l.derivedEvent != nil {
		var err error
		l.derivedDone, err = l.derivedEvent.Start(ctx)
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

// RuntimeShutdownGrace includes the required RA multicast spacing and final
// withdrawal write. Expiry fences admission; actual exit still owns the lease.
const RuntimeShutdownGrace = 8 * time.Second

func (l controllerLifetimes) wait() error {
	ctx, cancel := context.WithTimeout(context.Background(), RuntimeShutdownGrace)
	defer cancel()
	return l.waitContext(ctx)
}

func (l controllerLifetimes) waitContext(ctx context.Context) error {
	if l.eventSubscription != nil {
		l.eventSubscription.Stop()
	}
	if l.eventRuleDone != nil {
		<-l.eventRuleDone
	}
	if l.derivedDone != nil {
		<-l.derivedDone
	}
	var failures []error
	if l.bgp != nil {
		if err := l.bgp.Stop(ctx); err != nil {
			failures = append(failures, fmt.Errorf("stop BGP controller: %w", err))
		}
	}
	if l.advertisements != nil {
		if err := l.advertisements.Stop(ctx); err != nil {
			failures = append(failures, fmt.Errorf("stop router advertisements: %w", err))
		}
	}
	if l.pipeline != nil {
		if err := l.pipeline.Stop(ctx); err != nil {
			failures = append(failures, fmt.Errorf("stop observability pipeline: %w", err))
		}
	}
	return errors.Join(failures...)
}

func (r *Runner) closeControllerLifetimes(lifetimes controllerLifetimes, releaseLease func() error) error {
	if err := lifetimes.wait(); err != nil {
		done := make(chan struct{})
		r.reloadMu.Lock()
		r.runtimeCleanupDone = done
		r.runtimeExitErr = errors.Join(r.runtimeExitErr, err)
		r.reloadMu.Unlock()
		r.fenceRuntime(true)
		go func() {
			defer close(done)
			if stopErr := lifetimes.waitContext(context.Background()); stopErr != nil {
				slog.Error("unconfirmed controller cleanup failed; retaining cluster lease", "error", stopErr)
				return
			}
			if releaseLease != nil {
				if closeErr := releaseLease(); closeErr != nil {
					slog.Error("release cluster lease after controller exit", "error", closeErr)
				}
			}
		}()
		return err
	}
	if releaseLease != nil {
		return releaseLease()
	}
	return nil
}

func generationLeaseRelease(decision ha.Decision) func() error {
	if decision.Lease == nil {
		return nil
	}
	return decision.Lease.Close
}

func (r *Runner) prepareControllerGeneration(ctx context.Context, logger *slog.Logger, store eventedStore) (*controllerGeneration, error) {
	return r.prepareControllerGenerationWithGate(ctx, logger, store, r.Opts.MutationGate)
}

func (r *Runner) prepareControllerGenerationWithGate(ctx context.Context, logger *slog.Logger, store eventedStore, gate *sync.RWMutex) (*controllerGeneration, error) {
	return r.prepareControllerGenerationForRequest(ctx, nil, logger, store, gate)
}

func (r *Runner) prepareControllerGenerationForRequest(ctx, requestCtx context.Context, logger *slog.Logger, store eventedStore, gate *sync.RWMutex) (*controllerGeneration, error) {
	return r.prepareControllerGenerationOwned(ctx, requestCtx, logger, store, gate, nil, false)
}

func (r *Runner) prepareControllerGenerationOwned(ctx, requestCtx context.Context, logger *slog.Logger, store eventedStore, gate *sync.RWMutex, admitted *ha.Decision, strict bool) (*controllerGeneration, error) {
	if err := r.RuntimeMutationError(); err != nil {
		return nil, err
	}
	r.startedAt()
	generationCtx, cancel := context.WithCancel(ctx)
	// The request may cancel preparation, but a confirmed generation belongs
	// to serve. Detach this callback before publishing a successful generation.
	stopRequestCancel := func() bool { return true }
	requestCanceled := make(chan struct{})
	if requestCtx != nil {
		stopRequestCancel = context.AfterFunc(requestCtx, func() { cancel(); close(requestCanceled) })
	}
	defer stopRequestCancel()
	router := r.currentRouter()
	var decision ha.Decision
	var err error
	if admitted != nil {
		decision = *admitted
	} else {
		decision, err = acquireClusterLease(generationCtx, router, store)
	}
	releaseLease := generationLeaseRelease(decision)
	if admitted != nil {
		releaseLease = nil
	}
	if err != nil {
		cancel()
		return nil, err
	}
	if admitted != nil {
		if err := recordRuntimeHADecision(router, store, decision); err != nil {
			cancel()
			return nil, err
		}
	}
	builder := r.frameworkControllers
	if r.generationBuilder != nil {
		builder = r.generationBuilder
	}
	controllers, daemonStatusSync, lifetimes, err := builder(generationCtx, logger, store, true, decision)
	if err == nil {
		err = generationCtx.Err()
	}
	if err != nil {
		cancel()
		if stopErr := r.closeControllerLifetimes(lifetimes, releaseLease); stopErr != nil {
			r.fenceRuntime(true)
			err = errors.Join(err, stopErr)
		}
		return nil, err
	}
	if admitted != nil && decision.Enabled {
		owned, authorityErr := decision.Lease.OwnsPath(decision.Lease.Config.LeasePath)
		if authorityErr != nil || !owned {
			cancel()
			r.fenceRuntime(true)
			stopErr := r.closeControllerLifetimes(lifetimes, nil)
			return nil, errors.Join(authorityErr, errors.New("admitted HA lease no longer owns its pathname"), stopErr)
		}
	}
	heartbeatStarted := false
	if decision.Enabled && decision.Leader && decision.Lease != nil {
		heartbeatStarted = true
		go decision.Lease.Heartbeat(generationCtx, func(err error) {
			logger.Error("routerd cluster lease heartbeat failed during generation bootstrap; fencing generation", "error", err)
			cancel()
		})
	}
	bootstrap := framework.Runner{Bus: r.Bus, MutationGate: gate, Logger: logger, Interval: 30 * time.Second, Observer: r.Opts.ControllerObserver}
	var reconcileErr error
	if strict {
		reconcileErr = bootstrap.RunOnce(generationCtx, controllers...)
	} else {
		err = bootstrap.Bootstrap(generationCtx, controllers...)
	}
	if !stopRequestCancel() {
		<-requestCanceled
	}
	if err == nil {
		err = generationCtx.Err()
	}
	if err == nil {
		r.daemonSourceMu.Lock()
		sourcesEnabled := r.daemonSourcesEnabled
		r.daemonSourceMu.Unlock()
		if sourcesEnabled {
			err = r.refreshDaemonEventSources(generationCtx)
		}
	}
	if err != nil {
		cancel()
		if stopErr := r.closeControllerLifetimes(lifetimes, releaseLease); stopErr != nil {
			r.fenceRuntime(true)
			err = errors.Join(err, stopErr)
		}
		return nil, err
	}
	return &controllerGeneration{
		router:           router,
		ctx:              generationCtx,
		cancel:           cancel,
		decision:         decision,
		controllers:      controllers,
		daemonStatusSync: daemonStatusSync,
		heartbeatStarted: heartbeatStarted,
		lifetimes:        lifetimes,
	}, reconcileErr
}

func (r *Runner) runControllerGenerations(ctx context.Context, logger *slog.Logger, store eventedStore, generation *controllerGeneration) (err error) {
	reloads := r.runtimeReloadChannel()
	stopAdmission := context.AfterFunc(ctx, func() { r.fenceRuntime(false) })
	defer stopAdmission()
	var pending *generationReload
	var reloadErr error
	var restored bool
	teardownUnconfirmed := false
	stopCurrentGeneration := func() error {
		current := generation
		generation = nil
		stopErr := r.closeControllerGeneration(current)
		if stopErr != nil {
			teardownUnconfirmed = true
		}
		return stopErr
	}
	defer func() {
		if stopErr := stopCurrentGeneration(); stopErr != nil {
			err = errors.Join(err, stopErr)
		}
		r.clearRuntimeGeneration(!teardownUnconfirmed)
		r.fenceRuntime(err != nil && ctx.Err() == nil)
		r.markRuntimeReady()
		if pending != nil {
			r.acknowledgeRuntimeReload(*pending, restored, errors.Join(reloadErr, runtimeStoppedError(err)))
		}
	}()
	for ctx.Err() == nil {
		if generation == nil {
			select {
			case request := <-reloads:
				if request.apply {
					r.acknowledgeRuntimeReload(request, false, errors.New("selected runtime is temporarily unavailable"))
					continue
				}

				pending = &request
				generation, restored, reloadErr = r.prepareRuntimeReload(ctx, logger, store, request)
				if generation == nil {
					return runtimeStoppedError(reloadErr)
				}
			default:
				generation, err = r.prepareControllerGeneration(ctx, logger, store.withRouter(r.currentRouter()))
				if err != nil {
					return err
				}
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if r.RuntimeMutationError() != nil {
			return ErrRuntimeMutationStopped
		}
		if generation.ctx.Err() != nil {
			if err := stopCurrentGeneration(); err != nil {
				return err
			}
			r.clearRuntimeGeneration(false)
			if pending != nil {
				return context.Canceled
			}
			continue
		}
		if r.controllerEnabled("daemon-status") {
			r.warmDaemonStatuses(generation.ctx, generation.daemonStatusSync, logger)
		}
		if generation.decision.Enabled && generation.decision.Leader && generation.decision.Lease != nil && !generation.heartbeatStarted {
			current := generation
			go current.decision.Lease.Heartbeat(current.ctx, func(err error) {
				logger.Error("routerd cluster lease heartbeat failed; fencing controller generation", "error", err)
				current.cancel()
			})
		} else if generation.decision.Enabled && !generation.decision.Leader {
			time.AfterFunc(clusterRetryInterval(r.currentRouter()), generation.cancel)
		}
		if !r.publishRuntimeGeneration(generation) {
			generation.cancel()
			failure := runtimeStoppedError(ctx.Err())
			if stopErr := stopCurrentGeneration(); stopErr != nil {
				return errors.Join(failure, stopErr)
			}
			return failure
		}
		if !generation.activated {
			if err := generation.lifetimes.activate(generation.ctx); err != nil {
				r.clearRuntimeGeneration(false)
				generation.cancel()
				stopErr := stopCurrentGeneration()
				r.fenceRuntime(true)
				return errors.Join(err, stopErr)
			}
			generation.activated = true
		}
		loop := framework.Runner{Bus: r.Bus, MutationGate: r.Opts.MutationGate, Logger: logger, Interval: 30 * time.Second, Observer: r.Opts.ControllerObserver, SkipBootstrap: true}
		loopDone := make(chan error, 1)
		loopCtx, stopLoop := context.WithCancel(generation.ctx)
		go func(current *controllerGeneration) {
			loopDone <- loop.Run(loopCtx, current.controllers...)
		}(generation)
		r.markRuntimeReady()
		if pending != nil {
			r.acknowledgeRuntimeReload(*pending, restored, reloadErr)
			pending = nil
		}
	waitForGeneration:
		select {
		case <-ctx.Done():
			stopLoop()
			r.clearRuntimeGeneration(false)
			generation.cancel()
			<-loopDone
			if stopErr := stopCurrentGeneration(); stopErr != nil {
				return errors.Join(ctx.Err(), stopErr)
			}
			return ctx.Err()
		case <-generation.ctx.Done():
			stopLoop()
			r.clearRuntimeGeneration(false)
			<-loopDone
			if err := stopCurrentGeneration(); err != nil {
				return err
			}
			r.clearRuntimeGeneration(false)
		case request := <-reloads:
			if request.ctx != nil && request.ctx.Err() != nil {
				r.acknowledgeRuntimeReload(request, false, request.ctx.Err())
				goto waitForGeneration
			}
			if request.apply {
				admission, standby, admitErr := r.admitRuntimeApply(request.ctx, generation, request.router, store)
				if admitErr == nil && !standby {
					admitErr = request.ctx.Err()
					if admitErr == nil {
						admitErr = generation.ctx.Err()
					}
					if admitErr == nil {
						admitErr = request.commit()
						admitErr = errors.Join(admitErr, r.validateSelectedApplyOwner(generation))
					}
				}
				if admitErr != nil || standby {
					admission.closeNew()
					request.done <- generationReloadResult{outcome: RuntimeReloadOutcome{Active: r.ActiveRuntimeSnapshot(), Accepted: true, OperationID: request.id, Standby: standby}, err: admitErr}
					goto waitForGeneration
				}
				stopLoop()
				<-loopDone
				pending = &request
				generation, restored, reloadErr = r.applyAdmittedRuntime(ctx, logger, store, generation, request, admission)
				if generation == nil {
					return runtimeStoppedError(reloadErr)
				}
				continue
			}
			pending = &request
			stopLoop()
			r.clearRuntimeGeneration(false)
			generation.cancel()
			<-loopDone
			if err := stopCurrentGeneration(); err != nil {
				return err
			}
			r.clearRuntimeGeneration(false)
			generation, restored, reloadErr = r.prepareRuntimeReload(ctx, logger, store, request)
			if generation == nil {
				return runtimeStoppedError(reloadErr)
			}
		}
	}
	return ctx.Err()
}

func (r *Runner) prepareReloadedControllerGeneration(ctx context.Context, logger *slog.Logger, store eventedStore, router *api.Router) (*controllerGeneration, error) {
	generation, _, err := r.prepareRuntimeReload(ctx, logger, store, generationReload{router: router})
	return generation, err
}

func (r *Runner) prepareRuntimeReload(ctx context.Context, logger *slog.Logger, store eventedStore, request generationReload) (*controllerGeneration, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := r.RuntimeMutationError(); err != nil {
		return nil, false, err
	}
	previous, err := r.currentPreparedRouter()
	if err != nil {
		return nil, false, err
	}
	prepared, err := prepareRouterConfig(request.router, platform.CurrentOS())
	var next *controllerGeneration
	if err == nil {
		r.publishPreparedRouter(prepared)
		next, err = r.prepareControllerGenerationForRequest(ctx, request.ctx, logger, store.withRouter(request.router), nil)
	}
	if err == nil {
		return next, false, nil
	}
	if ctx.Err() != nil || r.RuntimeMutationError() != nil {
		return nil, false, errors.Join(err, ctx.Err(), r.RuntimeMutationError())
	}
	r.publishPreparedRouter(previous)
	rollback, rollbackErr := r.prepareControllerGenerationWithGate(ctx, logger, store.withRouter(previous.declared), nil)
	if rollbackErr != nil {
		return nil, false, errors.Join(err, fmt.Errorf("restore previous generation: %w", rollbackErr))
	}
	return rollback, true, err
}

func (r *Runner) closeControllerGeneration(generation *controllerGeneration) error {
	if generation == nil {
		return nil
	}
	generation.cancel()
	return r.closeControllerLifetimes(generation.lifetimes, generationLeaseRelease(generation.decision))
}

func clusterRetryInterval(router *api.Router) time.Duration {
	_, spec, ok, _ := routerdClusterResource(router)
	if !ok {
		return 10 * time.Second
	}
	ttl := 30 * time.Second
	if parsed, err := time.ParseDuration(strings.TrimSpace(spec.LeaseTTL)); err == nil && parsed > 0 {
		ttl = parsed
	}
	interval := ttl / 3
	if interval < time.Second {
		interval = time.Second
	}
	return interval
}

func (r *Runner) ReconcileOnce(ctx context.Context) (err error) {
	if err := r.RuntimeMutationError(); err != nil {
		return err
	}
	if err := r.validatePFNATOptions(platform.CurrentOS(), r.Opts); err != nil {
		return err
	}
	if r.currentRouter() == nil || r.Bus == nil || r.Store == nil {
		return fmt.Errorf("router, bus, and store are required")
	}
	logger := r.Opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	store := eventedStore{Store: r.Store, Bus: r.Bus, Router: r.currentRouter()}
	decision, closeLease, err := r.oneShotHADecision(ctx, store)
	if err != nil {
		return err
	}
	controllers, _, lifetimes, err := r.frameworkControllers(ctx, logger, store, false, decision)
	defer func() {
		if stopErr := r.closeControllerLifetimes(lifetimes, closeLease); stopErr != nil {
			r.fenceRuntime(true)
			err = errors.Join(err, stopErr)
		}
	}()
	if err != nil {
		return err
	}
	loop := framework.Runner{MutationGate: r.Opts.MutationGate, Exclusive: true, Logger: logger, Interval: 30 * time.Second, Observer: r.Opts.ControllerObserver}
	return loop.RunOnce(ctx, controllers...)
}

func (r *Runner) ReconcileScheduled(ctx context.Context) (err error) {
	if err := r.RuntimeMutationError(); err != nil {
		return err
	}
	if err := r.validatePFNATOptions(platform.CurrentOS(), r.Opts); err != nil {
		return err
	}
	if r.currentRouter() == nil || r.Bus == nil || r.Store == nil {
		return fmt.Errorf("router, bus, and store are required")
	}
	logger := r.Opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	store := eventedStore{Store: r.Store, Bus: r.Bus, Router: r.currentRouter()}
	decision, closeLease, err := r.oneShotHADecision(ctx, store)
	if err != nil {
		return err
	}
	controllers, _, lifetimes, err := r.frameworkControllers(ctx, logger, store, false, decision)
	defer func() {
		if stopErr := r.closeControllerLifetimes(lifetimes, closeLease); stopErr != nil {
			r.fenceRuntime(true)
			err = errors.Join(err, stopErr)
		}
	}()
	if err != nil {
		return err
	}
	controllers = filterScheduledReconcileControllers(controllers)
	loop := framework.Runner{MutationGate: r.Opts.MutationGate, Exclusive: true, Logger: logger, Interval: 30 * time.Second, Observer: r.Opts.ControllerObserver}
	return loop.RunOnce(ctx, controllers...)
}

func filterScheduledReconcileControllers(controllers []framework.Controller) []framework.Controller {
	out := make([]framework.Controller, 0, len(controllers))
	for _, controller := range controllers {
		if scheduledReconcileSkipsController(controller.Name()) {
			continue
		}
		out = append(out, controller)
	}
	return out
}

func scheduledReconcileSkipsController(name string) bool {
	switch name {
	case "package", "kernel-module", "sysctl", "hostname", "network-adoption", "service-unit", "log-retention", "ntp-client", "ntp-server",
		// These controllers already run from the serve event loop with their own
		// periodic cadence and status subscriptions. Running them again from
		// --apply-interval makes the scheduled pass probe or rewrite host state
		// without adding a stronger convergence signal.
		"link", "tunnel", "wireguard", "vrf", "ipv4-static-address", "vxlan-segment", "ipv4-policy-route", "ipv4-route", "hybrid-route",
		"sam", "sam-enrollment-client", "sam-transport", "path-mtu", "dslite", "dhcpv6-information", "dhcpv6-address", "lan-address",
		"dhcpv6-server", "dhcpv4-lease", "pppoe-session", "dns-resolver", "event-federation", "event-subscription", "plugin",
		"mobility-discovery", "mobility-arp-request", "mobility", "mobility-shard", "provider-action-execution", "egress-route-policy",
		"ingress-service", "nat44", "bfd", "bgp", "vrrp", "ip-address-set", "firewall", "observability-pipeline",
		"daemon-status", "daemon-supervisor", "daemon-supervisor-reconcile", "dhcp-lease-sync", "nat44-session-sync":
		return true
	default:
		return false
	}
}

func (r *Runner) oneShotHADecision(ctx context.Context, store eventedStore) (ha.Decision, func() error, error) {
	r.startedAt()
	if r.HADecision != nil {
		return *r.HADecision, func() error { return nil }, nil
	}
	decision, err := acquireClusterLease(ctx, r.currentRouter(), store)
	if err != nil {
		return decision, nil, err
	}
	return decision, generationLeaseRelease(decision), nil
}

func (r *Runner) frameworkControllers(ctx context.Context, logger *slog.Logger, store eventedStore, startAuxiliary bool, haDecision ha.Decision) ([]framework.Controller, DaemonStatusController, controllerLifetimes, error) {
	var lifetimes controllerLifetimes
	if _, err := r.currentPreparedRouter(); err != nil {
		return nil, DaemonStatusController{}, lifetimes, err
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
		opts.DryRunPlugin = true
		opts.DryRunLeaseSync = true
		opts.DryRunNAT44SessionSync = true
		opts.DryRunProviderAction = true
		opts.DryRunNAT = true
		opts.DryRunIngress = true
		opts.DryRunFirewall = true
		opts.DryRunBGP = true
		opts.DryRunVRRP = true
		opts.DryRunPackage = true
		opts.DryRunSysctl = true
		opts.DryRunHostname = true
		opts.DryRunNetworkAdoption = true
		opts.DryRunBridge = true
		opts.DryRunVXLANTunnel = true
		opts.DryRunServiceUnit = true
		opts.DryRunLogRetention = true
		opts.DryRunNTP = true
		opts.ReconcileIPsec = nil
	}
	// Keep Runner.Opts immutable across generations. Standby dry-run flags are
	// effective only for this generation and must not poison a later leader
	// generation after lease acquisition. Keep the options local rather than
	// copying Runner: Runner owns synchronization and runtime state.
	if err := r.validatePFNATOptions(platform.CurrentOS(), opts); err != nil {
		return nil, DaemonStatusController{}, lifetimes, err
	}
	packages := PackageController{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunPackage}
	sysctl := SysctlController{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunSysctl}
	hostname := HostnameController{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunHostname}
	kernelModules := KernelModuleController{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunPackage}
	adoption := NetworkAdoptionController{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunNetworkAdoption}
	bridge := BridgeController{Router: r.currentRouter(), Store: store, DryRun: opts.DryRunBridge}
	serviceUnits := SystemdUnitController{Router: r.currentRouter(), DeclaredRouter: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunServiceUnit, SynthesizeClientDaemonUnits: !opts.SuperviseClientDaemons && !opts.SkipLegacyClientUnits, BGPSocketPath: opts.BGPSocketPath, BGPControlSocketPath: opts.BGPControlSocketPath, BGPStatePath: opts.BGPStatePath}
	logRetention := LogRetentionController{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunLogRetention}
	logRetention.Data, _ = r.Store.(logRetentionData)
	ntpClient, ntpServer := ntpControllersForOptions(r.currentRouter(), r.Bus, store, opts)
	info := DHCPv6InformationController{Router: r.currentRouter(), Bus: r.Bus, Store: store, DaemonSockets: opts.DaemonSockets, Logger: logger}
	link := LinkController{Router: r.currentRouter(), Store: store}
	tunnel := TunnelInterfaceController{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunRoute, OS: platform.CurrentOS(), Logger: logger}
	wireGuard := WireGuardController{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunRoute, Logger: logger}
	vrfs := VRFController{Router: r.currentRouter(), Store: store, DryRun: opts.DryRunRoute}
	nativeMobilityMu := &sync.Mutex{}
	vxlanParent := &VXLANSegmentController{Store: store, StartedAt: r.startedAt()}
	dsliteParent := &DSLiteTunnelController{ResolverPort: opts.DnsmasqPort}
	ipv4Static := IPv4StaticAddressController{Router: r.currentRouter(), DeclaredRouter: r.currentRouter(), WhenRouter: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunAddress, Logger: logger, Segments: vxlanParent, DSLite: dsliteParent, Bridges: &bridge, WireGuard: &wireGuard, VRFs: &vrfs}
	lan := LANAddressController{Router: r.currentRouter(), DeclaredRouter: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunAddress, Logger: logger, Segments: vxlanParent, DSLite: dsliteParent, Bridges: &bridge, WireGuard: &wireGuard, VRFs: &vrfs}
	dslite := DSLiteTunnelController{Router: r.currentRouter(), DeclaredRouter: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunDSLite, ResolverPort: opts.DnsmasqPort, Logger: logger}
	route := IPv4RouteController{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunRoute, Logger: logger, Segments: vxlanParent, DSLite: dsliteParent, Bridges: &bridge, WireGuard: &wireGuard, VRFs: &vrfs, Tunnels: &tunnel}
	hybridRoute := HybridRouteController{Router: r.currentRouter(), EffectiveRouter: r.currentRouter(), Store: store}
	samController := SAMController{Router: r.currentRouter(), Store: store, DryRun: opts.DryRunRoute}
	policyRoute := IPv4PolicyRouteController{Router: r.currentRouter(), DeclaredRouter: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunRoute, NftCommand: opts.NftCommand, Logger: logger}
	pathMTU := PathMTUController{Router: r.currentRouter(), OS: platform.CurrentOS(), Bus: r.Bus, Store: store, DryRun: opts.DryRunRoute, NftCommand: opts.NftCommand, Path: opts.PathMTUPath, ForceFragmentPath: opts.ForceFragmentPath}
	dhcpv6 := DHCPv6ServerController{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunDHCPv6, Command: opts.DnsmasqCommand, ConfigPath: opts.DnsmasqConfig, PIDFile: opts.DnsmasqPID, Port: opts.DnsmasqPort, ListenAddresses: opts.DnsmasqListen, Logger: logger}
	if r.controllerEnabled("dhcpv6-server") {
		lifetimes.advertisements = NewRouterAdvertisementService(ctx, opts.DryRunDHCPv6)
		lifetimes.advertisements.segments = vxlanParent
		lifetimes.advertisements.dslite = dsliteParent
		lifetimes.advertisements.bridges = &bridge
		lifetimes.advertisements.wireGuard = &wireGuard
		lifetimes.advertisements.vrfs = &vrfs
		dhcpv6.Advertisements = lifetimes.advertisements
	}
	dhcp4Client := dhcpv4client.Controller{Router: r.currentRouter(), Bus: r.Bus, Store: store, DaemonSockets: opts.DaemonSockets, DryRun: opts.DryRunDHCPv4Client, Logger: logger}
	pppoeSession := pppoesession.Controller{Router: r.currentRouter(), Bus: r.Bus, Store: store, DaemonSockets: opts.DaemonSockets, DryRun: opts.DryRunPPPoESession, Logger: logger}
	reconcilePPPoESessions := func(ctx context.Context) error {
		prepared, err := r.currentPreparedRouter()
		if err != nil {
			return err
		}
		current := pppoeSession
		current.Router = prepared.startup.Router()
		current.Store = store.withRouter(prepared.declared)
		current.Source = func(name string) (pppoesession.Source, bool, error) {
			return r.currentPPPoESource(prepared.declared, name, store)
		}
		for _, resource := range current.Router.Spec.Resources {
			if resource.Kind == "PPPoESession" {
				if err := current.Reconcile(ctx, resource.Metadata.Name); err != nil {
					return err
				}
			}
		}
		return nil
	}
	defaults, features := platform.Current()
	plugins := &plugincontroller.Controller{}
	if r.controllerEnabled("plugin") {
		pluginStore, _ := r.Store.(plugincontroller.DataStore)
		var err error
		plugins, err = plugincontroller.New(r.currentRouter(), pluginStore, r.Bus, opts.DryRunPlugin)
		if err != nil {
			return nil, DaemonStatusController{}, lifetimes, err
		}
	}
	dnsResolver := dnsresolvercontroller.Controller{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunDNSResolver, RuntimeDir: defaults.RuntimeDir, StateDir: defaults.StateDir, DHCPHostsPath: dnsmasqHostsFile(opts.DnsmasqConfig)}
	eventFederation := eventfederationcontroller.Controller{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunEventFederation, RuntimeDir: defaults.RuntimeDir, StateDir: defaults.StateDir}
	var leaseSyncRolesObserved atomic.Pointer[leaseSyncRoleProof]
	leaseSync := FileSyncController{Router: r.currentRouter(), Store: store, DryRun: opts.DryRunLeaseSync, Logger: logger}
	nat44SessionSync := NAT44SessionSyncController{Router: r.currentRouter(), Store: store, DryRun: opts.DryRunNAT44SessionSync}
	bgpDaemon := bgpcontroller.DefaultDaemonSpec()
	if strings.TrimSpace(opts.BGPSocketPath) != "" {
		bgpDaemon.SocketPath = strings.TrimSpace(opts.BGPSocketPath)
		if strings.TrimSpace(opts.BGPControlSocketPath) == "" {
			bgpDaemon.ControlSocketPath = filepath.Join(filepath.Dir(bgpDaemon.SocketPath), "control.sock")
		}
	}
	if strings.TrimSpace(opts.BGPControlSocketPath) != "" {
		bgpDaemon.ControlSocketPath = strings.TrimSpace(opts.BGPControlSocketPath)
	}
	if strings.TrimSpace(opts.BGPStatePath) != "" {
		bgpDaemon.StatePath = strings.TrimSpace(opts.BGPStatePath)
	}
	// EventSubscriptionController needs the SQLite-backed federation/dynamic/
	// plugin methods in addition to status writes. The raw r.Store is the
	// *state.SQLiteStore; status writes are routed through the evented store so
	// they keep ownership annotation + bus publication parity with peers.
	var eventSubscription eventsubscriptioncontroller.Controller
	if rawStore, ok := r.Store.(eventsubscriptioncontroller.DataStore); ok {
		eventSubscription = eventsubscriptioncontroller.Controller{
			Router:     r.currentRouter(),
			Bus:        r.Bus,
			Store:      eventSubscriptionStore{evented: store, data: rawStore},
			DryRun:     opts.DryRunEventSubscription,
			RuntimeDir: defaults.RuntimeDir,
			StateDir:   defaults.StateDir,
		}
		if r.controllerEnabled("event-subscription") {
			eventSubscription.Start(ctx)
			lifetimes.eventSubscription = &eventSubscription
		}
	}
	var mobility mobilitycontroller.Controller
	var mobilityDiscovery mobilitycontroller.DiscoveryController
	var mobilityTransport mobilitycontroller.TransportController
	var mobilityEnrollmentClient mobilitycontroller.SAMEnrollmentClientController
	var mobilityShard mobilitycontroller.ShardController
	if rawStore, ok := r.Store.(mobilityDataStore); ok {
		mobilityData := mobilityStore{evented: store, data: rawStore}
		peerGroupSync := opts.PeerGroupSyncClient
		if peerGroupSync == nil {
			peerGroupSync = mobilitycontroller.NewPeerGroupSyncClient(rawStore)
		}
		mobilityDiscovery = mobilitycontroller.DiscoveryController{
			Router:           r.currentRouter(),
			Bus:              r.Bus,
			Store:            mobilityData,
			Runner:           opts.ProviderInventoryRunner,
			ProbeARP:         r.probeARPObservers,
			ARPProbeRequests: mobilitycontroller.NewARPProbeRequestTracker(),
			StartedAt:        r.startedAt(),
		}
		mobility = mobilitycontroller.Controller{
			Router:    r.currentRouter(),
			Bus:       r.Bus,
			Store:     mobilityData,
			BGPPaths:  bgpdaemon.NewControlClient(bgpDaemon.ControlSocketPath),
			StartedAt: r.startedAt(),
		}
		mobilityTransport = mobilitycontroller.TransportController{
			Router:        r.currentRouter(),
			Bus:           r.Bus,
			Store:         mobilityData,
			PeerGroupSync: peerGroupSync,
			OS:            platform.CurrentOS(),
		}
		mobilityEnrollmentClient = mobilitycontroller.SAMEnrollmentClientController{
			Router: r.currentRouter(),
			Store:  mobilityData,
		}
		mobilityShard = mobilitycontroller.ShardController{
			Router: r.currentRouter(),
			Bus:    r.Bus,
			Store:  mobilityData,
		}
	}
	var providerAction provideractioncontroller.Controller
	if rawStore, ok := r.Store.(provideractioncontroller.Store); ok {
		providerAction = provideractioncontroller.Controller{
			Router: r.currentRouter(),
			Bus:    r.Bus,
			Store:  rawStore,
			Runner: opts.ProviderActionRunner,
			DryRun: opts.DryRunProviderAction,
			Logger: logger,
		}
	}
	daemonStatusSync := DaemonStatusController{Router: r.currentRouter(), Bus: r.Bus, Store: store, DaemonSockets: opts.DaemonSockets, Logger: logger}
	wan := egressroute.Controller{Router: r.currentRouter(), Bus: r.Bus, Store: store, Logger: logger}
	durableEvents, _ := r.Store.(eventconsumer.Store)
	rules := eventrule.Controller{Router: r.currentRouter(), Bus: r.Bus, Store: eventRuleStore{store}, Logger: logger}
	derivedEvents := derived.Controller{Router: r.currentRouter(), Bus: r.Bus, Store: derivedStore{store}, Logger: logger}
	observabilityPipeline := observabilitypipeline.Controller{Router: r.currentRouter(), Bus: r.Bus, Store: store, Events: durableEvents}
	if r.controllerEnabled("observability-pipeline") {
		lifetimes.pipeline = &observabilityPipeline
	}
	nat := nat44.Controller{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunNAT, IngressLive: !opts.DryRunIngress, NftablesPath: opts.NftablesPath, NftCommand: opts.NftCommand, Logger: logger}
	ingressService := ingressservicecontroller.Controller{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunIngress, Logger: logger}
	bfd := bfdcontroller.Controller{Router: r.currentRouter(), Store: store, DryRun: opts.DryRunBGP, RuntimeDir: defaults.RuntimeDir}
	bgp := bgpcontroller.Controller{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunBGP, Logger: logger, Daemon: bgpDaemon, MutationGate: opts.MutationGate}
	serviceUnits.BGPController = &bgp
	bgp.ManagedHelperPath = filepath.Join(defaults.SystemdSystemDir, render.BGPUnitName)
	if features.HasRCD {
		bgp.ManagedHelperPath = filepath.Join(defaults.RCScriptDir, "routerd_bgp")
	}
	bgp.StopDaemon = opts.BGPDaemonStop
	if opts.DryRunServiceUnit {
		bgp.StopDaemon = nil
	} else if bgp.StopDaemon == nil {
		bgp.StopDaemon = func(ctx context.Context) error {
			command := serviceUnits.Command
			if command == nil {
				command = runOutputCommandContext
			}
			return serviceUnits.stopBGPDaemon(ctx, features, command)
		}
	}
	vrrp := vrrpcontroller.Controller{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunVRRP, Logger: logger}
	ipAddressSet := IPAddressSetController{Router: r.currentRouter(), Store: store, DryRunNAT: opts.DryRunNAT, DryRunRoute: opts.DryRunRoute, DryRunFirewall: opts.DryRunFirewall, NftCommand: opts.NftCommand, RuntimeDir: defaults.RuntimeDir}
	firewall := firewallcontroller.Controller{Router: r.currentRouter(), Bus: r.Bus, Store: store, DryRun: opts.DryRunFirewall, NftablesPath: stringutil.FirstNonEmpty(opts.FirewallPath, "/run/routerd/firewall.nft"), NftCommand: opts.NftCommand, Logger: logger}
	conntrackObs := conntrackobserver.Controller{Router: r.currentRouter(), Bus: r.Bus, Store: store, Paths: conntrack.DefaultPaths(), Logger: logger}
	if features.HasPF && !features.HasIproute2 {
		conntrackObs.SnapshotSource = "pf"
		conntrackObs.Snapshot = func(ctx context.Context) (conntrack.Snapshot, error) {
			table, err := observe.PFStateSnapshot(ctx)
			if err != nil {
				return conntrack.Snapshot{}, conntrack.UnavailableError{
					CountPath:   "pfctl -ss -v",
					EntriesPath: "pfctl -sm",
					CountErr:    err,
					EntriesErr:  err,
				}
			}
			return conntrack.Snapshot{Count: table.Count, Max: table.Max}, nil
		}
	}
	effectiveForReconcile := func() (routerObservation, error) {
		return r.effectiveRouterForReconcile(store)
	}
	effectiveDynamicForReconcile := func() (*api.Router, error) {
		return r.effectiveDynamicRouterForReconcile(store, time.Now().UTC(), platform.CurrentOS())
	}
	if r.controllerEnabled("bgp") {
		lifetimes.bgp = &bgp
	}
	if startAuxiliary {
		if r.controllerEnabled("event-rule") {
			if err := rules.Prepare(ctx); err != nil {
				return nil, DaemonStatusController{}, lifetimes, err
			}
			lifetimes.eventRule = &rules
		}
		if r.controllerEnabled("derived-event") {
			lifetimes.derivedEvent = &derivedEvents
		}
		if r.controllerEnabled("observability-pipeline") {
			observation, err := effectiveForReconcile()
			if err != nil {
				return nil, DaemonStatusController{}, lifetimes, err
			}
			effective := observation.EffectiveRouter
			observabilityPipeline.Router = effective
			observabilityPipeline.Store = store.withRouter(observation.DeclaredRouter)
			if err := observabilityPipeline.Start(ctx); err != nil {
				return nil, DaemonStatusController{}, lifetimes, err
			}
		}
		if r.controllerEnabled("bgp") {
			bgp.Start(ctx)
		}
	}
	controllers := []framework.Controller{
		framework.FuncController{ControllerName: "observability-pipeline", Every: 30 * time.Second, Subs: observabilityPipelineStatusSubscriptions(r.currentRouter()), PeriodicFunc: func(ctx context.Context) (bool, error) {
			observation, err := effectiveForReconcile()
			if err != nil {
				return false, err
			}
			effective := observation.EffectiveRouter
			observabilityPipeline.Router = effective
			observabilityPipeline.Store = store.withRouter(observation.DeclaredRouter)
			return didWorkError(observabilityPipeline.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "daemon-status", Every: 5 * time.Second, Subs: []bus.Subscription{{Topics: []string{"routerd.dhcpv6.client.**", "routerd.dhcpv4.client.**", "routerd.healthcheck.**", "routerd.pppoe.client.**", "routerd.mobility.arp.**", "routerd.mobility.pve-svnet.**"}}}, PeriodicFunc: func(ctx context.Context) (bool, error) {
			prepared, err := r.currentPreparedRouter()
			if err != nil {
				return false, err
			}
			current := daemonStatusSync
			current.Router = prepared.startup.Router()
			current.Store = store.withRouter(prepared.declared)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "nat44-session-sync", Every: 30 * time.Second, Subs: statusSubscriptionsWithWhen(r.currentRouter(), []string{"NAT44SessionSync"}, "NAT44SessionSync", "NAT44Rule", "VirtualAddress", "RouterdCluster"), PeriodicFunc: func(ctx context.Context) (bool, error) {
			observation, err := effectiveForReconcile()
			if err != nil {
				return false, err
			}
			effective := observation.EffectiveRouter
			current := nat44SessionSync
			current.Router = effective
			current.Store = store.withRouter(observation.DeclaredRouter)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "package", Every: 5 * time.Minute, Subs: bootstrapSubscriptions(), PeriodicFunc: func(ctx context.Context) (bool, error) {
			prepared, err := r.currentPreparedRouter()
			if err != nil {
				return false, err
			}
			current := packages
			current.Router = prepared.startup.Router()
			current.Store = store.withRouter(prepared.declared)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "kernel-module", Every: 5 * time.Minute, Subs: withDynamicConfigPartSubscriptions(bootstrapSubscriptions()), PeriodicFunc: func(ctx context.Context) (bool, error) {
			effective, err := effectiveDynamicForReconcile()
			if err != nil {
				return false, err
			}
			current := kernelModules
			current.Router = effective
			current.Store = store.withRouter(effective)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "sysctl", Every: 5 * time.Minute, Subs: bootstrapSubscriptions(), PeriodicFunc: func(ctx context.Context) (bool, error) {
			prepared, err := r.currentPreparedRouter()
			if err != nil {
				return false, err
			}
			current := sysctl
			current.Router = prepared.startup.Router()
			current.Store = store.withRouter(prepared.declared)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "hostname", Every: 5 * time.Minute, Subs: bootstrapSubscriptions(), PeriodicFunc: func(ctx context.Context) (bool, error) {
			prepared, err := r.currentPreparedRouter()
			if err != nil {
				return false, err
			}
			current := hostname
			current.Router = prepared.startup.Router()
			current.Store = store.withRouter(prepared.declared)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "network-adoption", Every: 5 * time.Minute, Subs: bootstrapSubscriptions(), PeriodicFunc: func(ctx context.Context) (bool, error) {
			prepared, err := r.currentPreparedRouter()
			if err != nil {
				return false, err
			}
			current := adoption
			current.Router = prepared.startup.Router()
			current.Store = store.withRouter(prepared.declared)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "bridge", Every: 30 * time.Second, Subs: statusSubscriptions("Interface", "VXLANTunnel", "VXLANSegment"), PeriodicFunc: func(ctx context.Context) (bool, error) {
			observation, err := effectiveForReconcile()
			if err != nil {
				return false, err
			}
			effective := observation.EffectiveRouter
			current := bridge
			current.Router = effective
			current.Store = store.withRouter(effective)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "service-unit", Every: 5 * time.Minute, Subs: serviceUnitStatusSubscriptions(r.currentRouter()), PeriodicFunc: func(ctx context.Context) (bool, error) {
			observation, err := effectiveForReconcile()
			if err != nil {
				return false, err
			}
			effective := observation.EffectiveRouter
			current := serviceUnits
			current.Router = effective
			current.DeclaredRouter = observation.DeclaredRouter
			current.Store = store.withRouter(observation.DeclaredRouter)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "log-retention", Every: time.Hour, Subs: bootstrapSubscriptions(), PeriodicFunc: func(ctx context.Context) (bool, error) {
			prepared, err := r.currentPreparedRouter()
			if err != nil {
				return false, err
			}
			current := logRetention
			current.Router = prepared.startup.Router()
			current.Store = store.withRouter(prepared.declared)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "ntp-client", Every: 5 * time.Minute, Subs: statusSubscriptions("DHCPv4Client", "DHCPv6Information"), PeriodicFunc: func(ctx context.Context) (bool, error) {
			observation, err := effectiveForReconcile()
			if err != nil {
				return false, err
			}
			effective := observation.EffectiveRouter
			current := ntpClient
			current.Router = effective
			current.DeclaredRouter = observation.DeclaredRouter
			current.Store = store.withRouter(observation.DeclaredRouter)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "ntp-server", Every: 5 * time.Minute, Subs: statusSubscriptionsWithWhen(r.currentRouter(), []string{"NTPServer"}, "DHCPv4Client", "DHCPv6Information", "IPv4StaticAddress", "IPv6DelegatedAddress"), PeriodicFunc: func(ctx context.Context) (bool, error) {
			observation, err := effectiveForReconcile()
			if err != nil {
				return false, err
			}
			effective := observation.EffectiveRouter
			current := ntpServer
			current.Router = effective
			current.DeclaredRouter = observation.DeclaredRouter
			current.Store = store.withRouter(observation.DeclaredRouter)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "link", Every: 30 * time.Second, PeriodicFunc: func(ctx context.Context) (bool, error) {
			observation, err := effectiveForReconcile()
			if err != nil {
				return false, err
			}
			effective := observation.EffectiveRouter
			current := link
			current.Router = effective
			current.Store = store.withRouter(effective)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "sam-enrollment-client", Every: time.Minute, Subs: statusSubscriptions("SAMEnrollmentClient", "SAMEnrollmentClaim"), NextAfter: func() time.Duration {
			current := mobilityEnrollmentClient
			current.Router = r.currentRouter()
			return current.NextReconcileAfter()
		}, PeriodicFunc: func(ctx context.Context) (bool, error) {
			current := mobilityEnrollmentClient
			prepared, err := r.currentPreparedRouter()
			if err != nil {
				return false, err
			}
			current.Router = prepared.startup.Router()
			if concrete, ok := current.Store.(mobilityStore); ok {
				current.Store = concrete.withRouter(prepared.declared)
			}
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "sam-transport", Every: 30 * time.Second, Subs: samTransportStatusSubscriptions(), PeriodicFunc: func(ctx context.Context) (bool, error) {
			effective, err := effectiveDynamicForReconcile()
			if err != nil {
				return false, err
			}
			current := mobilityTransport
			current.Router = effective
			if concrete, ok := current.Store.(mobilityStore); ok {
				current.Store = concrete.withRouter(effective)
			}
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "tunnel", Every: 30 * time.Second, Subs: withDynamicConfigPartSubscriptions(statusSubscriptions("TunnelInterface", "SAMTransportProfile")), PeriodicFunc: func(ctx context.Context) (bool, error) {
			effective, err := effectiveDynamicForReconcile()
			if err != nil {
				return false, err
			}
			current := tunnel
			current.Router = effective
			current.Store = store.withRouter(effective)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "wireguard", Every: 30 * time.Second, Subs: withDynamicConfigPartSubscriptions(statusSubscriptions("WireGuardInterface", "WireGuardPeer", "SAMNodeSet", "SAMPeerGroup", "SAMRRSet", "SAMEnrollmentPolicy", "SAMEnrollmentClaim", "BGPRouter")), PeriodicFunc: func(ctx context.Context) (bool, error) {
			effective, err := effectiveDynamicForReconcile()
			if err != nil {
				return false, err
			}
			current := wireGuard
			current.Router = effective
			current.Store = store.withRouter(effective)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "vrf", Every: 30 * time.Second, Subs: withDynamicConfigPartSubscriptions(statusSubscriptions("VRF", "Interface", "Bridge", "WireGuardInterface", "VXLANSegment", "VXLANTunnel")), PeriodicFunc: func(ctx context.Context) (bool, error) {
			effective, err := effectiveDynamicForReconcile()
			if err != nil {
				return false, err
			}
			current := vrfs
			current.Router = effective
			current.Store = store.withRouter(effective)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "ipv4-static-address", Subs: ipv4StaticAddressStatusSubscriptions(), PeriodicFunc: func(ctx context.Context) (bool, error) {
			nativeMobilityMu.Lock()
			defer nativeMobilityMu.Unlock()
			view, err := r.effectiveDynamicRouteSAMViewForReconcile(store, time.Now().UTC(), platform.CurrentOS())
			if err != nil {
				return false, err
			}
			current := ipv4Static
			current.Router = view.RouteRouter
			current.ParentRouter = view.ParentRouter
			current.DeclaredRouter = view.RouteRouter
			current.WhenRouter = view.DeclaredRouter
			current.Store = store.withRouter(view.RouteRouter)
			current.MobilityDataplane = view.MobilityDataplane
			current.MobilitySource = store
			if !current.DryRun {
				if err := invalidateMobilityNativeAck(current.Store, dynamicconfig.MobilityAddressWithdrawalAcks); err != nil {
					return false, err
				}
			}
			return didWorkError(current.Reconcile(ctx))
		}},
		r.vxlanSegmentController(store, VXLANSegmentController{StateDir: defaults.StateDir, DryRun: opts.DryRunVXLANTunnel}),
		framework.FuncController{ControllerName: "dhcpv6-information", Every: 30 * time.Second, Subs: statusSubscriptions("DHCPv6PrefixDelegation"), ReconcileFunc: func(ctx context.Context, event daemonapi.DaemonEvent) error {
			observation, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := info
			current.Router = observation.EffectiveRouter
			current.Store = store.withRouter(observation.DeclaredRouter)
			request := event.Type == "routerd.controller.bootstrap" || becamePhase(event, daemonapi.ResourcePhaseBound)
			for _, resource := range current.Router.Spec.Resources {
				if resource.Kind == "DHCPv6PrefixDelegation" {
					if err := current.reconcile(ctx, resource.Metadata.Name, request); err != nil {
						return err
					}
				}
			}
			return nil
		}},
		framework.FuncController{ControllerName: "dhcpv6-address", Every: 2 * time.Second, Subs: statusSubscriptionsWithWhen(r.currentRouter(), []string{"DHCPv6Address"}, "DHCPv6Address", "DHCPv6PrefixDelegation", "Interface", "Bridge", "VRF"), ReconcileFunc: func(ctx context.Context, _ daemonapi.DaemonEvent) error {
			observation, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			current := DHCPv6AddressController{Router: observation.EffectiveRouter, DeclaredRouter: observation.DeclaredRouter, Store: store.withRouter(observation.DeclaredRouter), DryRun: opts.DryRunAddress, Parents: lan.nativeParentReader()}
			current.Source = func(ctx context.Context, name string) (dhcpv6AddressSource, error) {
				return r.currentDHCPv6AddressSource(observation.DeclaredRouter, name, store)
			}
			return current.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "lan-address", Every: 30 * time.Second, Subs: statusSubscriptionsWithWhen(r.currentRouter(), []string{"DHCPv6PrefixDelegation", "IPv6DelegatedAddress"}, "DHCPv6PrefixDelegation", "Interface", "VRF"), ReconcileFunc: func(ctx context.Context, _ daemonapi.DaemonEvent) error {
			observation, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			effective := observation.EffectiveRouter
			current := lan
			current.Router = effective
			current.DeclaredRouter = observation.DeclaredRouter
			current.Store = store.withRouter(observation.DeclaredRouter)
			return current.Reconcile(ctx)
		}},
		framework.FuncController{ControllerName: "dslite", Every: 30 * time.Second, Subs: statusSubscriptionsWithWhen(r.currentRouter(), []string{"DSLiteTunnel"}, "DHCPv6PrefixDelegation", "DHCPv6Information", "IPv6DelegatedAddress", "Interface", "DNSResolver"), PeriodicFunc: func(ctx context.Context) (bool, error) {
			observation, err := effectiveForReconcile()
			if err != nil {
				return false, err
			}
			effective := observation.EffectiveRouter
			current := dslite
			current.Router = effective
			current.DeclaredRouter = observation.DeclaredRouter
			current.Store = store.withRouter(observation.DeclaredRouter)
			return didWorkError(current.reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "ipv4-policy-route", Subs: statusSubscriptions("DSLiteTunnel", "HealthCheck", "IPv4StaticAddress", "Interface", "VirtualAddress"), PeriodicFunc: func(ctx context.Context) (bool, error) {
			observation, err := effectiveForReconcile()
			if err != nil {
				return false, err
			}
			effective := observation.EffectiveRouter
			current := policyRoute
			current.Router = effective
			current.DeclaredRouter = observation.DeclaredRouter
			current.Store = store.withRouter(observation.DeclaredRouter)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "ipv4-route", Every: 30 * time.Second, Subs: ipv4RouteControllerStatusSubscriptions(r.currentRouter()), PeriodicFunc: func(ctx context.Context) (bool, error) {
			nativeMobilityMu.Lock()
			defer nativeMobilityMu.Unlock()
			view, err := r.effectiveDynamicRouteSAMViewForReconcile(store, time.Now().UTC(), platform.CurrentOS())
			if err != nil {
				return false, err
			}
			current := route
			current.Router = view.RouteRouter
			current.ParentRouter = view.ParentRouter
			current.Store = store.withRouter(view.RouteRouter)
			current.MobilityDataplane = view.MobilityDataplane
			current.MobilitySource = store
			if !current.DryRun {
				if err := invalidateMobilityNativeAck(current.Store, dynamicconfig.MobilityRouteWithdrawalAcks); err != nil {
					return false, err
				}
			}
			return didWorkError(current.reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "hybrid-route", Subs: hybridRouteStatusSubscriptions(), PeriodicFunc: func(ctx context.Context) (bool, error) {
			view, err := r.effectiveDynamicRouteSAMViewForReconcile(store, time.Now().UTC(), platform.CurrentOS())
			if err != nil {
				return false, err
			}
			current := hybridRoute
			current.Router = view.EffectiveRouter
			current.EffectiveRouter = view.RouteRouter
			current.Store = store.withRouter(view.RouteRouter)
			current.Lowerings = view.HybridLowerings
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "sam", Every: 30 * time.Second, Subs: samStatusSubscriptions(), PeriodicFunc: func(ctx context.Context) (bool, error) {
			nativeMobilityMu.Lock()
			defer nativeMobilityMu.Unlock()
			view, err := r.effectiveDynamicRouteSAMViewForReconcile(store, time.Now().UTC(), platform.CurrentOS())
			if err != nil {
				return false, err
			}
			current := samController
			current.Router = view.EffectiveRouter
			current.Store = store.withRouter(view.EffectiveRouter)
			current.LocalCaptureIntents = view.MobilityDataplane.Captures
			current.Withdrawals = view.MobilityDataplane.Withdrawals
			if !current.DryRun {
				if err := invalidateMobilityNativeAck(current.Store, dynamicconfig.MobilitySAMWithdrawalAcks); err != nil {
					return false, err
				}
			}
			// FreeBSD routeguard intentionally refuses to delete an owned ARP
			// neighbor while a stale /32 resolves through a tunnel. Tear down
			// owned routes from the same effective route view first. If that
			// teardown fails, SAM retains its capture ownership for retry.
			routeTeardown := route
			routeTeardown.Router = view.RouteRouter
			routeTeardown.Store = store.withRouter(view.RouteRouter)
			routeTeardown.MobilityDataplane = view.MobilityDataplane
			routeTeardown.MobilitySource = store
			return didWorkError(reconcileSAMAfterRouteTeardown(ctx, routeTeardown, current))
		}},
		framework.FuncController{ControllerName: "path-mtu", Subs: pathMTUStatusSubscriptions(r.currentRouter()), PeriodicFunc: func(ctx context.Context) (bool, error) {
			view, err := r.effectiveDynamicRouteSAMViewForReconcile(store, time.Now().UTC(), platform.CurrentOS())
			if err != nil {
				return false, err
			}
			current := pathMTU
			current.Router = view.EffectiveRouter
			current.Store = store.withRouter(view.EffectiveRouter)
			current.LocalCaptureIntents = view.MobilityDataplane.Captures
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "dhcpv6-server", Every: 30 * time.Second, Subs: []bus.Subscription{{Topics: []string{"routerd.resource.status.changed", daemonapi.EventDHCPLeaseAdded, daemonapi.EventDHCPLeaseRenewed, daemonapi.EventDHCPLeaseRemoved}}}, PeriodicFunc: func(ctx context.Context) (bool, error) {
			prepared, err := r.currentPreparedRouter()
			if err != nil {
				return false, err
			}
			currentStore := store.withRouter(prepared.declared)
			if err := r.saveWhenFalseStatuses(currentStore); err != nil {
				return false, err
			}
			current := dhcpv6
			// This owner retains declared services so its own selection can
			// withdraw a previously configured service whose condition is false.
			current.Router = prepared.startup.Router()
			current.Store = currentStore
			return didWorkError(current.reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "dhcpv4-lease", Every: 10 * time.Second, NextAfter: dhcp4Client.NextExpiryAfter, Subs: append([]bus.Subscription{{Topics: []string{"routerd.dhcpv4.client.**"}}}, whenStatusSubscriptions(r.currentRouter(), "DHCPv4Client")...), ReconcileFunc: func(ctx context.Context, _ daemonapi.DaemonEvent) error {
			prepared, err := r.currentPreparedRouter()
			if err != nil {
				return err
			}
			return dhcp4Client.ReconcileAllForRouter(ctx, prepared.declared, store.withRouter(prepared.declared))
		}, PeriodicFunc: func(ctx context.Context) (bool, error) {
			prepared, err := r.currentPreparedRouter()
			if err != nil {
				return false, err
			}
			return didWorkError(dhcp4Client.ReconcileAllForRouter(ctx, prepared.declared, store.withRouter(prepared.declared)))
		}},
		framework.FuncController{ControllerName: "pppoe-session", Every: 5 * time.Second, Subs: []bus.Subscription{{Topics: []string{"routerd.pppoe.client.**"}}}, ReconcileFunc: func(ctx context.Context, _ daemonapi.DaemonEvent) error { return reconcilePPPoESessions(ctx) }, PeriodicFunc: func(ctx context.Context) (bool, error) { return didWorkError(reconcilePPPoESessions(ctx)) }},
		framework.FuncController{ControllerName: "dns-resolver", Subs: dnsResolverStatusSubscriptions(r.currentRouter()), ReconcileFunc: func(ctx context.Context, event daemonapi.DaemonEvent) error {
			observation, err := effectiveForReconcile()
			if err != nil {
				return err
			}
			effective := observation.EffectiveRouter
			current := dnsResolver
			current.Router = effective
			current.DeclaredRouter = observation.DeclaredRouter
			current.Store = store.withRouter(observation.DeclaredRouter)
			return current.HandleEvent(ctx, event)
		}, PeriodicFunc: func(ctx context.Context) (bool, error) {
			observation, err := effectiveForReconcile()
			if err != nil {
				return false, err
			}
			effective := observation.EffectiveRouter
			current := dnsResolver
			current.Router = effective
			current.DeclaredRouter = observation.DeclaredRouter
			current.Store = store.withRouter(observation.DeclaredRouter)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "event-federation", Subs: withDynamicConfigPartSubscriptions([]bus.Subscription{{Topics: []string{"routerd.resource.status.changed"}}}), ReconcileFunc: func(ctx context.Context, event daemonapi.DaemonEvent) error {
			effective, err := effectiveDynamicForReconcile()
			if err != nil {
				return err
			}
			current := eventFederation
			current.Router = effective
			current.Store = store.withRouter(effective)
			return current.HandleEvent(ctx, event)
		}, PeriodicFunc: func(ctx context.Context) (bool, error) {
			effective, err := effectiveDynamicForReconcile()
			if err != nil {
				return false, err
			}
			current := eventFederation
			current.Router = effective
			current.Store = store.withRouter(effective)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "plugin", Subs: plugins.Subscriptions(), NextAfter: plugins.NextAfter, BootstrapFunc: func(ctx context.Context) error { _, err := plugins.Periodic(ctx); return err }, ReconcileFunc: plugins.HandleEvent, PeriodicFunc: plugins.Periodic},
		framework.FuncController{ControllerName: "event-subscription", Every: 5 * time.Second, Subs: []bus.Subscription{{Topics: []string{"routerd.resource.status.changed"}}}, PeriodicFunc: didWorkPeriodic(eventSubscription.Reconcile)},
		framework.FuncController{ControllerName: "mobility-discovery", Every: 30 * time.Second, Subs: withDynamicConfigPartSubscriptions([]bus.Subscription{{Topics: []string{"routerd.resource.status.changed", daemonapi.EventDHCPLeaseAdded, daemonapi.EventDHCPLeaseRenewed, daemonapi.EventDHCPLeaseRemoved, mobilitycontroller.OnPremARPObservedEvent, mobilitycontroller.OnPremARPProbeHitEvent, mobilitycontroller.OnPremARPRequestObservedEvent, mobilitycontroller.OnPremPVESVNetObservedEvent, provideraction.ProviderCaptureChangedEvent}}}), ReconcileFunc: func(ctx context.Context, event daemonapi.DaemonEvent) error {
			effective, err := effectiveDynamicForReconcile()
			if err != nil {
				return err
			}
			current := mobilityDiscovery
			current.Router = effective
			if concrete, ok := current.Store.(mobilityStore); ok {
				current.Store = concrete.withRouter(effective)
			}
			return current.HandleEvent(ctx, event)
		}, PeriodicFunc: func(ctx context.Context) (bool, error) {
			effective, err := effectiveDynamicForReconcile()
			if err != nil {
				return false, err
			}
			current := mobilityDiscovery
			current.Router = effective
			if concrete, ok := current.Store.(mobilityStore); ok {
				current.Store = concrete.withRouter(effective)
			}
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "mobility-arp-request", Every: time.Second, PeriodicFunc: func(ctx context.Context) (bool, error) {
			effective, err := effectiveDynamicForReconcile()
			if err != nil {
				return false, err
			}
			current := mobilityDiscovery
			current.Router = effective
			if concrete, ok := current.Store.(mobilityStore); ok {
				current.Store = concrete.withRouter(effective)
			}
			return didWorkError(current.ReconcileARPProbeRequests(ctx))
		}},
		framework.FuncController{ControllerName: "mobility", Every: 5 * time.Second, Subs: withDynamicConfigPartSubscriptions([]bus.Subscription{{Topics: []string{"routerd.resource.status.changed", mobilitycontroller.OwnershipChangedEvent}}}), ReconcileFunc: func(ctx context.Context, event daemonapi.DaemonEvent) error {
			nativeMobilityMu.Lock()
			defer nativeMobilityMu.Unlock()
			view, err := r.effectiveDynamicRouteSAMViewForReconcile(store, time.Now().UTC(), platform.CurrentOS())
			if err != nil {
				return err
			}
			current := mobility
			current.Router = view.EffectiveRouter
			current.DeclaredRouter = view.DeclaredRouter
			if concrete, ok := current.Store.(mobilityStore); ok {
				current.Store = concrete.withRouter(view.EffectiveRouter)
			}
			return current.HandleEvent(ctx, event)
		}, PeriodicFunc: func(ctx context.Context) (bool, error) {
			nativeMobilityMu.Lock()
			defer nativeMobilityMu.Unlock()
			view, err := r.effectiveDynamicRouteSAMViewForReconcile(store, time.Now().UTC(), platform.CurrentOS())
			if err != nil {
				return false, err
			}
			current := mobility
			current.Router = view.EffectiveRouter
			current.DeclaredRouter = view.DeclaredRouter
			if concrete, ok := current.Store.(mobilityStore); ok {
				current.Store = concrete.withRouter(view.EffectiveRouter)
			}
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "mobility-shard", Every: 30 * time.Second, Subs: dynamicConfigPartSubscriptions(), PeriodicFunc: func(ctx context.Context) (bool, error) {
			effective, err := effectiveDynamicForReconcile()
			if err != nil {
				return false, err
			}
			current := mobilityShard
			current.Router = effective
			if concrete, ok := current.Store.(mobilityStore); ok {
				current.Store = concrete.withRouter(effective)
			}
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "provider-action-execution", Every: 5 * time.Second, Subs: []bus.Subscription{{Topics: []string{"routerd.resource.status.changed"}}}, PeriodicFunc: func(ctx context.Context) (bool, error) {
			prepared, err := r.currentPreparedRouter()
			if err != nil {
				return false, err
			}
			current := providerAction
			current.Router = prepared.startup.Router()
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "egress-route-policy", Every: 15 * time.Second, Subs: statusSubscriptionsWithWhen(r.currentRouter(), []string{"EgressRoutePolicy"}, "HealthCheck", "DSLiteTunnel", "Interface", "DHCPv4Client", "PPPoESession"), PeriodicFunc: func(ctx context.Context) (bool, error) {
			observation, err := effectiveForReconcile()
			if err != nil {
				return false, err
			}
			effective := observation.EffectiveRouter
			current := wan
			current.Router = effective
			current.Store = store.withRouter(observation.DeclaredRouter)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "ingress-service", Every: 5 * time.Second, Subs: statusSubscriptionsWithWhen(r.currentRouter(), []string{"IngressService"}), PeriodicFunc: func(ctx context.Context) (bool, error) {
			observation, err := effectiveForReconcile()
			if err != nil {
				return false, err
			}
			effective := observation.EffectiveRouter
			current := ingressService
			current.Router = effective
			currentStore := store.withRouter(observation.DeclaredRouter)
			current.Store = currentStore
			current.Resolver, err = ingressServiceDNSResolver(effective, currentStore)
			if err != nil {
				return false, err
			}
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "nat44", Subs: statusSubscriptionsWithWhen(r.currentRouter(), []string{"NAT44Rule", "NAT44FlowDNATPinhole", "LocalServiceRedirect"}, "EgressRoutePolicy", "IngressService"), PeriodicFunc: func(ctx context.Context) (bool, error) {
			observation, err := effectiveForReconcile()
			if err != nil {
				return false, err
			}
			effective := observation.EffectiveRouter
			current := nat
			current.Router = effective
			current.Store = store.withRouter(observation.DeclaredRouter)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "bfd", Every: time.Second, Subs: withDynamicConfigPartSubscriptions(statusSubscriptionsWithWhen(r.currentRouter(), []string{"BFD"}, "BGPPeer", "BFD", "SAMTransportProfile")), PeriodicFunc: func(ctx context.Context) (bool, error) {
			view, err := r.effectiveDynamicRouteSAMViewForReconcile(store, time.Now().UTC(), platform.CurrentOS())
			if err != nil {
				return false, err
			}
			current := bfd
			current.Router = view.EffectiveRouter
			current.Store = store.withRouter(view.EffectiveRouter)
			return didWorkError(current.Reconcile(ctx))
		}},
		framework.FuncController{ControllerName: "bgp", Every: bgpcontroller.PollInterval(r.currentRouter()), Subs: bgpStatusSubscriptions(r.currentRouter()), PeriodicFunc: func(ctx context.Context) (bool, error) {
			effective, err := effectiveDynamicForReconcile()
			if err != nil {
				return false, err
			}
			return didWorkError(bgp.ReconcileRouter(ctx, effective, store.withRouter(effective)))
		}},
		framework.FuncController{ControllerName: "vrrp", Every: 5 * time.Second, Subs: vrrpStatusSubscriptions(r.currentRouter()), PeriodicFunc: func(ctx context.Context) (bool, error) {
			observation, err := effectiveForReconcile()
			if err != nil {
				leaseSyncRolesObserved.Store(nil)
				return false, err
			}
			effective := observation.EffectiveRouter
			vrrp.Router = effective
			vrrp.Store = store.withRouter(observation.DeclaredRouter)
			return didWorkError(reconcileLeaseSyncVRRPRoles(ctx, &vrrp, &leaseSyncRolesObserved))
		}},
		framework.FuncController{ControllerName: "dhcp-lease-sync", EventLockKey: "dhcp-lease-sync", Every: 30 * time.Second, Subs: statusSubscriptionsWithWhen(r.currentRouter(), []string{"DHCPv4ServerLeaseSync", "DHCPv6ServerLeaseSync", "DHCPv6PrefixDelegationLeaseSync"}, "DHCPv4ServerLeaseSync", "DHCPv6ServerLeaseSync", "DHCPv6PrefixDelegationLeaseSync", "VirtualAddress", "RouterdCluster"), PeriodicFunc: func(ctx context.Context) (bool, error) {
			observation, err := effectiveForReconcile()
			if err != nil {
				return false, err
			}
			effective := observation.EffectiveRouter
			current := leaseSync
			current.Router = effective
			current.Store = leaseSyncRoleStore{eventedStore: store.withRouter(observation.DeclaredRouter), rolesObserved: &leaseSyncRolesObserved}
			return didWorkError(current.Reconcile(ctx))
		}},
		// Client daemons with a VRRP role condition must be supervised only
		// after this generation has observed the kernel-owned VIP.  A persisted
		// role from the previous process is not authoritative during startup.
		framework.FuncController{ControllerName: "daemon-supervisor", Every: 5 * time.Second, Subs: whenStatusSubscriptions(r.currentRouter(), "DNSResolver", "DHCPv6PrefixDelegation", "DHCPv4Client", "PPPoESession"), PeriodicFunc: func(ctx context.Context) (bool, error) {
			effective, err := r.daemonSupervisionRouter(store)
			if err != nil {
				return false, err
			}
			return didWorkError(r.reconcileSupervisedClientDaemons(ctx, effective, logger))
		}},
		framework.FuncController{ControllerName: "ip-address-set", Every: 30 * time.Second, Subs: statusSubscriptionsWithWhen(r.currentRouter(), []string{"IPAddressSet", "LocalServiceRedirect", "FirewallFlowPinhole"}, "IPAddressSet", "LocalServiceRedirect", "FirewallFlowPinhole"), PeriodicFunc: func(ctx context.Context) (bool, error) {
			observation, err := effectiveForReconcile()
			if err != nil {
				return false, err
			}
			effective := observation.EffectiveRouter
			current := ipAddressSet
			current.Router = effective
			current.Store = store.withRouter(observation.DeclaredRouter)
			return didWorkError(current.Reconcile(ctx))
		}},
	}
	if startAuxiliary {
		controllers = append(controllers, r.conntrackObserverController(store, &conntrackObs))
	}

	if !opts.FirewallDisabled {
		controllers = append(controllers, framework.FuncController{ControllerName: "firewall", Subs: firewallStatusSubscriptions(r.currentRouter()), PeriodicFunc: func(ctx context.Context) (bool, error) {
			view, err := r.effectiveDynamicRouteSAMViewForReconcile(store, time.Now().UTC(), platform.CurrentOS())
			if err != nil {
				return false, err
			}
			current := firewall
			current.Router = view.EffectiveRouter
			current.Store = store.withRouter(view.EffectiveRouter)
			current.LocalCaptureIntents = view.MobilityDataplane.Captures
			return didWorkError(current.Reconcile(ctx))
		}})
	}
	if opts.SuperviseClientDaemons && r.controllerEnabled("daemon-supervisor") {
		controllers = append(controllers, framework.FuncController{ControllerName: "daemon-supervisor-reconcile", Every: 30 * time.Second, Subs: []bus.Subscription{{Topics: []string{"routerd.resource.status.changed"}}}, PeriodicFunc: func(ctx context.Context) (bool, error) {
			return didWorkError(r.reconcileARPObserverDaemons(ctx, logger))
		}})
	}
	if opts.ReconcileIPsec != nil {
		reconcileIPsec := func(ctx context.Context) error {
			view, err := r.ObserveRuntimeRouter(time.Now().UTC(), platform.CurrentOS())
			if err != nil {
				return err
			}
			return opts.ReconcileIPsec(ctx, view.RouteRouter)
		}
		controllers = append(controllers, framework.FuncController{
			ControllerName: "ipsec", Every: 5 * time.Second,
			Subs:          withDynamicConfigPartSubscriptions([]bus.Subscription{{Topics: []string{"routerd.resource.status.changed", "routerd.state.**"}}}),
			ReconcileFunc: func(ctx context.Context, _ daemonapi.DaemonEvent) error { return reconcileIPsec(ctx) },
			PeriodicFunc:  didWorkPeriodic(reconcileIPsec),
		})
	}
	controllers = r.filterControllers(controllers)
	if opts.ReconcileHTTPServers != nil {
		var nextChangeAt time.Time
		reconcileHTTP := func(ctx context.Context) error {
			nextChangeAt = time.Time{}
			view, err := r.ObserveRuntimeRouter(time.Now().UTC(), platform.CurrentOS())
			if err != nil {
				return err
			}
			nextChangeAt = view.NextChangeAt
			return opts.ReconcileHTTPServers(ctx, view)
		}
		controllers = append(controllers, framework.FuncController{
			ControllerName: "control-http", Every: 5 * time.Second,
			Subs: withDynamicConfigPartSubscriptions([]bus.Subscription{{Topics: []string{"routerd.resource.status.changed", "routerd.state.**"}}}),
			NextAfter: func() time.Duration {
				if nextChangeAt.IsZero() {
					return 5 * time.Second
				}
				remaining := time.Until(nextChangeAt)
				if remaining <= 0 {
					return time.Millisecond
				}
				return min(remaining, 5*time.Second)
			},
			ReconcileFunc: func(ctx context.Context, _ daemonapi.DaemonEvent) error { return reconcileHTTP(ctx) },
			PeriodicFunc:  didWorkPeriodic(reconcileHTTP),
		})
	}
	return controllers, daemonStatusSync, lifetimes, nil
}

func (r *Runner) conntrackObserverController(store eventedStore, observer *conntrackobserver.Controller) framework.Controller {
	interval := r.Opts.ConntrackInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return framework.FuncController{ControllerName: "conntrack-observer", Every: interval, PeriodicFunc: func(ctx context.Context) (bool, error) {
		prepared, err := r.currentPreparedRouter()
		if err != nil {
			return false, err
		}
		observer.Router = prepared.startup.Router()
		observer.Store = store.withRouter(prepared.declared)
		return didWorkError(observer.Reconcile(ctx))
	}}
}

func declaredPrefixDelegationNames(effective, declared *api.Router) []string {
	seen := map[string]bool{}
	for _, router := range []*api.Router{effective, declared} {
		if router != nil {
			for _, resource := range router.Spec.Resources {
				if resource.Kind == "DHCPv6PrefixDelegation" && resource.Metadata.Name != "" {
					seen[resource.Metadata.Name] = true
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func didWorkPeriodic(fn func(context.Context) error) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		err := fn(ctx)
		return false, err
	}
}

// reconcileSAMAfterRouteTeardown preserves the ownership ordering between
// route-backed SAM delivery and proxy-neighbor cleanup. In particular,
// FreeBSD must remove an owned /32 route before routeguard can safely remove
// the corresponding ARP neighbor. The route controller already knows how to
// retain a failed teardown for retry; do not run SAM if that prerequisite
// fails.
func reconcileSAMAfterRouteTeardown(ctx context.Context, route IPv4RouteController, controller SAMController) error {
	if err := route.cleanupRemovedMobilityRoutes(ctx); err != nil {
		return fmt.Errorf("teardown stale mobility IPv4 routes before SAM cleanup: %w", err)
	}
	if err := route.cleanupRemovedRoutes(ctx); err != nil {
		return fmt.Errorf("teardown stale IPv4Routes before SAM cleanup: %w", err)
	}
	return controller.Reconcile(ctx)
}

func didWorkError(err error) (bool, error) {
	return false, err
}

func (r *Runner) validatePFNATOptions(targetOS platform.OS, opts Options) error {
	if targetOS != platform.OSFreeBSD || !r.controllerEnabled("nat44") || opts.DryRunNAT && opts.DryRunIngress {
		return nil
	}
	if !opts.DryRunFirewall && !opts.FirewallDisabled && r.controllerEnabled("firewall") || r.currentRouter() == nil || r.Store == nil {
		return nil
	}
	router, err := r.effectiveRouter(eventedStore{Store: r.Store, Bus: r.Bus, Router: r.currentRouter()})
	if err != nil {
		return err
	}
	for _, resource := range router.Spec.Resources {
		needsNAT := resource.Kind == "NAT44Rule" && !opts.DryRunNAT
		needsIngress := resource.APIVersion == api.FirewallAPIVersion && (resource.Kind == "IngressService" && !opts.DryRunIngress || resource.Kind == "PortForward")
		if needsNAT || needsIngress {
			return fmt.Errorf("FreeBSD live NAT or ingress requires an enabled live firewall controller to attach the routerd_nat PF anchor")
		}
	}
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
		if err := recordRuntimeHADecision(router, store, decision); err != nil {
			if decision.Lease != nil {
				err = errors.Join(err, decision.Lease.Close())
			}
			return decision, err
		}
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

func ingressServiceDNSResolver(router *api.Router, store Store) (*net.Resolver, error) {
	endpoint, ok, err := dnsResolverEndpoint(router, store)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			dialer := net.Dialer{}
			return dialer.DialContext(ctx, network, endpoint)
		},
	}, nil
}

func dnsResolverEndpoint(router *api.Router, store Store) (string, bool, error) {
	if router == nil {
		return "", false, nil
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
				value, err := resourcequery.ReadValue(store, source)
				if err != nil {
					return "", false, err
				}
				if address := normalizeResolverAddress(value); address != "" {
					candidates = append(candidates, net.JoinHostPort(address, strconv.Itoa(port)))
				}
			}
		}
	}
	for _, candidate := range candidates {
		host, _, err := net.SplitHostPort(candidate)
		if err == nil && (host == "127.0.0.1" || host == "::1") {
			return candidate, true, nil
		}
	}
	if len(candidates) > 0 {
		return candidates[0], true, nil
	}
	return "", false, nil
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

func (r *Runner) reconcileSupervisedClientDaemons(ctx context.Context, router *api.Router, logger *slog.Logger) error {
	if !r.Opts.SuperviseClientDaemons {
		return r.refreshDaemonEventSources(ctx)
	}
	specs, err := r.supervisedDaemonSpecs(router)
	if err != nil {
		return err
	}
	if err := r.reconcileSupervisedDaemonSpecs(ctx, logger, specs); err != nil {
		return err
	}
	return r.cleanupRemovedDaemonStatuses()
}

func (r *Runner) daemonSupervisionRouter(store eventedStore) (*api.Router, error) {
	return resourcequery.FilterRouterByResolvedWhen(r.currentRouter(), store)
}

func (r *Runner) clientDaemonSpecs(router *api.Router) []supervisedDaemonSpec {
	if router == nil {
		return nil
	}
	var out []supervisedDaemonSpec
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "DNSResolver":
			if !r.Opts.SuperviseDNSResolvers {
				continue
			}
			defaults, _ := platform.Current()
			name := resource.Metadata.Name
			args := []string{"daemon", "--resource", name,
				"--config-file", filepath.Join(defaults.StateDir, "dns-resolver", name, "config.json"),
				"--socket", filepath.Join(defaults.RuntimeDir, "dns-resolver", name+".sock"),
				"--state-file", filepath.Join(defaults.StateDir, "dns-resolver", name, "state.json"),
				"--event-file", filepath.Join(defaults.StateDir, "dns-resolver", name, "events.jsonl"),
			}
			out = append(out, supervisedDaemonSpec{ResourceName: name, Binary: "routerd-dns-resolver", Args: args})
		case "DHCPv4Client":
			spec, err := resource.DHCPv4ClientSpec()
			if err != nil {
				continue
			}
			ifname := interfaceIfName(router, spec.Interface)
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
			out = append(out, supervisedDaemonSpec{ResourceName: resource.Metadata.Name, Binary: "routerd-dhcpv4-client", Args: args})
		case "PPPoESession":
			spec, err := resource.PPPoESessionSpec()
			if err != nil {
				continue
			}
			ifname := interfaceIfName(router, spec.Interface)
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
			out = append(out, supervisedDaemonSpec{ResourceName: resource.Metadata.Name, Binary: "routerd-pppoe-client", Args: args})
		}
	}
	for _, worker := range dhcpv6Workers(router) {
		out = append(out, worker.daemonSpec())
	}
	return out
}

func (r *Runner) supervisedDaemonSpecs(router *api.Router) ([]supervisedDaemonSpec, error) {
	out := r.clientDaemonSpecs(router)
	observers, err := r.mobilityARPObserverDaemonSpecs()
	if err != nil {
		return nil, err
	}
	for _, spec := range observers {
		out = append(out, supervisedDaemonSpec{
			ResourceName: spec.ResourceName,
			Binary:       "routerd-arp-observer",
			Args:         arpObserverDaemonArgs(spec),
		})
	}
	return out, nil
}

func (r *Runner) reconcileSupervisedDaemonSpecs(ctx context.Context, logger *slog.Logger, specs []supervisedDaemonSpec) error {
	desiredAll := make(map[string]supervisedDaemonSpec, len(specs))
	for _, spec := range specs {
		if spec.ResourceName == "" || spec.Binary == "" {
			continue
		}
		desiredAll[supervisedDaemonKey(spec.Binary, spec.ResourceName)] = spec
	}
	pendingStart := make(map[string]supervisedDaemonSpec, len(desiredAll))
	for key, spec := range desiredAll {
		pendingStart[key] = spec
	}
	markers, err := readSupervisedDaemonMarkers()
	if err != nil {
		if logger != nil {
			logger.Error("supervised daemon ownership records unavailable; refusing adoption", "error", err)
		}
		return err
	}
	for key, marker := range markers {
		if spec, ok := desiredAll[key]; ok && marker.SpecHash == supervisedDaemonSpecHash(spec) {
			spec.OwnerToken = marker.OwnerToken
			desiredAll[key] = spec
			pendingStart[key] = spec
		}
	}

	var stale []supervisedDaemonState
	var staleMarkers []supervisedDaemonMarker
	r.supervisedMu.Lock()
	if r.clientDaemonStates == nil {
		r.clientDaemonStates = map[string]supervisedDaemonState{}
	}
	for key, state := range r.clientDaemonStates {
		next, ok := desiredAll[key]
		if ok && supervisedDaemonSpecEqual(state.Spec, next) {
			select {
			case <-state.Done:
				// A finished supervisor cannot monitor or restart its child.
				delete(r.clientDaemonStates, key)
				continue
			default:
			}
			delete(pendingStart, key)
			continue
		}
		state.Cancel()
		stale = append(stale, state)
	}
	r.supervisedMu.Unlock()
	for key, marker := range markers {
		spec, wanted := desiredAll[key]
		if !wanted || marker.SpecHash != supervisedDaemonSpecHash(spec) {
			staleMarkers = append(staleMarkers, marker)
		}
	}

	failedCleanup := make(map[string]bool)
	var failures []error
	for _, state := range stale {
		key := supervisedDaemonKey(state.Spec.Binary, state.Spec.ResourceName)
		if err := stopSupervisedDaemonState(state, logger); err != nil {
			failures = append(failures, err)
			failedCleanup[key] = true
			delete(pendingStart, key)
			if logger != nil {
				logger.Error("retain supervised daemon ownership after incomplete cleanup", "binary", state.Spec.Binary, "resource", state.Spec.ResourceName, "error", err)
			}
			continue
		}
		r.supervisedMu.Lock()
		if current, ok := r.clientDaemonStates[key]; ok && current.Done == state.Done {
			delete(r.clientDaemonStates, key)
		}
		r.supervisedMu.Unlock()
	}
	for _, marker := range staleMarkers {
		key := supervisedDaemonKey(marker.Binary, marker.Resource)
		if failedCleanup[key] {
			continue
		}
		if err := stopOwnedSupervisedDaemon(marker, logger); err != nil {
			failures = append(failures, err)
			if logger != nil {
				logger.Error("retaining supervised daemon ownership record after failed cleanup", "binary", marker.Binary, "resource", marker.Resource, "error", err)
			}
			failedCleanup[key] = true
			delete(pendingStart, key)
			continue
		}
		if err := removeSupervisedDaemonMarker(marker); err != nil {
			failures = append(failures, err)
			failedCleanup[key] = true
			delete(pendingStart, key)
			if logger != nil {
				logger.Error("remove supervised daemon ownership record", "binary", marker.Binary, "resource", marker.Resource, "error", err)
			}
		}
	}

	// A renamed worker can need the same native socket as an obsolete worker.
	// Marker keys alone do not prove that those owners are independent. Finish
	// every obsolete stop before creating any replacement ownership record.
	if len(failures) != 0 {
		return errors.Join(failures...)
	}
	for key, spec := range desiredAll {
		if failedCleanup[key] {
			continue
		}
		if marker, ok := markers[key]; ok && marker.SpecHash == supervisedDaemonSpecHash(supervisedDaemonSpec{ResourceName: spec.ResourceName, Binary: spec.Binary, Args: spec.Args}) {
			continue
		}
		if supervisedDaemonSocketReady(defaultClientSocket(spec.Binary, spec.ResourceName)) {
			failures = append(failures, fmt.Errorf("refusing to adopt markerless ready %s socket for %s", spec.Binary, spec.ResourceName))
			if logger != nil {
				logger.Error("refusing to adopt markerless ready supervised daemon socket", "binary", spec.Binary, "resource", spec.ResourceName)
			}
			delete(pendingStart, key)
			continue
		}
		token, err := newSupervisedDaemonOwnerToken()
		if err != nil {
			failures = append(failures, err)
			if logger != nil {
				logger.Error("generate supervised daemon ownership token", "binary", spec.Binary, "resource", spec.ResourceName, "error", err)
			}
			delete(pendingStart, key)
			continue
		}
		spec.OwnerToken = token
		if err := writeSupervisedDaemonMarker(supervisedDaemonMarker{Version: 1, Binary: spec.Binary, Resource: spec.ResourceName, SpecHash: supervisedDaemonSpecHash(spec), OwnerToken: token}); err != nil {
			failures = append(failures, err)
			if logger != nil {
				logger.Error("write supervised daemon ownership record", "binary", spec.Binary, "resource", spec.ResourceName, "error", err)
			}
			delete(pendingStart, key)
			continue
		}
		desiredAll[key] = spec
		pendingStart[key] = spec
	}

	r.supervisedMu.Lock()
	for key, spec := range pendingStart {
		if _, ok := r.clientDaemonStates[key]; ok {
			continue
		}
		parent := r.clientDaemonContext
		if parent == nil {
			parent = ctx
		}
		childCtx, cancel := context.WithCancel(parent)
		done := r.startSupervisedDaemonSpec(childCtx, logger, spec)
		r.clientDaemonStates[key] = supervisedDaemonState{Spec: spec, Cancel: cancel, Done: done}
	}
	r.supervisedMu.Unlock()

	return errors.Join(errors.Join(failures...), r.reconcileARPObserverDaemons(ctx, logger))
}

func supervisedDaemonKey(binary, resourceName string) string {
	return binary + "/" + resourceName
}

func supervisedDaemonSpecEqual(a, b supervisedDaemonSpec) bool {
	if a.ResourceName != b.ResourceName || a.Binary != b.Binary || a.OwnerToken != b.OwnerToken || len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if a.Args[i] != b.Args[i] {
			return false
		}
	}
	return true
}

type mobilityARPObserverDaemonSpec struct {
	ResourceName      string
	PoolName          string
	Prefix            string
	SourceType        string
	IfName            string
	EventInterface    string
	Network           string
	Bridge            string
	Socket            string
	EventFile         string
	SourceAddress     string
	Observe           bool
	OnDemand          bool
	ProbeTimeout      string
	ProbeRetries      int
	ScanInterval      string
	IgnoredSenderMACs []string
}

func (r *Runner) mobilityARPObserverDaemonSpecs() ([]mobilityARPObserverDaemonSpec, error) {
	if r == nil {
		return nil, errors.New("ARP observer runtime is unavailable")
	}
	lister, ok := r.Store.(dynamicConfigPartLister)
	if !ok {
		return nil, errors.New("dynamic config part reading is unavailable")
	}
	records, err := lister.ListDynamicConfigParts()
	if err != nil {
		return nil, fmt.Errorf("list ARP observer dynamic config: %w", err)
	}
	intents, err := mobilityARPObserverIntentsFromRecords(records, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return r.mobilityARPObserverDaemonSpecsFromIntents(intents), nil
}

func (r *Runner) mobilityARPObserverDaemonSpecsFromIntents(intents []dynamicconfig.ARPObserverIntent) []mobilityARPObserverDaemonSpec {
	defaults, _ := platform.Current()
	seen := map[string]bool{}
	var out []mobilityARPObserverDaemonSpec
	for _, intent := range intents {
		resourceName := strings.TrimSpace(intent.ResourceName)
		poolName := strings.TrimSpace(intent.PoolRef)
		prefix := strings.TrimSpace(intent.Prefix)
		sourceType := strings.TrimSpace(intent.SourceType)
		ifName := strings.TrimSpace(intent.IfName)
		eventInterface := strings.TrimSpace(intent.EventInterface)
		if resourceName == "" || poolName == "" || prefix == "" || sourceType == "" || ifName == "" || eventInterface == "" || seen[resourceName] {
			continue
		}
		seen[resourceName] = true
		socket := filepath.Join(defaults.RuntimeDir, "arp-observer", resourceName+".sock")
		if override := strings.TrimSpace(r.Opts.DaemonSockets[resourceName]); override != "" {
			socket = override
		}
		out = append(out, mobilityARPObserverDaemonSpec{
			ResourceName:      resourceName,
			PoolName:          poolName,
			Prefix:            prefix,
			SourceType:        sourceType,
			IfName:            ifName,
			EventInterface:    eventInterface,
			Network:           strings.TrimSpace(intent.Network),
			Bridge:            strings.TrimSpace(intent.Bridge),
			Socket:            socket,
			EventFile:         filepath.Join(defaults.StateDir, "arp-observer", resourceName, "events.jsonl"),
			SourceAddress:     strings.TrimSpace(intent.SourceAddress),
			Observe:           intent.Observe,
			OnDemand:          intent.OnDemand,
			ProbeTimeout:      strings.TrimSpace(intent.ProbeTimeout),
			ProbeRetries:      intent.ProbeRetries,
			ScanInterval:      strings.TrimSpace(intent.ScanInterval),
			IgnoredSenderMACs: append([]string(nil), intent.IgnoredSenderMACs...),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ResourceName < out[j].ResourceName })
	return out
}

func (r *Runner) reconcileARPObserverDaemons(ctx context.Context, logger *slog.Logger) error {
	prepared, err := r.currentPreparedRouter()
	if err != nil {
		return err
	}
	specs, err := r.mobilityARPObserverDaemonSpecs()
	if err != nil {
		return err
	}

	var failures []error
	for _, spec := range specs {
		operation, cancel := context.WithTimeout(ctx, 5*time.Second)
		if r.arpObserverReady(spec.ResourceName) {
			err = r.syncARPObserverIgnoredSenderMACs(operation, spec)
		} else {
			err = r.waitForARPObserverInitialSync(operation, spec)
		}
		cancel()
		if err != nil {
			failures = append(failures, fmt.Errorf("sync ARP observer %s: %w", spec.ResourceName, err))
		}
	}
	return errors.Join(errors.Join(failures...), r.reconcileDaemonEventSources(ctx, prepared.declared, specs))
}

func arpObserverDaemonArgs(spec mobilityARPObserverDaemonSpec) []string {
	args := []string{
		"daemon",
		"--resource", spec.ResourceName,
		"--interface", spec.IfName,
		"--event-interface", spec.EventInterface,
		"--socket", spec.Socket,
		"--event-file", spec.EventFile,
		"--pool", spec.PoolName,
		"--prefix", spec.Prefix,
		"--source-type", spec.SourceType,
	}
	if spec.Network != "" {
		args = append(args, "--network", spec.Network)
	}
	if spec.Bridge != "" {
		args = append(args, "--bridge", spec.Bridge)
	}
	if spec.SourceAddress != "" {
		args = append(args, "--source-address", spec.SourceAddress)
	}
	if spec.OnDemand {
		args = append(args, "--on-demand")
	}
	if spec.Observe {
		args = append(args, "--observe")
	}
	if spec.ProbeTimeout != "" {
		args = append(args, "--probe-timeout", spec.ProbeTimeout)
	}
	if spec.ProbeRetries != 0 {
		args = append(args, "--probe-retries", fmt.Sprintf("%d", spec.ProbeRetries))
	}
	if spec.ScanInterval != "" {
		args = append(args, "--scan-interval", spec.ScanInterval)
	}
	return args
}

func (r *Runner) startSupervisedDaemonSpec(ctx context.Context, logger *slog.Logger, spec supervisedDaemonSpec) <-chan struct{} {
	resourceName, binary, args := spec.ResourceName, spec.Binary, append([]string(nil), spec.Args...)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			running, ownershipErr := supervisedDaemonOwnedProcessRunning(spec)
			ready := supervisedDaemonSocketReady(defaultClientSocket(binary, resourceName))
			if ownershipErr != nil || running || ready {
				if logger != nil && (ownershipErr != nil || (ready && !running)) {
					logger.Error("supervised daemon ownership is unavailable or foreign; refusing replacement", "binary", binary, "resource", resourceName, "error", ownershipErr)
				}
				select {
				case <-time.After(10 * time.Second):
					continue
				case <-ctx.Done():
					return
				}
			}
			path := routerdClientBinary(binary)
			if binary == "routerd-arp-observer" {
				r.setARPObserverReady(resourceName, false)
				r.pauseDaemonEventSource(binary, resourceName)
			}
			cmdArgs := append([]string(nil), args...)
			if spec.OwnerToken != "" {
				cmdArgs = append(cmdArgs, "--supervisor-owner", spec.OwnerToken)
			}
			cmd := exec.CommandContext(ctx, path, cmdArgs...)
			cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
			cmd.WaitDelay = supervisedDaemonStopGrace(binary)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if logger != nil {
				logger.Info("starting supervised routerd client daemon", "binary", path, "resource", resourceName)
			}
			if err := cmd.Start(); err != nil {
				if logger != nil {
					logger.Warn("supervised routerd client daemon failed to start", "binary", path, "resource", resourceName, "error", err)
				}
				select {
				case <-time.After(5 * time.Second):
					continue
				case <-ctx.Done():
					return
				}
			}
			if err := writeSupervisedDaemonMarker(supervisedDaemonMarker{Version: 1, Binary: binary, Resource: resourceName, SpecHash: supervisedDaemonSpecHash(spec), OwnerToken: spec.OwnerToken, PID: cmd.Process.Pid}); err != nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
				timer := time.AfterFunc(supervisedDaemonStopGrace(binary), func() { _ = cmd.Process.Kill() })
				_ = cmd.Wait()
				timer.Stop()
				if logger != nil {
					logger.Error("supervised daemon ownership record write failed", "binary", path, "resource", resourceName, "error", err)
				}
				return
			}
			err := cmd.Wait()
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
	return done
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
	case "routerd-arp-observer":
		return filepath.Join(defaults.RuntimeDir, "arp-observer", resource+".sock")
	case "routerd-dns-resolver":
		return filepath.Join(defaults.RuntimeDir, "dns-resolver", resource+".sock")
	case "routerd-healthcheck":
		return filepath.Join(defaults.RuntimeDir, "healthcheck", resource+".sock")
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

func (r *Runner) syncARPObserverIgnoredSenderMACs(ctx context.Context, spec mobilityARPObserverDaemonSpec) error {
	pusher := r.arpObserverCommandPusher()
	status, err := pusher.Status(ctx, spec.Socket)
	if err != nil {
		return err
	}
	observed, err := parseARPObserverIgnoredSenderMACs(status.Observed["ignoredSenderMACs"])
	if err != nil {
		return err
	}
	desired := normalizeMACStringList(spec.IgnoredSenderMACs)
	configured := strings.EqualFold(strings.TrimSpace(status.Observed["ignoredSenderMACsConfigured"]), "true")
	if configured && equalStringSlices(observed, desired) {
		return nil
	}
	return pusher.SetIgnoredSenderMACs(ctx, spec.Socket, desired)
}

func (r *Runner) waitForARPObserverInitialSync(ctx context.Context, spec mobilityARPObserverDaemonSpec) error {
	err := r.syncARPObserverIgnoredSenderMACs(ctx, spec)
	if err != nil {
		r.setARPObserverReady(spec.ResourceName, false)
		return err
	}
	r.setARPObserverReady(spec.ResourceName, true)
	return nil
}

func (r *Runner) arpObserverReady(resourceName string) bool {
	r.supervisedMu.Lock()
	defer r.supervisedMu.Unlock()
	return r.arpObserverReadySet != nil && r.arpObserverReadySet[resourceName]
}

func (r *Runner) setARPObserverReady(resourceName string, ready bool) {
	r.supervisedMu.Lock()
	defer r.supervisedMu.Unlock()
	if r.arpObserverReadySet == nil {
		r.arpObserverReadySet = make(map[string]bool)
	}
	if ready {
		r.arpObserverReadySet[resourceName] = true
		return
	}
	delete(r.arpObserverReadySet, resourceName)
}

func (r *Runner) arpObserverCommandPusher() arpObserverCommandPusher {
	if r != nil && r.ARPObserverCommands != nil {
		return r.ARPObserverCommands
	}
	return socketARPObserverCommandPusher{}
}

type socketARPObserverCommandPusher struct{}

func (socketARPObserverCommandPusher) Status(ctx context.Context, socketPath string) (daemonapi.DaemonStatus, error) {
	return daemonStatus(ctx, socketPath)
}

func (socketARPObserverCommandPusher) SetIgnoredSenderMACs(ctx context.Context, socketPath string, macs []string) error {
	return postARPObserverCommand(ctx, socketPath, daemonapi.CommandRequest{
		TypeMeta: daemonapi.TypeMeta{APIVersion: daemonapi.APIVersion, Kind: daemonapi.KindCommandRequest},
		Command:  "set-ignored-sender-macs",
		Attributes: map[string]string{
			"macAddresses": strings.Join(normalizeMACStringList(macs), ","),
		},
	})
}

func (socketARPObserverCommandPusher) ProbeTarget(ctx context.Context, socketPath, target string) error {
	return postARPObserverCommand(ctx, socketPath, daemonapi.CommandRequest{
		TypeMeta:   daemonapi.TypeMeta{APIVersion: daemonapi.APIVersion, Kind: daemonapi.KindCommandRequest},
		Command:    "probe-target",
		Attributes: map[string]string{"target": strings.TrimSpace(target)},
	})
}

func postARPObserverCommand(ctx context.Context, socketPath string, reqBody daemonapi.CommandRequest) error {
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socketPath)
	}}}
	defer client.CloseIdleConnections()
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/commands", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Close = true
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result daemonapi.CommandResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return err
	}
	if !result.Accepted {
		return fmt.Errorf("arp observer command rejected: %s", result.Message)
	}
	return nil
}

func (r *Runner) probeARPObservers(ctx context.Context, poolName, address string) error {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(address))
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
		return fmt.Errorf("ARP probe target must be an IPv4 /32: %q", address)
	}
	target := prefix.Addr().String()
	matched := 0
	var errs []error
	specs, err := r.mobilityARPObserverDaemonSpecs()
	if err != nil {
		return err
	}
	for _, spec := range specs {
		if strings.TrimSpace(spec.PoolName) != strings.TrimSpace(poolName) || spec.SourceType != mobilitycontroller.OnPremSourceOnDemandARP || !spec.OnDemand || !r.arpObserverReady(spec.ResourceName) {
			continue
		}
		observerPrefix, parseErr := netip.ParsePrefix(spec.Prefix)
		if parseErr != nil || !observerPrefix.Contains(prefix.Addr()) {
			continue
		}
		matched++
		if source, parseErr := netip.ParseAddr(spec.SourceAddress); parseErr == nil && source == prefix.Addr() {
			// A ready observer never probes its own source address. Treat this
			// selection as completed so the federation request stays claimed;
			// still try other eligible observers and propagate their failures.
			continue
		}
		if err := r.arpObserverCommandPusher().ProbeTarget(ctx, spec.Socket, target); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", spec.ResourceName, err))
		}
	}
	if matched == 0 {
		return fmt.Errorf("no ready on-demand-arp observer for MobilityPool/%s target %s", poolName, target)
	}
	return errors.Join(errs...)
}

func parseARPObserverIgnoredSenderMACs(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	return normalizeMACStringListWithError(strings.Split(value, ","))
}

func normalizeMACStringList(values []string) []string {
	out, _ := normalizeMACStringListWithError(values)
	return out
}

func normalizeMACStringListWithError(values []string) ([]string, error) {
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		mac, err := net.ParseMAC(value)
		if err != nil {
			return nil, err
		}
		seen[strings.ToLower(mac.String())] = true
	}
	out := make([]string, 0, len(seen))
	for mac := range seen {
		out = append(out, mac)
	}
	sort.Strings(out)
	return out, nil
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func supervisedDaemonStopGrace(binary string) time.Duration {
	if binary == "routerd-pppoe-client" {
		return 10 * time.Second
	} // mpd grace3s + native interface withdrawal5s
	return 2 * time.Second
}

func stopSupervisedDaemonState(state supervisedDaemonState, logger *slog.Logger) error {
	if state.Done == nil {
		return errors.New("supervised daemon completion observation is unavailable")
	}
	marker := supervisedDaemonMarker{Version: 1, Binary: state.Spec.Binary, Resource: state.Spec.ResourceName, SpecHash: supervisedDaemonSpecHash(state.Spec), OwnerToken: state.Spec.OwnerToken}
	if err := stopOwnedSupervisedDaemon(marker, logger); err != nil {
		return err
	}
	timer := time.NewTimer(supervisedDaemonStopGrace(state.Spec.Binary) + 2*time.Second)
	defer timer.Stop()
	select {
	case <-state.Done:
		return nil
	case <-timer.C:
		return errors.New("supervised daemon stop has not completed")
	}
}

func stopOwnedSupervisedDaemon(marker supervisedDaemonMarker, logger *slog.Logger) error {
	pids, err := matchingOwnedSupervisedDaemonPIDs(marker)
	if err != nil {
		return err
	}
	if len(pids) == 0 {
		return nil
	}
	if logger != nil {
		logger.Info("stopping stale supervised routerd client daemon", "binary", marker.Binary, "resource", marker.Resource, "pids", pids)
	}
	var signalErr error
	for _, pid := range pids {
		if err := supervisedDaemonSignal(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			signalErr = errors.Join(signalErr, err)
		}
	}
	deadline := time.Now().Add(supervisedDaemonStopGrace(marker.Binary))
	for time.Now().Before(deadline) {
		pids, err = matchingOwnedSupervisedDaemonPIDs(marker)
		if err != nil {
			return errors.Join(signalErr, err)
		}
		if len(pids) == 0 {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, pid := range pids {
		if err := supervisedDaemonSignal(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			signalErr = errors.Join(signalErr, err)
		}
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pids, err = matchingOwnedSupervisedDaemonPIDs(marker)
		if err != nil {
			return errors.Join(signalErr, err)
		}
		if len(pids) == 0 {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.Join(signalErr, fmt.Errorf("owned supervised daemon remained after SIGKILL: binary=%s resource=%s", marker.Binary, marker.Resource))
}

func supervisedDaemonOwnedProcessRunning(spec supervisedDaemonSpec) (bool, error) {
	markers, err := readSupervisedDaemonMarkers()
	if err != nil {
		return false, err
	}
	marker, ok := markers[supervisedDaemonKey(spec.Binary, spec.ResourceName)]
	if !ok || marker.OwnerToken != spec.OwnerToken || marker.SpecHash != supervisedDaemonSpecHash(spec) {
		return false, errors.New("supervised daemon ownership record does not match current intent")
	}
	pids, err := matchingOwnedSupervisedDaemonPIDs(marker)
	if err != nil {
		return false, err
	}
	if len(pids) > 1 {
		return false, errors.New("multiple processes claim supervised daemon ownership")
	}
	if len(pids) == 0 {
		return false, nil
	}
	if marker.PID == 0 {
		marker.PID = pids[0]
		if err := writeSupervisedDaemonMarker(marker); err != nil {
			return false, err
		}
	}
	return true, nil
}

func matchingOwnedSupervisedDaemonPIDs(marker supervisedDaemonMarker) ([]int, error) {
	processes, err := supervisedDaemonProcesses(marker)
	if err != nil {
		return nil, err
	}
	var out []int
	for _, process := range processes {
		if process.PID <= 0 || process.PID == os.Getpid() {
			continue
		}
		if marker.PID > 0 && process.PID != marker.PID {
			continue
		}
		if routerdDaemonCommandMatches(process.Command, marker.Binary, marker.Resource, marker.OwnerToken) {
			out = append(out, process.PID)
		}
	}
	sort.Ints(out)
	return out, nil
}

func listSupervisedDaemonProcesses(marker supervisedDaemonMarker) ([]supervisedDaemonProcess, error) {
	switch runtime.GOOS {
	case "linux":
		return listLinuxSupervisedDaemonProcesses(marker)
	case "freebsd":
		return listFreeBSDSupervisedDaemonProcesses(marker)
	default:
		return nil, errors.New("supervised process observation is unsupported on this platform")
	}
}

func listLinuxSupervisedDaemonProcesses(marker supervisedDaemonMarker) ([]supervisedDaemonProcess, error) {
	if marker.PID > 0 {
		process, err := readLinuxSupervisedDaemonProcess(marker.PID)
		if err != nil {
			return nil, err
		}
		if process.Command == "" {
			return nil, nil
		}
		return []supervisedDaemonProcess{process}, nil
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var out []supervisedDaemonProcess
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		process, err := readLinuxSupervisedDaemonProcess(pid)
		if err != nil {
			// An unreadable unrelated executable does not hide this owner's PID.
			comm, commErr := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
			want := marker.Binary
			if len(want) > 15 {
				want = want[:15]
			}
			if errors.Is(commErr, os.ErrNotExist) {
				continue
			}
			if commErr == nil && strings.TrimSpace(string(comm)) != want {
				continue
			}
			return nil, err
		}
		if process.Command != "" {
			out = append(out, process)
		}
	}
	return out, nil
}

func readLinuxSupervisedDaemonProcess(pid int) (supervisedDaemonProcess, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return supervisedDaemonProcess{}, nil
	}
	if err != nil {
		return supervisedDaemonProcess{}, err
	}
	return supervisedDaemonProcess{PID: pid, Command: strings.ReplaceAll(strings.TrimRight(string(data), "\x00"), "\x00", " ")}, nil
}

func listFreeBSDSupervisedDaemonProcesses(marker supervisedDaemonMarker) ([]supervisedDaemonProcess, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := freeBSDSupervisedDaemonPSCommand(ctx).Output()
	if err != nil {
		return nil, err
	}
	var out []supervisedDaemonProcess
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 2 {
			return nil, errors.New("supervised process observation has incomplete command")
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid < 0 {
			return nil, fmt.Errorf("invalid supervised process PID %q", fields[0])
		}
		// FreeBSD ps includes PID 0 for the kernel. It cannot be a managed
		// daemon, and must not make every userspace process unobservable.
		if pid == 0 {
			continue
		}
		if marker.PID > 0 && pid != marker.PID {
			continue
		}
		out = append(out, supervisedDaemonProcess{PID: pid, Command: strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))})
	}
	return out, nil
}

func freeBSDSupervisedDaemonPSCommand(ctx context.Context) *exec.Cmd {
	return exec.CommandContext(ctx, "ps", "-axww", "-o", "pid=", "-o", "command=")
}

func routerdDaemonCmdlineMatches(args []string, binary, resourceName string) bool {
	if len(args) == 0 || filepath.Base(args[0]) != binary {
		return false
	}
	hasDaemon := false
	for _, arg := range args[1:] {
		if arg == "daemon" {
			hasDaemon = true
			break
		}
	}
	if !hasDaemon {
		return false
	}
	for i := 1; i < len(args)-1; i++ {
		if args[i] == "--resource" && args[i+1] == resourceName {
			return true
		}
	}
	prefix := "--resource="
	for _, arg := range args[1:] {
		if strings.TrimPrefix(arg, prefix) != arg && strings.TrimPrefix(arg, prefix) == resourceName {
			return true
		}
	}
	return false
}

// routerdDaemonCommandMatches intentionally requires the opaque owner token
// in addition to binary/resource. A matching resource name alone is not
// ownership proof after a controller crash.
func routerdDaemonCommandMatches(command, binary, resourceName, ownerToken string) bool {
	if binary == "" || resourceName == "" || ownerToken == "" {
		return false
	}
	fields := strings.Fields(command)
	if len(fields) == 0 || filepath.Base(fields[0]) != binary {
		return false
	}
	daemonOK, resourceOK, ownerOK := false, false, false
	for i := 0; i < len(fields); i++ {
		switch {
		case fields[i] == "daemon":
			daemonOK = true
		case fields[i] == "--resource" && i+1 < len(fields) && fields[i+1] == resourceName:
			resourceOK = true
		case fields[i] == "--supervisor-owner" && i+1 < len(fields) && fields[i+1] == ownerToken:
			ownerOK = true
		case fields[i] == "--resource="+resourceName:
			resourceOK = true
		case fields[i] == "--supervisor-owner="+ownerToken:
			ownerOK = true
		}
	}
	return daemonOK && resourceOK && ownerOK
}

type LANAddressController struct {
	Segments       *VXLANSegmentController
	DSLite         *DSLiteTunnelController
	Bridges        *BridgeController
	WireGuard      *WireGuardController
	VRFs           *VRFController
	ObserveAddress func(context.Context, string, string, bool) (delegatedAddressObservation, error)
	Router         *api.Router
	DeclaredRouter *api.Router
	Bus            *bus.Bus
	Store          Store
	DryRun         bool
	Logger         *slog.Logger
	Command        commandFunc
	// EnsureVMAC is used only for standby staging.  The authoritative VRRP
	// helper creates the configured macvlan cold and leaves it DOWN.
	EnsureVMAC      func(context.Context, string) error
	VMACPresent     func(string) bool
	Now             func() time.Time
	OperatingSystem platform.OS
}

func (c LANAddressController) currentOS() platform.OS {
	if c.OperatingSystem != "" {
		return c.OperatingSystem
	}
	return platform.CurrentOS()
}

type IPv4StaticAddressController struct {
	ParentRouter      *api.Router
	MobilitySource    dynamicConfigPartLister
	Router            *api.Router
	DeclaredRouter    *api.Router
	WhenRouter        *api.Router
	Bus               *bus.Bus
	Store             Store
	MobilityDataplane dynamicconfig.MobilityDataplanePlan
	DryRun            bool
	Logger            *slog.Logger
	Command           commandFunc
	DevicePresent     func(context.Context, string) bool
	ObserveAddresses  func(context.Context, string) (staticAddressSnapshot, error)
	Segments          *VXLANSegmentController
	DSLite            *DSLiteTunnelController
	Bridges           *BridgeController
	WireGuard         *WireGuardController
	VRFs              *VRFController
}

type DaemonStatusController struct {
	Router              *api.Router
	Bus                 *bus.Bus
	Store               Store
	DaemonSockets       map[string]string
	Logger              *slog.Logger
	PDLeaseSnapshotPath func(string) string
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
	if err := c.expireKnownPDLeases(ctx); err != nil {
		return err
	}
	sockets, err := c.daemonSockets()
	if err != nil {
		return err
	}
	for _, socket := range sockets {
		statusCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		status, err := daemonStatus(statusCtx, socket)
		cancel()
		if err != nil {
			if resetErr := c.resetPDLeaseSourceForUnavailableSocket(ctx, socket); resetErr != nil {
				return resetErr
			}
			if c.Logger != nil && ctx.Err() == nil {
				c.Logger.Debug("daemon status snapshot skipped", "socket", socket, "error", err)
			}
			continue
		}
		for _, observed := range status.Resources {
			if observed.Resource.Kind == "PPPoESession" || observed.Resource.Kind == "DHCPv6Address" {
				continue
			} // source-bound native actors own these projections
			if observed.Resource.Kind == "DHCPv6PrefixDelegation" {
				if err := c.observePDLease(ctx, observed.Resource.Name, socket, observed); err != nil {
					return err
				}
				continue
			}
			if observed.Resource.Kind == "HealthCheck" {
				observed, err = c.currentHealthSnapshot(observed, time.Now().UTC())
				if err != nil {
					return err
				}
			}
			base := map[string]any{
				"phase":      observed.Phase,
				"health":     observed.Health,
				"conditions": observed.Conditions,
				"updatedAt":  time.Now().UTC().Format(time.RFC3339Nano),
			}
			next := copyStatusMap(base)
			if daemonStatusObservedOnlyKind(observed.Resource.Kind) {
				next = daemonObservedOnlyStatus(c.Store.ObjectStatus(observed.Resource.APIVersion, observed.Resource.Kind, observed.Resource.Name), base, observed)
			} else {
				for key, value := range observed.Observed {
					next[key] = value
				}
			}
			if observed.Resource.Kind == "HealthCheck" {
				delete(next, "routing")
				if observed.Routing != nil {
					next["routing"] = *observed.Routing
				}
			}
			if err := c.Store.SaveObjectStatus(observed.Resource.APIVersion, observed.Resource.Kind, observed.Resource.Name, next); err != nil {
				return err
			}
		}
	}
	return nil
}

func daemonStatusObservedOnlyKind(kind string) bool {
	switch kind {
	case "DHCPv4Client", "PPPoESession", "DHCPv6PrefixDelegation", "DHCPv6Address", "DNSResolver", "HealthCheck":
		return true
	default:
		return false
	}
}

func daemonObservedOnlyStatus(current, base map[string]any, observed daemonapi.ResourceStatus) map[string]any {
	next := copyStatusMap(current)
	for _, key := range []string{"conditions", "updatedAt"} {
		if value, ok := base[key]; ok {
			next[key] = value
		}
	}
	daemonObserved := normalizedDaemonObserved(observed.Resource.Kind, observed.Observed)
	daemonObserved["phase"] = observed.Phase
	daemonObserved["health"] = observed.Health
	next["observed"] = daemonObserved
	if daemonObservedPromotesTopLevel(observed.Resource.Kind) && strings.TrimSpace(observed.Phase) != "" {
		if observed.Resource.Kind == "HealthCheck" {
			clearHealthCheckProbeRoute(next)
			if strings.TrimSpace(fmt.Sprint(daemonObserved["lastResult"])) == healthcheck.ResultPassed {
				clearRecoveredHealthCheckFailureStatus(next)
			}
		}
		for key, value := range daemonObserved {
			next[key] = value
		}
		for _, key := range []string{"phase", "health", "conditions", "updatedAt"} {
			if value, ok := base[key]; ok {
				next[key] = value
			}
		}
		delete(next, "reason")
	}
	return next
}

// Route evidence describes the latest probe, not a merge with earlier probes.
// Optional fields disappear when a binding is removed or a route lookup fails.
// Event-only network.* attributes remain in event history, not current status.
func clearHealthCheckProbeRoute(status map[string]any) {
	for _, key := range []string{
		"via", "fwmark", "sourceInterface", "sourceAddress", "sourceOrigin",
		"egressInterface", "nextHop", "outInterface", "routeSource", "tunnelLocal", "tunnelRemote",
	} {
		delete(status, key)
	}
	for key := range status {
		if strings.HasPrefix(key, "network.") {
			delete(status, key)
		}
	}
}

// clearRecoveredHealthCheckFailureStatus removes event-only failure details
// once the health-check daemon has reported a successful probe. Historical
// timestamps (for example lastFailureTime) are deliberately retained.
func clearRecoveredHealthCheckFailureStatus(status map[string]any) {
	for _, key := range []string{
		"result",
		"message",
		"timeout",
		"failureKind",
		"routerd.healthcheck.failureKind",
	} {
		delete(status, key)
	}
}

func daemonObservedPromotesTopLevel(kind string) bool {
	switch kind {
	case "DHCPv6PrefixDelegation", "HealthCheck":
		return true
	default:
		return false
	}
}

func normalizedDaemonObserved(kind string, observed map[string]string) map[string]any {
	out := make(map[string]any, len(observed))
	for key, value := range observed {
		out[key] = normalizedDaemonObservedValue(kind, key, value)
	}
	return out
}

func normalizedDaemonObservedValue(kind, key, value string) any {
	switch key {
	case "dnsServers", "ntpServers", "sntpServers", "domainSearch":
		if values := decodeStringList(value); len(values) > 0 {
			return values
		}
	case "prefixLength":
		if kind == "DHCPv4Client" {
			if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && parsed > 0 {
				return parsed
			}
		}
	case "leaseTime":
		if kind == "DHCPv4Client" {
			if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
				return parsed
			}
		}
	case "bytesIn", "bytesOut":
		if kind == "PPPoESession" {
			if parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64); err == nil {
				return parsed
			}
		}
	case "listeners", "zones":
		if kind == "DNSResolver" {
			if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				return parsed
			}
		}
	case "queryLogRecords", "queryLogRecordErrors", "queryLogDBBytes", "queryLogWALBytes":
		if kind == "DNSResolver" {
			if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
				return parsed
			}
		}
	}
	return value
}

func (c DaemonStatusController) daemonSockets() ([]string, error) {
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
			// A disabled PD worker can still have a valid recovery snapshot.
			// Keep its unavailable-socket path reachable for source validation
			// and standby LAN staging; observing it never starts a UDP owner.
			socket := c.DaemonSockets[resource.Metadata.Name]
			if socket == "" {
				socket = defaultClientSocket("routerd-dhcpv6-client", resource.Metadata.Name)
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
			socket := c.DaemonSockets[resource.Metadata.Name]
			if socket == "" {
				defaults, _ := platform.Current()
				socket = filepath.Join(defaults.RuntimeDir, "healthcheck", resource.Metadata.Name+".sock")
			}
			add(socket)
		case "DNSResolver":
			socket := c.DaemonSockets[resource.Metadata.Name]
			if socket == "" {
				defaults, _ := platform.Current()
				socket = filepath.Join(defaults.RuntimeDir, "dns-resolver", resource.Metadata.Name+".sock")
			}
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
	workers, err := observedDHCPv6Workers(c.Router, c.Store)
	if err != nil {
		return nil, err
	}
	for _, worker := range workers {
		add(worker.socket(c.DaemonSockets))
	}
	runner := Runner{Router: c.Router, Store: c.Store, Opts: Options{DaemonSockets: c.DaemonSockets}}
	observers, err := runner.mobilityARPObserverDaemonSpecs()
	if err != nil {
		return nil, err
	}
	for _, spec := range observers {
		add(spec.Socket)
	}
	return out, nil
}

func cleanStatusString(value any) string {
	if value == nil {
		return ""
	}
	out := strings.TrimSpace(fmt.Sprint(value))
	if out == "<nil>" {
		return ""
	}
	return out
}

func ipv4StaticAddressDesiredByAnother(router *api.Router, resourceName, ifname, address string) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "IPv4StaticAddress" || resource.Metadata.Name == resourceName {
			continue
		}
		spec, err := resource.IPv4StaticAddressSpec()
		if err != nil {
			continue
		}
		if strings.TrimSpace(spec.Address) != strings.TrimSpace(address) {
			continue
		}
		if interfaceIfName(router, spec.Interface) == ifname {
			return true
		}
	}
	return false
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

func ipv4StaticAddressApplyCommand(osName platform.OS, ifname, address string) (string, []string) {
	if osName == platform.OSFreeBSD {
		return "ifconfig", []string{ifname, "inet", address, "alias"}
	}
	return "ip", []string{"-4", "addr", "replace", address, "dev", ifname}
}

func ipv4StaticAddressDeleteCommand(osName platform.OS, ifname, address string) (string, []string) {
	if osName == platform.OSFreeBSD {
		return "ifconfig", []string{ifname, "inet", address, "-alias"}
	}
	return "ip", []string{"-4", "addr", "del", address, "dev", ifname}
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
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] != "inet6" || localIPv6Address(fields[i+1]) != want {
				continue
			}
			if fieldsContain(fields, "tentative") || fieldsContain(fields, "dadfailed") {
				return false
			}
			return true
		}
	}
	return false
}

func ipv6AddressPresentWithPrefix(ctx context.Context, ifname, address string) bool {
	if platform.CurrentOS() == platform.OSFreeBSD {
		return ipv6AddressPresent(ctx, ifname, address)
	}
	want, err := netip.ParsePrefix(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	out, err := exec.CommandContext(ctx, "ip", "-6", "-o", "addr", "show", "dev", ifname).Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] != "inet6" {
				continue
			}
			got, err := netip.ParsePrefix(fields[i+1])
			if err != nil || got.Addr() != want.Addr() || got.Bits() != want.Bits() {
				continue
			}
			return !fieldsContain(fields, "tentative") && !fieldsContain(fields, "dadfailed")
		}
	}
	return false
}

func fieldsContain(fields []string, want string) bool {
	for _, field := range fields {
		if field == want {
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

func ipv6DeprecatedAddressApplyCommand(osName platform.OS, ifname, address string, validSeconds int64) (string, []string) {
	valid := strconv.FormatInt(validSeconds, 10)
	if osName == platform.OSFreeBSD {
		host := strings.TrimSpace(address)
		prefixLen := "64"
		if parsed, err := netip.ParsePrefix(host); err == nil {
			host = parsed.Addr().String()
			prefixLen = strconv.Itoa(parsed.Bits())
		}
		return "ifconfig", []string{ifname, "inet6", host, "prefixlen", prefixLen, "pltime", "0", "vltime", valid, "alias"}
	}
	return "ip", []string{"-6", "addr", "replace", address, "dev", ifname, "preferred_lft", "0", "valid_lft", valid}
}

func remainingIPv6ValidLifetime(now, expiresAt time.Time) int64 {
	if expiresAt.Equal(time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)) {
		return int64(infinitePDLifetime)
	}
	remaining := expiresAt.Sub(now)
	if remaining <= 0 {
		return 0
	}
	seconds := int64(remaining / time.Second)
	if seconds >= int64(infinitePDLifetime) {
		return int64(infinitePDLifetime) - 1
	}
	return seconds
}

func ipv6StaticAddressDeleteCommand(osName platform.OS, ifname, address string) (string, []string) {
	if osName == platform.OSFreeBSD {
		host := strings.TrimSpace(address)
		if parsed, err := netip.ParsePrefix(host); err == nil {
			host = parsed.Addr().String()
		} else if value, _, ok := strings.Cut(host, "/"); ok {
			host = value
		}
		return "ifconfig", []string{ifname, "inet6", host, "-alias"}
	}
	return "ip", []string{"-6", "addr", "del", address, "dev", ifname}
}

func delegatedAddressWithPrefixLength(address string, prefixLength int) (string, error) {
	prefix, err := netip.ParsePrefix(address)
	if err != nil {
		return "", err
	}
	if prefixLength == 0 || prefixLength == prefix.Bits() {
		return prefix.String(), nil
	}
	if prefixLength != 128 {
		return "", fmt.Errorf("unsupported prefixLength %d (want 64 or 128)", prefixLength)
	}
	return netip.PrefixFrom(prefix.Addr(), prefixLength).String(), nil
}

type managedPreviousDelegatedAddress struct {
	Address   string    `json:"address"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func previousDelegatedAddressesFromStatus(status map[string]any) []managedPreviousDelegatedAddress {
	switch raw := status["previousAddresses"].(type) {
	case []managedPreviousDelegatedAddress:
		return append([]managedPreviousDelegatedAddress(nil), raw...)
	case []any:
		var out []managedPreviousDelegatedAddress
		for _, value := range raw {
			entry, ok := value.(map[string]any)
			if !ok {
				return nil
			}
			address, ok := entry["address"].(string)
			if !ok {
				return nil
			}
			text, ok := entry["expiresAt"].(string)
			if !ok {
				return nil
			}
			deadline, err := time.Parse(time.RFC3339Nano, text)
			if err != nil {
				return nil
			}
			out = append(out, managedPreviousDelegatedAddress{Address: address, ExpiresAt: deadline})
		}
		return out
	}
	return nil
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
	if err := c.cleanupDelegatedReceipts(ctx, pdName); err != nil {
		return err
	}
	now := c.now()
	lease, found, err := livePDLease(c.Store, pdName, now)
	if err != nil {
		return err
	}
	activePD := found && lease.Phase == daemonapi.ResourcePhaseBound && lease.HasFreshTransactionEvidence(now)
	prefix := lease.CurrentPrefix
	if !activePD {
		declared := c.DeclaredRouter
		if declared == nil {
			declared = c.Router
		}
		if declared != nil {
			for _, resource := range declared.Spec.Resources {
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
				reason := "PDLeaseStale"
				stateStore, _ := c.Store.(resourcequery.StateStore)
				eligible, err := resourcequery.ResourceWhenEligible(spec.When, stateStore)
				if err != nil {
					return err
				}
				if !eligible {
					reason = "WhenFalse"
				}
				if err := c.saveAddressPending(resource.Metadata.Name, reason, ""); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if activePD && prefix == "" {
		return nil
	}

	resources := append([]api.Resource(nil), c.Router.Spec.Resources...)
	pdRuntimeEnabled := false
	for _, resource := range resources {
		if resource.Kind == "DHCPv6PrefixDelegation" && resource.Metadata.Name == pdName {
			pdRuntimeEnabled = true
			break
		}
	}

	if pdRuntimeEnabled {
		pdRuntimeEnabled, err = resourcequery.ReadSourceReady(c.Store, "DHCPv6PrefixDelegation/"+pdName)
		if err != nil {
			return err
		}
	}
	stagedIDs := map[string]bool{}
	if c.DeclaredRouter != nil && activePD {
		present := map[string]bool{}
		for _, resource := range resources {
			present[resource.ID()] = true
		}
		for _, resource := range c.DeclaredRouter.Spec.Resources {
			if resource.Kind != "IPv6DelegatedAddress" || present[resource.ID()] {
				continue
			}
			spec, err := resource.IPv6DelegatedAddressSpec()
			if err == nil && spec.PrefixDelegation == pdName && c.vrrpVMACConfigured(spec.Interface) {
				eligible, err := c.delegatedAddressStagingEligible(resource)
				if err != nil {
					return err
				}
				if !eligible {
					continue
				}
				resources = append(resources, resource)
				stagedIDs[resource.ID()] = true
			}
		}
	}
	for _, resource := range resources {
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
		stagingVMAC := stagedIDs[resource.ID()] || (!pdRuntimeEnabled && c.vrrpVMACConfigured(spec.Interface))
		dependenciesReady, err := c.delegatedAddressDependenciesReady(spec.DependsOn, spec.Interface, pdName, stagingVMAC)
		if err != nil {
			return err
		}
		if !dependenciesReady {
			if err := c.saveAddressPending(resource.Metadata.Name, "DependsOnFalse", ""); err != nil {
				return err
			}
			continue
		}
		if stagingVMAC {
			if err := c.ensureStagingVMAC(ctx, spec.Interface); err != nil || !c.stagingVMACPresent(spec.Interface) {
				reason := "VMACMissing"
				message := ""
				if err != nil {
					reason = "VMACEnsureFailed"
					message = err.Error()
				}
				if err := c.saveAddressPending(resource.Metadata.Name, reason, message); err != nil {
					return err
				}
				continue
			}
		}
		linkUp := stagingVMAC
		if !stagingVMAC {
			linkUp, err = c.linkReady(spec.Interface)
			if err != nil {
				return err
			}
		}
		if !linkUp {
			if err := c.saveAddressPending(resource.Metadata.Name, "", ""); err != nil {
				return err
			}
			continue
		}
		addr, err := DeriveIPv6Address(prefix, spec.SubnetID, spec.AddressSuffix)
		if err != nil {
			return err
		}
		addr, err = delegatedAddressWithPrefixLength(addr, spec.PrefixLength)
		if err != nil {
			return fmt.Errorf("%s derive address prefix length: %w", resource.ID(), err)
		}
		sourceRouter := c.DeclaredRouter
		if sourceRouter == nil {
			sourceRouter = c.Router
		}
		reader, ok := c.Store.(interface {
			ReadObjectStatus(string, string, string) (map[string]any, error)
		})
		if !ok {
			return fmt.Errorf("delegated address ownership reader unavailable")
		}
		previousStatus, err := reader.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", resource.Metadata.Name)
		if err != nil {
			return err
		}
		receipt, err := delegatedReceipt(previousStatus)
		if err != nil {
			return err
		}
		ifname := ""
		if c.DeclaredRouter != nil {
			ifname = interfaceIfName(c.DeclaredRouter, spec.Interface)
		}
		if ifname == "" {
			ifname = interfaceIfName(c.Router, spec.Interface)
		}
		if ifname == "" {
			ifname = spec.Interface
		}
		previousAddresses := []managedPreviousDelegatedAddress(nil)
		previousAddresses, err = derivePreviousPDLeaseAddresses(lease, prefix, spec, now)
		if err != nil {
			return fmt.Errorf("%s derive previous addresses: %w", resource.ID(), err)
		}
		// Canonical complete association is the sole lifetime authority.
		// Local receipts authorize exact cleanup, never an absent old prefix.
		journalPrevious := previousDelegatedAddressesFromStatus(previousStatus)

		status := map[string]any{
			"phase":        "Applied",
			"address":      addr,
			"interface":    spec.Interface,
			"ifname":       ifname,
			"prefixSource": pdName,
			"dryRun":       c.DryRun,
		}
		if c.DryRun {
			status["phase"] = "Rendered"
		}
		dsliteSource := c.currentOS() == platform.OSLinux && delegatedAddressUsedByDSLite(sourceRouter, resource.Metadata.Name)
		if dsliteSource {
			status["sourceSelection"] = "deprecated"
		}
		if stagingVMAC {
			status["staged"] = true
		}
		status["pdLeaseAcquiredAt"] = lease.LastReplyAt.UTC().Format(time.RFC3339Nano)
		status["preferredUntil"] = pdLifetimeDeadline(lease.LastReplyAt, lease.PreferredSeconds)
		status["validUntil"] = pdLifetimeDeadline(lease.LastReplyAt, lease.ValidSeconds)
		if len(previousAddresses) > 0 {
			status["previousAddresses"] = previousAddresses
		}
		changed := objectStatusChanged("IPv6DelegatedAddress", previousStatus, status)
		observation := delegatedAddressObservation{usable: true, deprecated: true}
		if !c.DryRun {
			observation, err = c.observeCurrentDelegatedAddress(ctx, ifname, addr, stagingVMAC)
			if err != nil {
				return fmt.Errorf("%s observe current address: %w", resource.ID(), err)
			}
		}
		if !c.DryRun {
			if observation.ifIndex < 1 || observation.scope == "" {
				if err := c.saveAddressPending(resource.Metadata.Name, "InterfaceNotPresent", ""); err != nil {
					return err
				}
				continue
			}
			if receipt != nil && (receipt.IfName != ifname || receipt.IfIndex != observation.ifIndex || receipt.Scope != observation.scope || receipt.ParentToken != observation.parentToken || receipt.PrefixDelegation != pdName) {
				return fmt.Errorf("%s native interface identity differs from receipt", resource.ID())
			}
			for _, existing := range observation.sameHost {
				if !receipt.ownedAt(existing, c.now()) {
					if receipt.has(existing) {
						return c.fenceDelegatedAddress(resource.Metadata.Name, previousStatus, receipt, existing)
					}
					return c.rejectForeignDelegatedAddress(resource.Metadata.Name, previousStatus, receipt, existing, ifname)
				}
			}
			if observation.present && !receipt.ownedAt(addr, c.now()) {
				if receipt.has(addr) {
					return c.fenceDelegatedAddress(resource.Metadata.Name, previousStatus, receipt, addr)
				}
				return c.rejectForeignDelegatedAddress(resource.Metadata.Name, previousStatus, receipt, addr, ifname)
			}
			if receipt == nil {
				receipt = &delegatedNativeReceipt{IfName: ifname, IfIndex: observation.ifIndex, Scope: observation.scope, ParentToken: observation.parentToken, PrefixDelegation: pdName}
			}
		}
		if !c.DryRun && !observation.present {
			receipt.forget(addr)
		}
		addressPresent := observation.usable
		currentOwned := receipt.ownedAt(addr, c.now())
		addressDeprecated := !dsliteSource || observation.deprecated
		command := c.Command
		if command == nil {
			command = runCommandContext
		}
		if !c.DryRun && observation.present && !observation.usable {
			reason := "AddressNotReady"
			if observation.dadFailed {
				reason = "AddressDADFailed"
			}
			if err := c.saveAddressPending(resource.Metadata.Name, reason, ""); err != nil {
				return err
			}
			continue
		}

		nativePreferred, nativeValid := nativePDAddressDeadlines(lease, dsliteSource)
		if !c.DryRun && (!receipt.matches(addr, nativePreferred, nativeValid) || !addressPresent || !addressDeprecated) {
			freshLease, err := c.currentAddressAuthority(pdName, prefix)
			if err != nil {
				return fmt.Errorf("%s before address apply: %w", resource.ID(), err)
			}
			preferred, valid, err := currentPDAddressLifetimes(freshLease, c.now(), dsliteSource)
			if err != nil {
				return fmt.Errorf("%s before address apply: %w", resource.ID(), err)
			}
			lease = freshLease
			status["pdLeaseAcquiredAt"] = lease.LastReplyAt.UTC().Format(time.RFC3339Nano)
			status["preferredUntil"] = pdLifetimeDeadline(lease.LastReplyAt, lease.PreferredSeconds)
			status["validUntil"] = pdLifetimeDeadline(lease.LastReplyAt, lease.ValidSeconds)
			pending := copyStatusMap(status)
			pending["phase"], pending["reason"] = "Pending", "AddressApplying"
			if history := journalPrevious; len(history) != 0 {
				pending["previousAddresses"] = history
			}
			nativePreferred, nativeValid = nativePDAddressDeadlines(lease, dsliteSource)
			receipt.claim(addr, nativePreferred, nativeValid)
			if err := c.saveDelegatedReceipt(resource.Metadata.Name, pending, receipt); err != nil {
				return err
			}

			freshLease, err = c.currentAddressAuthority(pdName, prefix)
			if err != nil {
				return err
			}
			// Persistence and observations can block. Recheck the exact parent
			// and native tuple before using the recorded acquisition intent.
			beforeWrite, err := c.observeCurrentDelegatedAddress(ctx, ifname, addr, stagingVMAC)
			if err != nil {
				return err
			}
			if !receipt.sameInterface(beforeWrite) || beforeWrite.present != observation.present {
				return fmt.Errorf("native delegated address or parent changed while recording intent")
			}
			for _, existing := range beforeWrite.sameHost {
				if !receipt.ownedAt(existing, c.now()) {
					return c.fenceDelegatedAddress(resource.Metadata.Name, pending, receipt, existing)
				}
			}
			// Recompute lifetimes after all blocking reads, so they cannot
			// extend the canonical absolute deadline.
			preferred, valid, err = currentPDAddressLifetimes(freshLease, c.now(), dsliteSource)
			if err != nil {
				return err
			}
			status["pdLeaseAcquiredAt"] = freshLease.LastReplyAt.UTC().Format(time.RFC3339Nano)
			status["preferredUntil"] = pdLifetimeDeadline(freshLease.LastReplyAt, freshLease.PreferredSeconds)
			status["validUntil"] = pdLifetimeDeadline(freshLease.LastReplyAt, freshLease.ValidSeconds)
			freshPreferred, freshValid := nativePDAddressDeadlines(freshLease, dsliteSource)
			if !freshPreferred.Equal(nativePreferred) || !freshValid.Equal(nativeValid) {
				return fmt.Errorf("PD authority changed while recording native intent")
			}
			name, args := ipv6DelegatedAddressApplyCommand(c.currentOS(), ifname, addr, preferred, valid)
			if !currentOwned && c.currentOS() == platform.OSLinux {
				args[2] = "add"
			}
			if err := command(ctx, name, args...); err != nil {
				return err
			}
			currentOwned = true
			after, readErr := c.observeCurrentDelegatedAddress(ctx, ifname, addr, stagingVMAC)
			if readErr != nil {
				return readErr
			}
			if !receipt.sameInterface(after) {
				return fmt.Errorf("native delegated interface changed during apply")
			}
			if err := c.checkDelegatedLifetimeReadback(after, preferred, valid); err != nil {
				return err
			}
			if after.present {
				receipt.confirm(addr)
				pendingNative := copyStatusMap(status)
				pendingNative["phase"], pendingNative["reason"] = "Pending", "AddressNotReady"
				if err := c.saveDelegatedReceipt(resource.Metadata.Name, pendingNative, receipt); err != nil {
					return err
				}
			}
			if !after.usable {
				reason := "AddressNotReady"
				if stagingVMAC {
					reason = "AddressReadbackFailed"
				}
				// A cold BACKUP can retain a tentative address as staged state, but
				// an active VMAC must not publish Applied until DAD has completed.
				// DS-Lite and its health checks consume this phase as a readiness
				// boundary and would otherwise transmit with an unusable outer source.
				if err := c.saveAddressPending(resource.Metadata.Name, reason, ""); err != nil {
					return err
				}
				continue
			}
		}
		if !c.DryRun {
			if _, err := c.currentAddressAuthority(pdName, prefix); err != nil {
				if currentOwned && errors.Is(err, errPDAddressAuthorityChanged) {
					if removeErr := c.withdrawDelegatedReceiptAddress(ctx, resource.Metadata.Name, status, receipt, addr); removeErr != nil {
						return errors.Join(err, removeErr)
					}
				}

				return fmt.Errorf("%s after address apply: %w", resource.ID(), err)
			}
		}
		if !c.DryRun {
			receipt.confirm(addr)
		}
		if !c.DryRun {
			for previousIndex, previous := range previousAddresses {
				freshPrevious, err := c.currentPreviousAddressAuthority(pdName, prefix, previous.Address, spec)
				if err != nil {
					return err
				}
				previous = freshPrevious
				previousAddresses[previousIndex] = freshPrevious
				validSeconds := remainingIPv6ValidLifetime(c.now(), previous.ExpiresAt)
				if validSeconds == 0 {
					continue
				}
				observedPrevious, readErr := c.observeCurrentDelegatedAddress(ctx, ifname, previous.Address, stagingVMAC)
				if readErr != nil {
					return readErr
				}
				present := observedPrevious.usable
				if !receipt.sameInterface(observedPrevious) {
					return fmt.Errorf("previous delegated interface identity changed")
				}
				for _, existing := range observedPrevious.sameHost {
					if !receipt.ownedAt(existing, c.now()) {
						return fmt.Errorf("%s foreign previous IPv6 address %s on %s", resource.ID(), existing, ifname)
					}
				}
				preferredDeadline := time.Unix(0, 0).UTC()
				if receipt.matches(previous.Address, preferredDeadline, previous.ExpiresAt) && present {
					continue
				}
				replaced := false
				for i := range journalPrevious {
					if journalPrevious[i].Address == previous.Address {
						journalPrevious[i] = previous
						replaced = true
						break
					}
				}
				if !replaced {
					journalPrevious = append(journalPrevious, previous)
				}
				pendingPrevious := copyStatusMap(status)
				pendingPrevious["phase"], pendingPrevious["reason"] = "Pending", "PreviousAddressApplying"
				pendingPrevious["previousAddresses"] = journalPrevious
				receipt.claim(previous.Address, preferredDeadline, previous.ExpiresAt)
				if err := c.saveDelegatedReceipt(resource.Metadata.Name, pendingPrevious, receipt); err != nil {
					return err
				}
				beforeWrite, err := c.observeCurrentDelegatedAddress(ctx, ifname, previous.Address, stagingVMAC)
				if err != nil {
					return err
				}
				if !receipt.sameInterface(beforeWrite) || beforeWrite.present != observedPrevious.present {
					return fmt.Errorf("previous delegated address or parent changed while recording intent")
				}
				for _, existing := range beforeWrite.sameHost {
					if !receipt.ownedAt(existing, c.now()) {
						return c.fenceDelegatedAddress(resource.Metadata.Name, pendingPrevious, receipt, existing)
					}
				}
				// FreeBSD SIOCAIFADDR_IN6 refreshes an existing alias in place.
				// Keep confirmed ownership across the lifetime update; deleting
				// first would create an unmarked absence/reacquisition window.

				freshPrevious, err = c.currentPreviousAddressAuthority(pdName, prefix, previous.Address, spec)
				if err != nil {
					return err
				}
				previousAddresses[previousIndex] = freshPrevious
				beforeApply, err := c.observeCurrentDelegatedAddress(ctx, ifname, previous.Address, stagingVMAC)
				if err != nil {
					return err
				}
				expectPresent := observedPrevious.present
				if !receipt.sameInterface(beforeApply) || beforeApply.present != expectPresent {
					return fmt.Errorf("previous delegated address or parent changed before apply")
				}
				validSeconds = remainingIPv6ValidLifetime(c.now(), freshPrevious.ExpiresAt)
				if validSeconds == 0 {
					return fmt.Errorf("previous delegated address expired before apply")
				}
				name, args := ipv6DeprecatedAddressApplyCommand(c.currentOS(), ifname, previous.Address, validSeconds)
				if !receipt.ownedAt(previous.Address, c.now()) && c.currentOS() == platform.OSLinux {
					args[2] = "add"
				}
				if err := command(ctx, name, args...); err != nil {
					return err
				}
				after, err := c.observeCurrentDelegatedAddress(ctx, ifname, previous.Address, stagingVMAC)
				if err != nil {
					return err
				}
				if !receipt.sameInterface(after) {
					return fmt.Errorf("previous delegated interface changed during apply")
				}
				if err := c.checkDelegatedLifetimeReadback(after, 0, uint32(validSeconds)); err != nil {
					return err
				}
				if after.present {
					receipt.confirm(previous.Address)
					pendingPrevious["reason"] = "AddressNotReady"
					if err := c.saveDelegatedReceipt(resource.Metadata.Name, pendingPrevious, receipt); err != nil {
						return err
					}
				}
				if !after.usable || !after.deprecated {
					return fmt.Errorf("previous delegated address readback differs from pending effect")
				}
			}

		}
		if err := c.saveDelegatedReceipt(resource.Metadata.Name, status, receipt); err != nil {
			return err
		}

		if (changed || !addressPresent || !addressDeprecated) && c.Bus != nil {
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

func delegatedAddressUsedByDSLite(router *api.Router, name string) bool {
	if router == nil || strings.TrimSpace(name) == "" {
		return false
	}
	var delegated api.IPv6DelegatedAddressSpec
	found := false
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "IPv6DelegatedAddress" || resource.Metadata.Name != strings.TrimSpace(name) {
			continue
		}
		var err error
		delegated, err = resource.IPv6DelegatedAddressSpec()
		if err != nil || delegated.PrefixLength != 128 {
			return false
		}
		found = true
		break
	}
	if !found {
		return false
	}
	delegatedSuffix, err := netip.ParseAddr(stringutil.FirstNonEmpty(strings.TrimSpace(delegated.AddressSuffix), "::1"))
	if err != nil || !delegatedSuffix.Is6() {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DSLiteTunnel" {
			continue
		}
		spec, err := resource.DSLiteTunnelSpec()
		if err != nil || stringutil.FirstNonEmpty(spec.LocalAddressSource, "interface") != "delegatedAddress" {
			continue
		}
		if strings.TrimSpace(spec.LocalDelegatedAddress) != strings.TrimSpace(name) {
			continue
		}
		endpointSuffix, err := netip.ParseAddr(stringutil.FirstNonEmpty(strings.TrimSpace(spec.LocalAddressSuffix), strings.TrimSpace(delegated.AddressSuffix), "::1"))
		if err == nil && endpointSuffix.Is6() && endpointSuffix == delegatedSuffix {
			return true
		}
	}
	return false
}

// delegatedAddressStagingEligible distinguishes an unresolved startup role
// from an explicitly false resource condition. A fresh PD snapshot may stage
// an unconditional or indeterminate delegated address, but it must never
// override a resolved when=false decision.
func (c LANAddressController) delegatedAddressStagingEligible(resource api.Resource) (bool, error) {
	when := resourcequery.ResourceWhen(resource)
	if !resourcequery.ResourceWhenPresent(when) {
		return true, nil
	}
	stateStore, ok := c.Store.(resourcequery.StateStore)
	if !ok {
		return false, fmt.Errorf("state-variable reading is unavailable")
	}
	return resourcequery.ResourceWhenEligible(when, stateStore)
}

func (c LANAddressController) delegatedAddressDependenciesReady(dependencies []api.ResourceDependencySpec, iface, pdName string, stagingVMAC bool) (bool, error) {
	if !stagingVMAC {
		return resourcequery.ReadDependenciesReady(c.Store, dependencies)
	}
	filtered := make([]api.ResourceDependencySpec, 0, len(dependencies))
	for _, dep := range dependencies {
		if dep.Resource == "Interface/"+iface && dep.Phase == "Up" {
			continue
		}
		if dep.Resource == "DHCPv6PrefixDelegation/"+pdName && dep.Phase == daemonapi.ResourcePhaseBound {
			continue
		}
		filtered = append(filtered, dep)
	}
	return resourcequery.ReadDependenciesReady(c.Store, filtered)
}

// stagingVMACPresent accepts only the fully cold BACKUP shape.  In particular,
// a VMAC left UP or retaining any link-local identity must be reconciled by the
// helper before delegated addresses can be staged.
func (c LANAddressController) stagingVMACPresent(logical string) bool {
	if c.VMACPresent != nil {
		return c.VMACPresent(logical)
	}
	return c.vrrpVMACReadbackPresent(logical)
}

func (c LANAddressController) vrrpVMACReadbackPresent(logical string) bool {
	router := c.DeclaredRouter
	if router == nil {
		router = c.Router
	}
	if router == nil {
		return false
	}
	ifname := interfaceIfName(router, logical)
	if ifname == "" {
		return false
	}
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "VirtualAddress" {
			continue
		}
		spec, err := resource.VirtualAddressSpec()
		if err != nil || spec.Mode != "vrrp" {
			continue
		}
		entries := append([]api.VirtualAddressVRRPFailoverVMACSpec(nil), spec.VRRP.AdditionalFailoverVMACs...)
		if spec.VRRP.FailoverVMAC != nil {
			entries = append(entries, *spec.VRRP.FailoverVMAC)
		}
		for _, entry := range entries {
			if entry.Interface == ifname {
				addrs, addrErr := ifi.Addrs()
				return addrErr == nil && vrrpVMACStagingReadbackMatches(entry, ifi.HardwareAddr, ifi.Flags, addrs)
			}
		}
	}
	return false
}

func vrrpVMACActiveReadbackMatches(entry api.VirtualAddressVRRPFailoverVMACSpec, mac net.HardwareAddr, flags net.Flags, addrs []net.Addr) bool {
	if flags&net.FlagUp == 0 || !vrrpVMACMACMatches(entry, mac) {
		return false
	}
	return vrrpVMACHasConfiguredLinkLocal(entry, addrs)
}

func vrrpVMACStagingReadbackMatches(entry api.VirtualAddressVRRPFailoverVMACSpec, mac net.HardwareAddr, flags net.Flags, addrs []net.Addr) bool {
	return flags&net.FlagUp == 0 && vrrpVMACMACMatches(entry, mac) && !hasIPv6LinkLocal(addrs)
}

func vrrpVMACMACMatches(entry api.VirtualAddressVRRPFailoverVMACSpec, mac net.HardwareAddr) bool {
	wantMAC, err := net.ParseMAC(entry.MACAddress)
	if err != nil || !strings.EqualFold(mac.String(), wantMAC.String()) {
		return false
	}
	return true
}

func vrrpVMACHasConfiguredLinkLocal(entry api.VirtualAddressVRRPFailoverVMACSpec, addrs []net.Addr) bool {
	if strings.TrimSpace(entry.LinkLocalAddress) == "" {
		return true
	}
	wantLL := net.ParseIP(entry.LinkLocalAddress)
	if wantLL == nil {
		return false
	}
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err == nil && ip.Equal(wantLL) {
			return true
		}
	}
	return false
}

func hasIPv6LinkLocal(addrs []net.Addr) bool {
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err == nil && ip.IsLinkLocalUnicast() && ip.To4() == nil {
			return true
		}
	}
	return false
}

func (c LANAddressController) vrrpVMACConfigured(logical string) bool {
	_, ok := c.vrrpVMAC(logical)
	return ok
}

func (c LANAddressController) ensureStagingVMAC(ctx context.Context, logical string) error {
	if c.stagingVMACPresent(logical) {
		return nil
	}
	entry, owner, ok := c.vrrpVMACOwner(logical)
	if !ok {
		return fmt.Errorf("no VRRP VMAC configured for %s", logical)
	}
	if c.EnsureVMAC != nil {
		return c.EnsureVMAC(ctx, logical)
	}
	router := c.DeclaredRouter
	if router == nil {
		router = c.Router
	}
	parent := interfaceIfName(router, entry.ParentInterface)
	if parent == "" {
		// ParentInterface normally names an Interface resource alias.  Preserve
		// the helper's direct-interface contract for configurations that provide
		// an actual Linux interface name instead.
		parent = entry.ParentInterface
	}
	command := c.Command
	args := []string{"deactivate", "--vmac", parent + "," + entry.Interface + "," + entry.MACAddress + "," + entry.LinkLocalAddress + "," + fmt.Sprintf("%t", entry.WithdrawRouterAdvertisement)}
	if owner != "" {
		// A fresh PD snapshot is reconciled both while the VMAC is a cold
		// BACKUP and during the first MASTER pass before the active PD client is
		// Bound.  Guard the staging repair with the same durable election role
		// used by the authoritative keepalived and VRRP reconciliation paths.
		// Otherwise this call can turn a newly activated MASTER VMAC back DOWN.
		args = append(args, "--guard-resource", owner, "--reconcile")
	}
	if command != nil {
		return command(ctx, "/usr/local/sbin/routerd-vrrp-vmac", args...)
	}
	output, err := exec.CommandContext(ctx, "/usr/local/sbin/routerd-vrrp-vmac", args...).CombinedOutput()
	if err != nil {
		return vmacHelperCommandError(args, err, output)
	}
	return nil
}

func vmacHelperCommandError(args []string, err error, output []byte) error {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return fmt.Errorf("routerd-vrrp-vmac %s: %w", strings.Join(args, " "), err)
	}
	return fmt.Errorf("routerd-vrrp-vmac %s: %w: %s", strings.Join(args, " "), err, message)
}

func (c LANAddressController) vrrpVMAC(logical string) (api.VirtualAddressVRRPFailoverVMACSpec, bool) {
	entry, _, ok := c.vrrpVMACOwner(logical)
	return entry, ok
}

func (c LANAddressController) vrrpVMACOwner(logical string) (api.VirtualAddressVRRPFailoverVMACSpec, string, bool) {
	router := c.DeclaredRouter
	if router == nil {
		router = c.Router
	}
	if router == nil {
		return api.VirtualAddressVRRPFailoverVMACSpec{}, "", false
	}
	ifname := interfaceIfName(router, logical)
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "VirtualAddress" {
			continue
		}
		spec, err := resource.VirtualAddressSpec()
		if err != nil || spec.Mode != "vrrp" {
			continue
		}
		entries := append([]api.VirtualAddressVRRPFailoverVMACSpec(nil), spec.VRRP.AdditionalFailoverVMACs...)
		if spec.VRRP.FailoverVMAC != nil {
			entries = append(entries, *spec.VRRP.FailoverVMAC)
		}
		for _, entry := range entries {
			if entry.Interface == ifname {
				return entry, resource.Metadata.Name, true
			}
		}
	}
	return api.VirtualAddressVRRPFailoverVMACSpec{}, "", false
}

func statusStringPreferObserved(status map[string]any, field string) string {
	if observed := statusValueMap(status["observed"]); len(observed) > 0 {
		text := strings.TrimSpace(fmt.Sprint(observed[field]))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	text := strings.TrimSpace(fmt.Sprint(status[field]))
	if text == "<nil>" {
		return ""
	}
	return text
}

func (c LANAddressController) linkReady(name string) (bool, error) {
	status, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "Interface", name)
	if err != nil {
		return false, err
	}
	if status != nil {
		if phase := fmt.Sprint(status["phase"]); phase != "" && phase != "<nil>" {
			return phase == "Up", nil
		}
	}
	ifname := interfaceIfName(c.Router, name)
	if ifname == "" {
		ifname = name
	}
	ifi, err := net.InterfaceByName(ifname)
	if err == nil && ifi.Flags&net.FlagUp != 0 {
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "Interface", name, map[string]any{"phase": "Up", "ifname": ifname}); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func interfaceIfName(router *api.Router, name string) string {
	return api.ResolveInterfaceIfName(router, name)
}

func routerNeedsDnsmasq(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "DHCPv4Server", "DHCPv4Relay":
			return true
		case "DHCPv6Server":
			spec, err := resource.DHCPv6ServerSpec()
			if err != nil || spec.Mode != "ra-only" {
				return true
			}
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

func writeDnsmasqConfig(router *api.Router, store Store, path, pidFile string, port int, listenAddresses []string) (bool, bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, false, err
	}
	if err := os.MkdirAll(filepath.Dir(pidFile), 0755); err != nil {
		return false, false, err
	}
	leaseFile, err := dnsmasqLeaseFile(router, path, pidFile)
	if err != nil {
		return false, false, err
	}
	if err := os.MkdirAll(filepath.Dir(leaseFile), 0755); err != nil {
		return false, false, err
	}
	var b strings.Builder
	b.WriteString(routerdGeneratedDNSMasqMarker)
	fmt.Fprintf(&b, "port=0\nno-resolv\nno-hosts\nbind-dynamic\npid-file=%s\ndhcp-leasefile=%s\n", pidFile, leaseFile)
	hostsFile := dnsmasqHostsFile(path)
	lines, err := dnsmasqLANServiceLines(router, store)
	if err != nil {
		return false, false, err
	}
	hostsChanged, err := writeDnsmasqHostsFile(router, hostsFile)
	if err != nil {
		return false, false, err
	}
	if routerHasDnsmasqHostsFile(router) {
		b.WriteString("dhcp-hostsfile=" + hostsFile + "\n")
	}
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	data := []byte(b.String())
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, data) {
		return false, hostsChanged, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, false, err
	}
	return true, hostsChanged, os.WriteFile(path, data, 0644)
}

func dnsmasqLeaseFile(router *api.Router, configPath, pidFile string) (string, error) {
	defaults, _ := platform.Current()
	return DNSMasqLeaseFile(router, defaults.DnsmasqLeaseFile())
}

// DNSMasqLeaseFile shares explicit lease-file selection with callback recovery.
func DNSMasqLeaseFile(router *api.Router, fallback string) (string, error) {
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
	return fallback, nil
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
	var lines []string
	for _, ifname := range render.DnsmasqIPv4OnlyInterfaces(router, aliases) {
		lines = append(lines, "no-dhcpv6-interface="+ifname)
	}
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
		pending, err := dhcpv4ServerPending(router, store, spec)
		if err != nil {
			return nil, err
		}
		if pending != "" {
			continue
		}
		tag := sanitizeChainTag(resource.Metadata.Name)
		lines = append(lines, "interface="+ifname)
		lines = append(lines, "dhcp-script=/usr/local/libexec/routerd/dhcp-event-relay")
		start := stringutil.FirstNonEmpty(spec.AddressPool.Start, spec.RangeStart)
		end := stringutil.FirstNonEmpty(spec.AddressPool.End, spec.RangeEnd)
		leaseTime := stringutil.FirstNonEmpty(spec.AddressPool.LeaseTime, spec.LeaseTime, "12h")
		gatewayValue, err := resourcequery.ReadValue(store, spec.GatewayFrom)
		if err != nil {
			return nil, err
		}
		gateway := stringutil.FirstNonEmpty(statusvalue.Address(gatewayValue), spec.Gateway)
		dnsServerSources, _, err := expandIPv4DHCPServerSources(store, spec.DNSServerFrom, "DNSServerFrom")
		if err != nil {
			return nil, err
		}
		ntpServerSources, _, err := expandIPv4DHCPServerSources(store, spec.NTPServerFrom, "NTPServerFrom")
		if err != nil {
			return nil, err
		}
		dnsServers := append(expandIPv4DHCPServers(spec.DNSServers), dnsServerSources...)
		ntpServers := append(expandIPv4DHCPServers(spec.NTPServers), ntpServerSources...)
		domains, _, err := expandDomainValues(router, store, []string{spec.Domain}, []api.StatusValueSourceSpec{spec.DomainFrom}, "DomainFrom")
		if err != nil {
			return nil, err
		}
		profiles := make(map[string]api.DHCPv4ServerProfileSpec, len(spec.Profiles))
		for _, profile := range spec.Profiles {
			profiles[profile.Name] = profile
		}
		for _, scope := range spec.Scopes {
			addressPool := scope.AddressPool
			netmask := scope.Netmask
			gateway := scope.Gateway
			dnsServers := scope.DNSServers
			ntpServers := scope.NTPServers
			options := scope.Options
			if profile, ok := profiles[scope.ProfileRef]; ok && scope.ProfileRef != "" {
				addressPool = profile.AddressPool
				netmask = profile.Netmask
				gateway = profile.Gateway
				dnsServers = profile.DNSServers
				ntpServers = profile.NTPServers
				options = profile.Options
			}
			scopeTag := sanitizeChainTag(resource.Metadata.Name + "-" + scope.Name)
			matchTag := scopeTag + "-match"
			for _, mac := range scope.Match.MACAddresses {
				lines = append(lines, "dhcp-mac=set:"+matchTag+","+strings.ToLower(mac))
			}
			for _, oui := range scope.Match.OUIPrefixes {
				lines = append(lines, "dhcp-mac=set:"+matchTag+","+strings.ToLower(oui)+":*:*:*")
			}
			scopeLease := stringutil.FirstNonEmpty(addressPool.LeaseTime, leaseTime)
			scopeRange := fmt.Sprintf("dhcp-range=tag:%s,set:%s,%s,%s", matchTag, scopeTag, addressPool.Start, addressPool.End)
			if netmask != "" {
				scopeRange += "," + netmask
			}
			lines = append(lines, scopeRange+","+scopeLease)
			if gateway != "" {
				lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:router,%s", scopeTag, gateway))
			}
			if len(dnsServers) > 0 {
				lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:dns-server,%s", scopeTag, strings.Join(dnsServers, ",")))
			}
			if len(ntpServers) > 0 {
				lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:ntp-server,%s", scopeTag, strings.Join(ntpServers, ",")))
			}
			for _, option := range options {
				lines = append(lines, "dhcp-option=tag:"+scopeTag+","+dnsmasqDHCPv4Option(option))
			}
		}
		// Profile ranges must be evaluated before the catch-all range. The selector
		// tag is intentionally separate from the range tag: an old lease from the
		// catch-all pool then receives only the catch-all options, never a mixed
		// address/default-gateway lease while it is being migrated.
		lines = append(lines, fmt.Sprintf("dhcp-range=set:%s,%s,%s,%s", tag, start, end, leaseTime))
		if gateway != "" {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:router,%s", tag, gateway))
		}
		if len(dnsServers) > 0 {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:dns-server,%s", tag, strings.Join(dnsServers, ",")))
		}
		if len(ntpServers) > 0 {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:ntp-server,%s", tag, strings.Join(ntpServers, ",")))
		}
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
			for _, option := range reservationSpec.Options {
				lines = append(lines, "dhcp-option=tag:"+sanitizeChainTag(reservation.Metadata.Name)+","+dnsmasqDHCPv4Option(option))
			}
		}
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DHCPv6Server" {
			continue
		}
		spec, err := resource.DHCPv6ServerSpec()
		if err != nil {
			return nil, err
		}
		if spec.Mode == "ra-only" {
			continue
		}
		if spec.Interface == "" && spec.DelegatedAddress != "" {
			for _, candidate := range router.Spec.Resources {
				if candidate.Kind == "IPv6DelegatedAddress" && candidate.Metadata.Name == spec.DelegatedAddress {
					value, err := candidate.IPv6DelegatedAddressSpec()
					if err != nil {
						return nil, err
					}
					spec.Interface = value.Interface
					break
				}
			}
		}
		if spec.Interface == "" {
			continue
		}
		ready, err := resourcequery.ReadDependenciesReady(store, spec.DependsOn)
		if err != nil {
			return nil, err
		}
		if !ready {
			continue
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			ifname = spec.Interface
		}
		pending, err := dhcpv6ServerPending(router, store, spec)
		if err != nil {
			return nil, err
		}
		if pending != "" {
			continue
		}
		tag := sanitizeChainTag(resource.Metadata.Name)
		lines = append(lines, "interface="+ifname)
		if line := render.DnsmasqDHCPv6Range(resource.Metadata.Name, ifname, spec); line != "" {
			lines = append(lines, line)
		}
		dnsServerSources, _, err := expandServerSources(store, spec.DNSServerFrom, "DNSServerFrom")
		if err != nil {
			return nil, err
		}
		for _, server := range append(expandServers(spec.DNSServers), dnsServerSources...) {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option6:dns-server,[%s]", tag, dnsmasqIPv6Address(server)))
		}
		searchDomains, _, err := expandDomainValues(router, store, spec.DomainSearch, spec.DomainSearchFrom, "DomainSearchFrom")
		if err != nil {
			return nil, err
		}
		if len(searchDomains) > 0 {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option6:domain-search,%s", tag, strings.Join(searchDomains, ",")))
		}
		sntpServerSources, _, err := expandServerSources(store, spec.SNTPServerFrom, "SNTPServerFrom")
		if err != nil {
			return nil, err
		}
		for _, server := range append(expandServers(spec.SNTPServers), sntpServerSources...) {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option6:sntp-server,[%s]", tag, dnsmasqIPv6Address(server)))
		}
		if spec.RapidCommit {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option6:rapid-commit", tag))
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
	if router == nil {
		return aliases
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "Interface", "Bridge", "VXLANSegment", "VXLANTunnel", "VRF", "WireGuardInterface", "PPPoESession", "DSLiteTunnel":
			aliases[resource.Metadata.Name] = api.ResolveInterfaceIfName(router, resource.Metadata.Name)
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

func dnsmasqStickyHostLines(router *api.Router, family, leaseTime string) []string {
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
	return dnsmasqStickyHostLinesFromRows(router, rows, family, leaseTime)
}

func dnsmasqStickyHostLinesFromRows(router *api.Router, rows []logstore.DHCPStickyLease, family, leaseTime string) []string {
	var hosts []render.DHCPStickyHost
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
		hosts = append(hosts, render.DHCPStickyHost{
			MACAddress: row.MAC,
			IPAddress:  row.IP,
			Hostname:   row.Hostname,
			Family:     rowFamily,
			LeaseTime:  leaseTime,
		})
	}
	reservationEntries := dnsmasqReservationEntries(router)
	reservations := make([]api.DHCPv4ReservationSpec, 0, len(reservationEntries))
	for _, reservation := range reservationEntries {
		reservations = append(reservations, reservation.spec)
	}
	var lines []string
	for _, host := range render.ExcludeReservedIPv4StickyHosts(hosts, reservations) {
		parts := []string{strings.ToLower(host.MACAddress), host.IPAddress}
		if host.Hostname != "" {
			parts = append(parts, host.Hostname)
		}
		if host.LeaseTime != "" {
			parts = append(parts, host.LeaseTime)
		}
		lines = append(lines, "dhcp-host="+strings.Join(parts, ","))
	}
	sort.Strings(lines)
	return lines
}

func routerHasDnsmasqHostsFile(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "DHCPv4Reservation":
			return true
		case "DHCPv4Server":
			spec, err := resource.DHCPv4ServerSpec()
			if err == nil && spec.StickyHoldDays > 0 {
				return true
			}
		case "DHCPv6Server":
			spec, err := resource.DHCPv6ServerSpec()
			if err == nil && spec.StickyHoldDays > 0 {
				return true
			}
		}
	}
	return false
}

func dnsmasqHostsFile(configPath string) string {
	if dir := strings.TrimSpace(filepath.Dir(configPath)); dir != "" && dir != "." {
		return filepath.Join(dir, "dnsmasq-hosts.hosts")
	}
	defaults, _ := platform.Current()
	return filepath.Join(defaults.RuntimeDir, "dnsmasq-hosts.hosts")
}

func writeDnsmasqHostsFile(router *api.Router, path string) (bool, error) {
	if strings.TrimSpace(path) == "" || !routerHasDnsmasqHostsFile(router) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	var lines []string
	lines = append(lines, dnsmasqReservationHostLines(router)...)
	if routerHasDHCPStickyHold(router) {
		lines = append(lines, dnsmasqHostFileLines(dnsmasqStickyHostLines(router, "ipv4", "12h"))...)
		lines = append(lines, dnsmasqHostFileLines(dnsmasqStickyHostLines(router, "ipv6", "12h"))...)
	}
	sort.Strings(lines)
	data := []byte(routerdGeneratedDNSMasqMarker + strings.Join(lines, "\n"))
	if len(data) > 0 {
		data = append(data, '\n')
	}
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, data) {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, os.WriteFile(path, data, 0644)
}

func dnsmasqHostFileLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "dhcp-host=")
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func routerHasDHCPStickyHold(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "DHCPv4Server":
			spec, err := resource.DHCPv4ServerSpec()
			if err == nil && spec.StickyHoldDays > 0 {
				return true
			}
		case "DHCPv6Server":
			spec, err := resource.DHCPv6ServerSpec()
			if err == nil && spec.StickyHoldDays > 0 {
				return true
			}
		}
	}
	return false
}

type dnsmasqReservationEntry struct {
	name string
	spec api.DHCPv4ReservationSpec
}

func dnsmasqReservationEntries(router *api.Router) []dnsmasqReservationEntry {
	if router == nil {
		return nil
	}
	servers := map[string]bool{}
	for _, resource := range router.Spec.Resources {
		if resource.Kind == "DHCPv4Server" {
			servers[resource.Metadata.Name] = true
		}
	}
	var entries []dnsmasqReservationEntry
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DHCPv4Reservation" {
			continue
		}
		spec, err := resource.DHCPv4ReservationSpec()
		if err != nil {
			continue
		}
		if strings.TrimSpace(spec.Scope) != "" {
			continue
		}
		if server := strings.TrimSpace(spec.Server); server != "" && !servers[server] {
			continue
		}
		entries = append(entries, dnsmasqReservationEntry{name: resource.Metadata.Name, spec: spec})
	}
	return entries
}

func dnsmasqReservationHostLines(router *api.Router) []string {
	entries := dnsmasqReservationEntries(router)
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, dnsmasqIPv4Reservation(entry.spec, sanitizeChainTag(entry.name)))
	}
	return lines
}

func expandIPv4DHCPServers(values []string) []string {
	var out []string
	for _, value := range values {
		if address := statusvalue.Address(value); address != "" {
			out = append(out, address)
		}
	}
	return out
}

func expandIPv4DHCPServerSources(store Store, sources []api.StatusValueSourceSpec, label string) ([]string, string, error) {
	var out []string
	for _, source := range sources {
		before := len(out)
		values, err := resourcequery.ReadValues(store, source)
		if err != nil {
			return nil, "", err
		}
		for _, value := range values {
			if address := statusvalue.Address(value); address != "" {
				out = append(out, address)
			}
		}
		if len(out) == before {
			if pending := unresolvedStatusSourceReason(label, source); pending != "" {
				return out, pending, nil
			}
		}
	}
	return out, "", nil
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

func expandServers(values []string) []string {
	var out []string
	for _, value := range values {
		resolved := value
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

func expandServerSources(store Store, sources []api.StatusValueSourceSpec, label string) ([]string, string, error) {
	var out []string
	for _, source := range sources {
		values, err := resourcequery.ReadValues(store, source)
		if err != nil {
			return nil, "", err
		}
		values = compactNonEmptyStrings(values)
		if len(values) == 0 {
			if pending := unresolvedStatusSourceReason(label, source); pending != "" {
				return out, pending, nil
			}
			continue
		}
		out = append(out, values...)
	}
	return out, "", nil
}

func expandDomainValues(router *api.Router, store Store, values []string, sources []api.StatusValueSourceSpec, label string) ([]string, string, error) {
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
		resolved, err := resourcequery.ReadValuesFromStoreOrRouter(store, router, source)
		if err != nil {
			return nil, "", err
		}
		resolved = compactNonEmptyStrings(resolved)
		if len(resolved) == 0 {
			if pending := unresolvedStatusSourceReason(label, source); pending != "" {
				return compactNonEmptyStrings(out), pending, nil
			}
			continue
		}
		out = append(out, resolved...)
	}
	return compactNonEmptyStrings(out), "", nil
}

func dhcpv4ServerPending(router *api.Router, store Store, spec api.DHCPv4ServerSpec) (string, error) {
	if strings.TrimSpace(spec.GatewayFrom.Resource) != "" {
		value, err := resourcequery.ReadValue(store, spec.GatewayFrom)
		if err != nil {
			return "", err
		}
		if address := statusvalue.Address(value); address == "" {
			if pending := unresolvedStatusSourceReason("GatewayFrom", spec.GatewayFrom); pending != "" {
				return pending, nil
			}
		}
	}
	if _, pending, err := expandIPv4DHCPServerSources(store, spec.DNSServerFrom, "DNSServerFrom"); pending != "" || err != nil {
		return pending, err
	}
	if _, pending, err := expandIPv4DHCPServerSources(store, spec.NTPServerFrom, "NTPServerFrom"); pending != "" || err != nil {
		return pending, err
	}
	if _, pending, err := expandDomainValues(router, store, nil, []api.StatusValueSourceSpec{spec.DomainFrom}, "DomainFrom"); pending != "" || err != nil {
		return pending, err
	}
	return "", nil
}

func dhcpv6ServerPending(router *api.Router, store Store, spec api.DHCPv6ServerSpec) (string, error) {
	if _, pending, err := expandServerSources(store, spec.DNSServerFrom, "DNSServerFrom"); pending != "" || err != nil {
		return pending, err
	}
	if _, pending, err := expandServerSources(store, spec.SNTPServerFrom, "SNTPServerFrom"); pending != "" || err != nil {
		return pending, err
	}
	if _, pending, err := expandDomainValues(router, store, nil, spec.DomainSearchFrom, "DomainSearchFrom"); pending != "" || err != nil {
		return pending, err
	}
	return "", nil
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

	command = stringutil.FirstNonEmpty(command, "dnsmasq")
	if err := testDnsmasqConfig(ctx, command, configPath); err != nil {
		return err
	}
	defaults, features := platform.Current()
	if features.HasSystemd {
		return ensureSystemdDnsmasqService(ctx, defaults.SystemdSystemDir, command, configPath, pidFile, changed)
	}
	spec, err := dnsmasqSpec(command, configPath, pidFile)
	if err != nil {
		return err
	}
	return ensureNativeDnsmasq(ctx, spec, changed)
}

func reloadDnsmasq(ctx context.Context, configPath, pidFile string) error {
	defaults, features := platform.Current()
	if features.HasSystemd {
		return reloadSystemdDnsmasq(ctx, filepath.Join(defaults.SystemdSystemDir, "routerd-dnsmasq.service"), configPath, pidFile)
	}
	return reloadOwnedDnsmasqHosts(ctx, pidFile)
}

type dnsmasqHostsSnapshot struct {
	data   []byte
	mode   os.FileMode
	exists bool
}

func snapshotDnsmasqHostsFile(path string) (dnsmasqHostsSnapshot, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return dnsmasqHostsSnapshot{}, nil
	}
	if err != nil {
		return dnsmasqHostsSnapshot{}, fmt.Errorf("read dnsmasq hosts sidecar %s before update: %w", path, err)
	}
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	return dnsmasqHostsSnapshot{data: data, mode: mode, exists: true}, nil
}

func restoreDnsmasqHostsForRetry(configPath string, sidecarChanged bool, snapshot dnsmasqHostsSnapshot, reloadErr error) error {
	if !sidecarChanged {
		return reloadErr
	}
	path := dnsmasqHostsFile(configPath)
	var restoreErr error
	if snapshot.exists {
		restoreErr = os.WriteFile(path, snapshot.data, snapshot.mode)
	} else if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		restoreErr = err
	}
	if restoreErr != nil {
		return errors.Join(reloadErr, fmt.Errorf("restore dnsmasq hosts sidecar %s for retry: %w", path, restoreErr))
	}
	return reloadErr
}

func reloadDnsmasqHostsSidecar(ctx context.Context, pidFile, configPath string, snapshot dnsmasqHostsSnapshot, reload func(context.Context, string) error) error {
	if err := reload(ctx, pidFile); err != nil {
		return restoreDnsmasqHostsForRetry(configPath, true, snapshot, err)
	}
	return nil
}

func chainFirstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	return stringutil.FirstNonEmpty(values...)
}

func atoi(value string) int {
	var out int
	_, _ = fmt.Sscanf(value, "%d", &out)
	return out
}
