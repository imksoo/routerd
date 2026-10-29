// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4client

import (
	"context"
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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/lifecycle"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	EventApplied = "routerd.dhcpv4.client.applied"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error)
	ListObjectStatuses() ([]routerstate.ObjectStatus, error)
	DeleteObject(apiVersion, kind, name string) error
}

type outputCommandFunc func(context.Context, string, ...string) ([]byte, error)

type Controller struct {
	Router         *api.Router
	Bus            *bus.Bus
	Store          Store
	DaemonSockets  map[string]string
	DryRun         bool
	Command        outputCommandFunc
	Logger         *slog.Logger
	ResolvConfPath string
	mu             sync.Mutex
	prepared       bool
	prepareErr     error
	leases         []preparedLease
	nextExpiry     time.Time
}

type preparedLease struct {
	name   string
	ifname string
	spec   api.DHCPv4ClientSpec
}

func (c *Controller) Start(ctx context.Context) {
	if c.Router == nil || c.Bus == nil || c.Store == nil {
		return
	}
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.dhcpv4.client.*"}}, 32)
	go func() {
		for event := range ch {
			if event.Resource == nil || event.Resource.Kind != "DHCPv4Client" {
				continue
			}
			if err := c.Reconcile(ctx, event.Resource.Name); err != nil && c.Logger != nil {
				c.Logger.Warn("DHCPv4 lease reconcile failed", "resource", event.Resource.Name, "error", err)
			}
		}
	}()
}

func (c *Controller) Reconcile(ctx context.Context, name string) error {
	if c.Router == nil {
		return errors.New("DHCPv4 router configuration is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.reconcile(ctx, name)
	if c.Store == nil {
		return err
	}
	statuses, readErr := c.Store.ListObjectStatuses()
	if readErr == nil {
		c.observeExpiry(statuses)
	}
	return errors.Join(err, readErr)
}

func (c *Controller) ReconcileAll(ctx context.Context) error {
	if c.Router == nil {
		return errors.New("DHCPv4 router configuration is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepare(); err != nil {
		return err
	}
	if c.Store == nil {
		return errors.New("DHCPv4 status store is required")
	}
	statuses, err := c.Store.ListObjectStatuses()
	if err != nil {
		return err
	}
	c.observeExpiry(statuses)
	var errs []error
	if err := c.cleanupRemoved(ctx, statuses); err != nil {
		errs = append(errs, err)
	}
	for _, lease := range c.leases {
		if err := c.reconcile(ctx, lease.name); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", lease.name, err))
		}
	}
	// Re-read after successful or partial effects so display history cannot
	// retain a safety deadline once there is no authority left to withdraw.
	statuses, readErr := c.Store.ListObjectStatuses()
	if readErr != nil {
		errs = append(errs, readErr)
	} else {
		c.observeExpiry(statuses)
	}
	return errors.Join(errs...)
}

func (c *Controller) observeExpiry(statuses []routerstate.ObjectStatus) {
	c.nextExpiry = time.Time{}
	for _, object := range lifecycle.PlanResourceTeardownGC(nil, statuses).ResourceTeardowns {
		if object.APIVersion != api.NetAPIVersion || object.Kind != "DHCPv4Client" {
			continue
		}
		active := mapString(object.Status, "phase") == daemonapi.ResourcePhaseBound && mapString(object.Status, "currentAddress") != ""
		for _, key := range []string{"appliedAddress", "appliedDefaultGateway", "appliedDNSServers", "pendingAddress", "pendingGateway", "pendingDNSServers"} {
			if mapString(object.Status, key) != "" {
				active = true
				break
			}
		}
		if active {
			c.rememberExpiry(mapString(object.Status, "expiresAt"))
		}
	}
}

// NextExpiryAfter integrates the existing framework safety timer with known
// lease deadlines. A past deadline retries cleanup at a bounded rate.
func (c *Controller) NextExpiryAfter() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.nextExpiry.IsZero() {
		return 0
	}
	after := time.Until(c.nextExpiry)
	if after <= 0 {
		return time.Second
	}
	return after
}

func (c *Controller) rememberExpiry(value string) {
	at, err := time.Parse(time.RFC3339Nano, value)
	if err == nil && !at.IsZero() && (c.nextExpiry.IsZero() || at.Before(c.nextExpiry)) {
		c.nextExpiry = at
	}
}

// cleanupRemoved shares the same controller lock and effect path as live
// leases. Generic lifecycle GC must retain these records until withdrawal is
// complete; each failed or interrupted operation keeps its recovery metadata.
func (c *Controller) cleanupRemoved(ctx context.Context, statuses []routerstate.ObjectStatus) error {
	desired := make(map[string]bool, len(c.leases))
	for _, lease := range c.leases {
		desired[lifecycle.OwnerKey(api.NetAPIVersion, "DHCPv4Client", lease.name)] = true
	}
	plan := lifecycle.PlanResourceTeardownGC(desired, statuses)
	var errs []error
	for _, action := range plan.Actions {
		object := action.Status
		if action.Type != lifecycle.GCActionTeardownResource || object.APIVersion != api.NetAPIVersion || object.Kind != "DHCPv4Client" || c.DryRun {
			continue
		}
		next := make(map[string]any, len(object.Status))
		for key, value := range object.Status {
			next[key] = value
		}
		next["phase"], next["reason"] = daemonapi.ResourcePhaseReleased, "ResourceRemoved"
		next["currentAddress"], next["defaultGateway"], next["gateway"], next["dnsServers"] = "", "", "", []string(nil)
		next["prefixLength"], next["leaseState"] = 0, "Released"
		delete(next, "observed")
		next["conditions"] = []daemonapi.Condition{{Type: "LeaseReady", Status: daemonapi.ConditionFalse, Reason: "ResourceRemoved", LastTransitionTime: time.Now().UTC()}}
		delete(next, "error")
		cleanupErr := c.applyLease(ctx, object.Name, object.Status, next)
		if cleanupErr != nil {
			next["phase"], next["reason"], next["error"] = "Error", "WithdrawFailed", cleanupErr.Error()
		}
		if saveErr := c.Store.SaveObjectStatus(object.APIVersion, object.Kind, object.Name, next); saveErr != nil {
			errs = append(errs, errors.Join(cleanupErr, saveErr))
			continue
		}
		if cleanupErr != nil {
			errs = append(errs, cleanupErr)
			continue
		}
		if err := c.Store.DeleteObject(object.APIVersion, object.Kind, object.Name); err != nil {
			errs = append(errs, err)
			continue
		}
		if c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.dhcpv4.client.removed", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: object.APIVersion, Kind: object.Kind, Name: object.Name}
			if err := c.Bus.Publish(ctx, event); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (c *Controller) reconcile(ctx context.Context, name string) error {
	if c.Store == nil {
		return errors.New("DHCPv4 status store is required")
	}
	if err := c.prepare(); err != nil {
		return err
	}
	_, _, declared, err := c.leaseSpecAndIfName(name)
	if err != nil {
		return err
	}
	if !declared {
		statuses, err := c.Store.ListObjectStatuses()
		if err != nil {
			return err
		}
		return c.cleanupRemoved(ctx, statuses)
	}
	eligible, err := c.eligible(name)
	if err != nil {
		return err
	}
	if !eligible {
		return c.withdrawInactive(ctx, name)
	}
	socket := c.socketFor(name)
	status, statusErr := daemonStatus(ctx, socket)
	// Runtime authority may have changed while the HTTP operation waited.
	eligible, err = c.eligible(name)
	if err != nil {
		return err
	}
	if !eligible {
		return c.withdrawInactive(ctx, name)
	}
	if statusErr != nil {
		err := statusErr
		current, readErr := c.Store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4Client", name)
		if readErr != nil {
			return errors.Join(err, readErr)
		}
		c.rememberExpiry(mapString(current, "expiresAt"))
		if expiry, parseErr := time.Parse(time.RFC3339Nano, mapString(current, "expiresAt")); parseErr == nil && !expiry.IsZero() && !time.Now().Before(expiry) {
			// The recorded deadline remains authoritative when the worker is
			// unavailable; a dead socket must not extend an expired lease.
			next := map[string]any{"phase": daemonapi.ResourcePhaseExpired, "health": daemonapi.HealthDegraded,
				"reason": "DaemonUnavailable", "error": err.Error(), "expiresAt": expiry.Format(time.RFC3339Nano)}
			applyErr := c.applyLease(ctx, name, current, next)
			if applyErr != nil {
				next["phase"], next["reason"], next["error"] = "Error", "WithdrawFailed", applyErr.Error()
			}
			saveErr := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", name, next)
			return errors.Join(err, applyErr, saveErr)
		}
		if c.DryRun {
			return c.savePendingDaemonStatus(name, socket, err)
		}
		return err
	}
	for _, resource := range status.Resources {
		if resource.Resource.Kind != "DHCPv4Client" || resource.Resource.Name != name {
			continue
		}
		observed := resource.Observed
		next := map[string]any{
			"phase":          resource.Phase,
			"health":         resource.Health,
			"conditions":     resource.Conditions,
			"observed":       observed,
			"interface":      observed["interface"],
			"currentAddress": observed["currentAddress"],
			"prefixLength":   observed["prefixLength"],
			"defaultGateway": observed["defaultGateway"],
			"device":         observed["interface"],
			"gateway":        observed["defaultGateway"],
			"domain":         observed["domain"],
			"renewAt":        observed["renewAt"],
			"rebindAt":       observed["rebindAt"],
			"expiresAt":      observed["expiresAt"],
			"dryRun":         c.DryRun,
		}
		if leaseTime, err := strconv.ParseInt(observed["leaseTime"], 10, 64); err == nil {
			next["leaseTime"] = leaseTime
		}
		if prefixLength, err := strconv.Atoi(observed["prefixLength"]); err == nil && prefixLength > 0 {
			next["prefixLength"] = prefixLength
		}
		if servers := parseJSONStringList(observed["dnsServers"]); len(servers) > 0 {
			next["dnsServers"] = servers
		}
		if servers := parseJSONStringList(observed["ntpServers"]); len(servers) > 0 {
			next["ntpServers"] = servers
		}
		current, readErr := c.Store.ReadObjectStatus(resource.Resource.APIVersion, resource.Resource.Kind, resource.Resource.Name)
		if readErr != nil {
			return readErr
		}
		if err := c.applyLease(ctx, name, current, next); err != nil {
			next["phase"] = "Error"
			next["reason"] = "ApplyFailed"
			next["error"] = err.Error()
			if saveErr := c.Store.SaveObjectStatus(resource.Resource.APIVersion, resource.Resource.Kind, resource.Resource.Name, next); saveErr != nil {
				return errors.Join(err, saveErr)
			}
			return err
		}
		c.rememberExpiry(mapString(next, "expiresAt"))
		changed := leaseEventChanged(current, next)
		if err := c.Store.SaveObjectStatus(resource.Resource.APIVersion, resource.Resource.Kind, resource.Resource.Name, next); err != nil {
			return err
		}
		if !changed || c.Bus == nil {
			return nil
		}
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, EventApplied, daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client", Name: name}
		event.Attributes = map[string]string{
			"currentAddress": observed["currentAddress"],
			"defaultGateway": observed["defaultGateway"],
			"dryRun":         fmt.Sprintf("%t", c.DryRun),
		}
		return c.Bus.Publish(ctx, event)
	}
	return fmt.Errorf("daemon status did not include DHCPv4Client/%s", name)
}

func (c *Controller) eligible(name string) (bool, error) {
	spec, _, declared, err := c.leaseSpecAndIfName(name)
	if err != nil || !declared {
		return false, err
	}
	if !resourcequery.ResourceWhenPresent(spec.When) {
		return true, nil
	}
	reader, ok := c.Store.(resourcequery.StateStore)
	if !ok {
		return false, errors.New("DHCPv4 when-state reading is unavailable")
	}
	return resourcequery.ResourceWhenMatches(spec.When, reader)
}

func (c *Controller) withdrawInactive(ctx context.Context, name string) error {
	current, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4Client", name)
	if err != nil {
		return err
	}
	next := map[string]any{"phase": daemonapi.ResourcePhasePending, "reason": "WhenFalse", "leaseState": "Inactive", "dryRun": c.DryRun, "expiresAt": mapString(current, "expiresAt"),
		"conditions": []daemonapi.Condition{{Type: "LeaseReady", Status: daemonapi.ConditionFalse, Reason: "WhenFalse", LastTransitionTime: time.Now().UTC()}}}
	cleanupErr := c.applyLease(ctx, name, current, next)
	if cleanupErr != nil {
		next["phase"], next["reason"], next["error"] = "Error", "WithdrawFailed", cleanupErr.Error()
	}
	saveErr := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", name, next)
	return errors.Join(cleanupErr, saveErr)
}

func (c *Controller) savePendingDaemonStatus(name, socket string, cause error) error {
	if c.Store == nil {
		return nil
	}
	status := map[string]any{
		"phase":     daemonapi.ResourcePhasePending,
		"reason":    "DaemonUnavailable",
		"socket":    socket,
		"message":   fmt.Sprintf("waiting for DHCPv4 client socket %s", socket),
		"error":     cause.Error(),
		"dryRun":    true,
		"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
	}
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", name, status)
}

func leaseEventChanged(current, next map[string]any) bool {
	for _, key := range []string{"phase", "currentAddress", "prefixLength", "defaultGateway"} {
		if leaseFieldString(current[key]) != leaseFieldString(next[key]) {
			return true
		}
	}
	return false
}

func leaseFieldString(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func (c *Controller) applyLease(ctx context.Context, name string, current, next map[string]any) error {
	spec, ifname, ok, err := c.leaseSpecAndIfName(name)
	if err != nil {
		return err
	}
	if !ok {
		if mapString(next, "phase") != daemonapi.ResourcePhaseReleased {
			return nil
		}
		// A removed declaration has no live spec. Its own persisted effect
		// record supplies the exact interface and route priority to withdraw.
		for _, key := range []string{"ifname", "pendingAddressInterface", "pendingRouteInterface", "pendingDNSInterface", "appliedDNSInterface"} {
			if ifname = mapString(current, key); ifname != "" {
				break
			}
		}
		if ifname == "" {
			for _, key := range []string{"appliedAddress", "appliedDefaultGateway", "appliedDNSServers", "pendingAddress", "pendingGateway", "pendingDNSServers"} {
				if mapString(current, key) != "" {
					return fmt.Errorf("DHCPv4Client/%s has owned effects without a retained interface", name)
				}
			}
		}
	}
	// Only recorded successful effects are owned. Retain them until each undo
	// succeeds, including when a later effect or the status write fails.
	for _, key := range []string{"appliedAddress", "appliedDefaultGateway", "appliedRouteMetric", "appliedDNSServers", "ifname", "pendingAddress", "pendingAddressInterface", "pendingGateway", "pendingRouteInterface", "pendingRouteMetric", "pendingDNSServers", "pendingDNSInterface", "appliedDNSInterface"} {
		if value, present := current[key]; present {
			next[key] = value
		}
	}
	wantedAddress, gateway := "", ""
	var servers []string
	valid := mapString(next, "phase") == daemonapi.ResourcePhaseBound && mapString(next, "currentAddress") != ""
	if expiry := mapString(next, "expiresAt"); valid && expiry != "" {
		when, err := time.Parse(time.RFC3339Nano, expiry)
		if err != nil {
			return fmt.Errorf("invalid DHCPv4 lease expiry: %w", err)
		}
		if !when.IsZero() && !time.Now().Before(when) {
			valid = false
			next["phase"] = daemonapi.ResourcePhaseExpired
		}
	}
	if valid {
		address, err := netip.ParseAddr(mapString(next, "currentAddress"))
		prefixLength, prefixErr := strconv.Atoi(mapString(next, "prefixLength"))
		if err != nil || !address.Is4() || address.IsUnspecified() || address.IsMulticast() || prefixErr != nil || prefixLength < 0 || prefixLength > 32 {
			return fmt.Errorf("DHCPv4 lease %s is bound without a usable IPv4 address and prefix", name)
		}
		if observed := mapString(next, "interface"); observed != "" && observed != ifname {
			return fmt.Errorf("DHCPv4 lease interface %s differs from configured %s", observed, ifname)
		}
		wantedAddress = netip.PrefixFrom(address, prefixLength).String()
		if api.BoolDefault(spec.UseRoutes, true) {
			gateway = mapString(next, "defaultGateway")
		}
		if gateway != "" {
			addr, err := netip.ParseAddr(gateway)
			if err != nil || !addr.Is4() || addr.IsUnspecified() || addr.IsMulticast() {
				return fmt.Errorf("invalid DHCPv4 gateway %q", gateway)
			}
		}
		if api.BoolDefault(spec.UseDNS, true) {
			servers = stringSlice(next["dnsServers"])
		}
	}
	if c.DryRun {
		next["applyMode"] = "dry-run"
		return nil
	}
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	osName := platform.CurrentOS()
	ownedIfname := mapString(current, "ifname")
	if ownedIfname == "" {
		ownedIfname = ifname
	}
	// A durable intent precedes every new address/route effect. If the process
	// stops after the kernel write but before status publication, the next
	// owner observes only that exact pending resource before adopting it.
	if pending := mapString(current, "pendingAddress"); pending != "" {
		pendingIf := mapString(current, "pendingAddressInterface")
		present, err := dhcpv4LeaseAddressPresent(ctx, osName, command, pendingIf, pending)
		if err != nil {
			return err
		}
		if present {
			next["appliedAddress"], next["ifname"] = pending, pendingIf
			ownedIfname = pendingIf
		}
		delete(next, "pendingAddress")
		delete(next, "pendingAddressInterface")
	}
	if pending := mapString(current, "pendingGateway"); pending != "" {
		pendingIf := mapString(current, "pendingRouteInterface")
		metric, err := strconv.Atoi(mapString(current, "pendingRouteMetric"))
		if err != nil {
			return fmt.Errorf("invalid pending DHCPv4 route metric: %w", err)
		}
		present, err := dhcpv4DefaultRoutePresent(ctx, osName, command, pendingIf, pending, metric)
		if err != nil {
			return err
		}
		if present {
			next["appliedDefaultGateway"], next["appliedRouteMetric"], next["ifname"] = pending, metric, pendingIf
			ownedIfname = pendingIf
		}
		delete(next, "pendingGateway")
		delete(next, "pendingRouteMetric")
		delete(next, "pendingRouteInterface")
	}
	ownedMetric, _ := strconv.Atoi(mapString(next, "appliedRouteMetric"))
	ownedGateway := mapString(next, "appliedDefaultGateway")
	ownedAddress := mapString(next, "appliedAddress")
	var cleanupErrors []error
	if ownedGateway != "" && (ownedIfname != ifname || ownedGateway != gateway || ownedMetric != spec.RouteMetric) {
		present, err := dhcpv4DefaultRoutePresent(ctx, osName, command, ownedIfname, ownedGateway, ownedMetric)
		if err == nil && present {
			err = removeDefaultRoute(ctx, osName, command, ownedIfname, ownedGateway, ownedMetric)
		}
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("withdraw DHCPv4 route: %w", err))
		} else {
			delete(next, "appliedDefaultGateway")
			delete(next, "appliedRouteMetric")
		}
	}
	if ownedAddress != "" && (ownedIfname != ifname || ownedAddress != wantedAddress) {
		present, err := dhcpv4LeaseAddressPresent(ctx, osName, command, ownedIfname, ownedAddress)
		if err == nil && present {
			err = removeIPv4Address(ctx, osName, command, ownedIfname, ownedAddress)
		}
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("withdraw DHCPv4 address: %w", err))
		} else {
			delete(next, "appliedAddress")
		}
	}
	ownedDNS, dnsIfname := mapString(current, "appliedDNSServers"), mapString(current, "appliedDNSInterface")
	if dnsIfname == "" {
		dnsIfname = ownedIfname
	}
	if pending := mapString(current, "pendingDNSServers"); pending != "" {
		pendingIf := mapString(current, "pendingDNSInterface")
		owned, exact, err := observeDNS(ctx, osName, command, pendingIf, defaultString(c.ResolvConfPath, "/etc/resolv.conf"), name, pending)
		if err != nil {
			cleanupErrors = append(cleanupErrors, err)
		} else if owned {
			ownedDNS, dnsIfname = pending, pendingIf
			if exact {
				next["appliedDNSServers"], next["appliedDNSInterface"] = pending, pendingIf
			}
		} else {
			delete(next, "pendingDNSServers")
			delete(next, "pendingDNSInterface")
		}
	}
	if ownedDNS != "" && (len(servers) == 0 || dnsIfname != ifname) {
		if err := withdrawDNS(ctx, osName, command, dnsIfname, defaultString(c.ResolvConfPath, "/etc/resolv.conf"), name, ownedDNS); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		} else {
			delete(next, "appliedDNSServers")
			delete(next, "appliedDNSInterface")
			delete(next, "pendingDNSServers")
			delete(next, "pendingDNSInterface")
		}
	}
	if len(cleanupErrors) > 0 {
		return errors.Join(cleanupErrors...)
	}
	if wantedAddress != "" {
		present, err := dhcpv4LeaseAddressPresent(ctx, osName, command, ifname, wantedAddress)
		if err != nil {
			return err
		}
		if !present {
			next["pendingAddress"], next["pendingAddressInterface"] = wantedAddress, ifname
			if err := c.savePendingEffect(name, next); err != nil {
				return err
			}
			if err := addIPv4Address(ctx, osName, command, ifname, wantedAddress); err != nil {
				return err
			}
			next["appliedAddress"] = wantedAddress
			delete(next, "pendingAddress")
			delete(next, "pendingAddressInterface")
		}
		next["addressPresent"], next["ifname"] = true, ifname
	}
	if gateway != "" {
		present, err := dhcpv4DefaultRoutePresent(ctx, osName, command, ifname, gateway, spec.RouteMetric)
		if err != nil {
			return err
		}
		if !present {
			next["pendingGateway"], next["pendingRouteInterface"], next["pendingRouteMetric"] = gateway, ifname, spec.RouteMetric
			if err := c.savePendingEffect(name, next); err != nil {
				return err
			}
			if err := addDefaultRoute(ctx, osName, command, ifname, gateway, spec.RouteMetric); err != nil {
				return err
			}
			next["appliedDefaultGateway"], next["appliedRouteMetric"] = gateway, spec.RouteMetric
			delete(next, "pendingGateway")
			delete(next, "pendingRouteInterface")
			delete(next, "pendingRouteMetric")
		}
		next["defaultRoutePresent"], next["ifname"] = true, ifname
	}
	if len(servers) > 0 {
		next["pendingDNSServers"], next["pendingDNSInterface"] = strings.Join(servers, ","), ifname
		if err := c.savePendingEffect(name, next); err != nil {
			return err
		}
		applied, err := applyDNS(ctx, osName, command, ifname, defaultString(c.ResolvConfPath, "/etc/resolv.conf"), name, servers, false)
		if applied {
			next["appliedDNSServers"], next["appliedDNSInterface"], next["ifname"] = strings.Join(servers, ","), ifname, ifname
			delete(next, "pendingDNSServers")
			delete(next, "pendingDNSInterface")
		}
		if err != nil {
			return err
		}
	}
	next["applyMode"] = "active"
	return nil
}

func (c *Controller) savePendingEffect(name string, next map[string]any) error {
	if c.Store == nil {
		return errors.New("DHCPv4 status store is required before applying a lease")
	}
	pending := make(map[string]any, len(next))
	for key, value := range next {
		pending[key] = value
	}
	pending["phase"] = "Applying"
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", name, pending)
}

func mapString(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func stringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		var out []string
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return parseJSONStringList(typed)
	default:
		return nil
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func applyDNS(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, resolvConfPath, resource string, servers []string, dryRun bool) (bool, error) {
	if osName == platform.OSLinux && systemdResolvedResolvConf(resolvConfPath) {
		if dryRun {
			return false, nil
		}
		args := append([]string{"dns", ifname}, servers...)
		if _, err := command(ctx, "resolvectl", args...); err != nil {
			return false, fmt.Errorf("set systemd-resolved DNS for %s: %w", ifname, err)
		}
		if _, err := command(ctx, "resolvectl", "domain", ifname, "~."); err != nil {
			return true, fmt.Errorf("set systemd-resolved default DNS domain for %s: %w", ifname, err)
		}
		return true, nil
	}
	err := writeResolvConf(resolvConfPath, resource, servers, dryRun)
	return err == nil && !dryRun, err
}

// observeDNS confirms a pending effect at the real resolver boundary. A managed
// file marker also identifies a partial file write; systemd-resolved has no such
// marker, so its current per-link server list must match the recorded intent.
func observeDNS(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, path, resource, servers string) (bool, bool, error) {
	if osName == platform.OSLinux && systemdResolvedResolvConf(path) {
		values, err := resolvedValues(ctx, command, "dns", ifname)
		if err != nil {
			return false, false, err
		}
		matches := strings.Join(values, ",") == servers
		return matches, matches, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	owned := strings.Contains(string(data), "# Source: DHCPv4Client/"+resource+"\n")
	var actual []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "nameserver" {
			actual = append(actual, fields[1])
		}
	}
	return owned, owned && strings.Join(actual, ",") == servers, nil
}

func resolvedValues(ctx context.Context, command outputCommandFunc, setting, ifname string) ([]string, error) {
	out, err := command(ctx, "resolvectl", setting, ifname)
	if err != nil {
		return nil, err
	}
	_, values, found := strings.Cut(string(out), ":")
	if !found {
		return nil, fmt.Errorf("unrecognized resolvectl %s response for %s", setting, ifname)
	}
	return strings.Fields(values), nil
}

func withdrawDNS(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, path, resource, servers string) error {
	if osName == platform.OSLinux && systemdResolvedResolvConf(path) {
		current, err := resolvedValues(ctx, command, "dns", ifname)
		if err != nil {
			return err
		}
		if len(current) > 0 && strings.Join(current, ",") != servers {
			return nil
		}
		domains, err := resolvedValues(ctx, command, "domain", ifname)
		if err != nil {
			return err
		}
		var remaining []string
		for _, domain := range domains {
			if domain != "~." {
				remaining = append(remaining, domain)
			}
		}
		var errs []error
		if len(remaining) != len(domains) {
			if len(remaining) == 0 {
				remaining = []string{""}
			}
			_, err := command(ctx, "resolvectl", append([]string{"domain", ifname}, remaining...)...)
			if err != nil {
				errs = append(errs, err)
			}
		}
		if len(current) > 0 {
			if _, err := command(ctx, "resolvectl", "dns", ifname, ""); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), "# Source: DHCPv4Client/"+resource+"\n") {
		return nil
	}
	return writeResolvConf(path, resource, nil, false)
}

func systemdResolvedResolvConf(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	target, err := os.Readlink(path)
	if err != nil {
		return false
	}
	return strings.Contains(target, "systemd/resolve") || strings.Contains(target, "stub-resolv.conf")
}

func writeResolvConf(path, resource string, servers []string, dryRun bool) error {
	var b strings.Builder
	b.WriteString("# Managed by routerd. Do not edit by hand.\n")
	b.WriteString("# Source: DHCPv4Client/")
	b.WriteString(resource)
	b.WriteByte('\n')
	for _, server := range servers {
		server = strings.TrimSpace(server)
		if server == "" {
			continue
		}
		b.WriteString("nameserver ")
		b.WriteString(server)
		b.WriteByte('\n')
	}
	data := []byte(b.String())
	if dryRun {
		return nil
	}
	if current, err := os.ReadFile(path); err == nil && string(current) == string(data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (c *Controller) prepare() error {
	if c.prepared {
		return c.prepareErr
	}
	c.prepared = true
	c.prepareErr = c.prepareConfig()
	return c.prepareErr
}

func (c *Controller) prepareConfig() error {
	if c.Router == nil {
		return nil
	}
	aliases := make(map[string]string)
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "Interface" {
			continue
		}
		spec, err := resource.InterfaceSpec()
		if err != nil {
			return err
		}
		aliases[resource.Metadata.Name] = spec.IfName
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DHCPv4Client" {
			continue
		}
		spec, err := resource.DHCPv4ClientSpec()
		if err != nil {
			return err
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			ifname = spec.Interface
		}
		if ifname == "" {
			return fmt.Errorf("DHCPv4Client/%s needs spec.interface", resource.Metadata.Name)
		}
		c.leases = append(c.leases, preparedLease{name: resource.Metadata.Name, ifname: ifname, spec: spec})
	}
	return nil
}

func (c *Controller) leaseSpecAndIfName(name string) (api.DHCPv4ClientSpec, string, bool, error) {
	if err := c.prepare(); err != nil {
		return api.DHCPv4ClientSpec{}, "", false, err
	}
	for _, lease := range c.leases {
		if lease.name == name {
			return lease.spec, lease.ifname, true, nil
		}
	}
	return api.DHCPv4ClientSpec{}, "", false, nil
}

func addIPv4Address(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, address string) error {
	if osName == platform.OSFreeBSD {
		_, err := command(ctx, "ifconfig", ifname, "inet", address, "alias")
		return err
	}
	_, err := command(ctx, "ip", "-4", "addr", "add", address, "dev", ifname)
	return err
}

func dhcpv4LeaseAddressPresent(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, address string) (bool, error) {
	if strings.TrimSpace(address) == "" {
		return false, nil
	}
	if osName == platform.OSFreeBSD {
		out, err := command(ctx, "ifconfig", ifname)
		if err != nil {
			return false, err
		}
		addr := strings.TrimSpace(strings.Split(address, "/")[0])
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			for i, field := range fields {
				if field == "inet" && i+1 < len(fields) && fields[i+1] == addr {
					return true, nil
				}
			}
		}
		return false, nil
	}
	out, err := command(ctx, "ip", "-4", "addr", "show", "dev", ifname)
	if err != nil {
		return false, err
	}
	return strings.Contains(string(out), "inet "+address+" "), nil
}

func removeIPv4Address(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, address string) error {
	if osName == platform.OSFreeBSD {
		host := address
		if value, _, ok := strings.Cut(host, "/"); ok {
			host = value
		}
		_, err := command(ctx, "ifconfig", ifname, "inet", host, "-alias")
		return err
	}
	_, err := command(ctx, "ip", "-4", "addr", "del", address, "dev", ifname)
	return err
}

func dhcpv4DefaultRoutePresent(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, gateway string, metric int) (bool, error) {
	if osName == platform.OSFreeBSD {
		out, err := command(ctx, "netstat", "-rn", "-f", "inet")
		if err != nil {
			return false, err
		}
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 4 && fields[0] == "default" && fields[1] == gateway && fields[len(fields)-1] == ifname {
				return true, nil
			}
		}
		return false, nil
	}
	out, err := command(ctx, "ip", "-4", "route", "show", "default")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if dhcpv4DefaultRouteLineMatches(line, ifname, gateway, metric) {
			return true, nil
		}
	}
	return false, nil
}

func dhcpv4DefaultRouteLineMatches(line, ifname, gateway string, metric int) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "default" {
		return false
	}
	if !dhcpv4RouteFieldsContainPair(fields, "dev", ifname) {
		return false
	}
	if gateway != "" && !dhcpv4RouteFieldsContainPair(fields, "via", gateway) {
		return false
	}
	observedMetric := 0
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "metric" {
			var err error
			observedMetric, err = strconv.Atoi(fields[i+1])
			if err != nil {
				return false
			}
		}
	}
	if observedMetric != metric {
		return false
	}
	return true
}

func dhcpv4RouteFieldsContainPair(fields []string, key, value string) bool {
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == key && fields[i+1] == value {
			return true
		}
	}
	return false
}

func addDefaultRoute(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, gateway string, metric int) error {
	if osName == platform.OSFreeBSD {
		_, err := command(ctx, "route", "-n", "add", "-inet", "default", gateway, "-ifp", ifname)
		return err
	}
	args := []string{"-4", "route", "add", "default", "via", gateway, "dev", ifname, "metric", strconv.Itoa(metric)}
	_, err := command(ctx, "ip", args...)
	return err
}

func removeDefaultRoute(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, gateway string, metric int) error {
	if osName == platform.OSFreeBSD {
		_, err := command(ctx, "route", "-n", "delete", "-inet", "default", gateway, "-ifp", ifname)
		return err
	}
	_, err := command(ctx, "ip", "-4", "route", "del", "default", "via", gateway, "dev", ifname, "metric", strconv.Itoa(metric))
	return err
}

func runOutputCommandContext(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (c *Controller) socketFor(resource string) string {
	if socket := c.DaemonSockets[resource]; socket != "" {
		return socket
	}
	defaults, _ := platform.Current()
	return filepath.Join(defaults.RuntimeDir, "dhcpv4-client", resource+".sock")
}

func daemonStatus(ctx context.Context, socketPath string) (daemonapi.DaemonStatus, error) {
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
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
	if resp.StatusCode != http.StatusOK {
		return daemonapi.DaemonStatus{}, fmt.Errorf("DHCPv4 daemon status: HTTP %d", resp.StatusCode)
	}
	var status daemonapi.DaemonStatus
	return status, json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&status)
}

func parseJSONStringList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out
	}
	return strings.Split(raw, ",")
}
