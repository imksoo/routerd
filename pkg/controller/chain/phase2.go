package chain

import (
	"context"
	"encoding/binary"
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
	"strconv"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/resourcequery"
)

type DHCPv6InformationController struct {
	Router        *api.Router
	Bus           *bus.Bus
	Store         Store
	DaemonSockets map[string]string
	Logger        *slog.Logger
}

func (c DHCPv6InformationController) Start(ctx context.Context) {
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.dhcpv6.client.prefix.*", "routerd.dhcpv6.client.info.*"}}, 32)
	go func() {
		for event := range ch {
			if event.Resource == nil {
				continue
			}
			request := !strings.HasPrefix(event.Type, "routerd.dhcpv6.client.info.")
			if err := c.reconcile(ctx, event.Resource.Name, request); err != nil && c.Logger != nil {
				c.Logger.Warn("dhcpv6 information reconcile failed", "pd", event.Resource.Name, "error", err)
			}
		}
	}()
}

func (c DHCPv6InformationController) reconcile(ctx context.Context, pdName string, request bool) error {
	pdStatus := c.Store.ObjectStatus(api.NetAPIVersion, "DHCPv6PrefixDelegation", pdName)
	if pdStatus["phase"] != daemonapi.ResourcePhaseBound {
		return nil
	}
	socket := c.socketFor(pdName)
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DHCPv6Information" {
			continue
		}
		spec, err := resource.DHCPv6InformationSpec()
		if err != nil {
			return err
		}
		if !c.matchesPD(resource, spec, pdName) {
			continue
		}
		if request {
			_, _ = postDaemonCommand(ctx, socket, daemonapi.CommandInfoRequest)
		}
		status, err := daemonStatus(ctx, socket)
		if err != nil {
			return err
		}
		observed := daemonObserved(status, "DHCPv6PrefixDelegation", pdName)
		next := map[string]any{
			"phase":        "Ready",
			"aftrName":     observed["aftrName"],
			"dnsServers":   decodeStringList(observed["dnsServers"]),
			"sntpServers":  decodeStringList(observed["sntpServers"]),
			"domainSearch": decodeStringList(observed["domainSearch"]),
			"source":       pdName,
		}
		changed := statusChanged(c.Store.ObjectStatus(api.NetAPIVersion, "DHCPv6Information", resource.Metadata.Name), next)
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Information", resource.Metadata.Name, next); err != nil {
			return err
		}
		if changed && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.dhcpv6.info.updated", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Information", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"aftrName": observed["aftrName"], "source": pdName}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c DHCPv6InformationController) matchesPD(resource api.Resource, spec api.DHCPv6InformationSpec, pdName string) bool {
	for _, owner := range resource.Metadata.OwnerRefs {
		if owner.Kind == "DHCPv6PrefixDelegation" && owner.Name == pdName {
			return true
		}
	}
	for _, candidate := range c.Router.Spec.Resources {
		if candidate.Kind != "DHCPv6PrefixDelegation" || candidate.Metadata.Name != pdName {
			continue
		}
		pdSpec, err := candidate.DHCPv6PrefixDelegationSpec()
		return err == nil && pdSpec.Interface == spec.Interface
	}
	return false
}

func (c DHCPv6InformationController) socketFor(resource string) string {
	if socket := c.DaemonSockets[resource]; socket != "" {
		return socket
	}
	return filepath.Join("/run/routerd/dhcpv6-client", resource+".sock")
}

type DSLiteTunnelController struct {
	Router       *api.Router
	Bus          *bus.Bus
	Store        Store
	DryRun       bool
	ResolverPort int
	Logger       *slog.Logger
}

func (c DSLiteTunnelController) Start(ctx context.Context) {
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.dhcpv6.info.*", "routerd.dhcpv6.client.prefix.*", "routerd.dns.resolver.*"}}, 32)
	go func() {
		for range ch {
			if err := c.reconcile(ctx); err != nil && c.Logger != nil {
				c.Logger.Warn("dslite tunnel reconcile failed", "error", err)
			}
		}
	}()
}

func (c DSLiteTunnelController) reconcile(ctx context.Context) error {
	var failures []string
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DSLiteTunnel" {
			continue
		}
		spec, err := resource.DSLiteTunnelSpec()
		if err != nil {
			return err
		}
		if !resourcequery.DependenciesReady(c.Store, spec.DependsOn) {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DependsOnFalse"})
			continue
		}
		aftrName, remote, err := c.resolveRemote(ctx, spec)
		if err != nil && aftrName == "" {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "AFTRMissing"})
			continue
		}
		if err != nil {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "AFTRResolveFailed", "aftrName": aftrName, "error": err.Error()})
			continue
		}
		local, localIfName, err := c.localAddress(spec)
		if err != nil || local == "" {
			reason := "LocalIPv6Missing"
			if err != nil {
				reason = err.Error()
			}
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": reason, "aftrName": aftrName, "aftrIPv6": remote})
			continue
		}
		ifname := firstNonEmpty(spec.TunnelName, spec.Interface, resource.Metadata.Name)
		actualIfName := ifname
		mtu := spec.MTU
		if mtu == 0 {
			mtu = 1460
		}
		status := map[string]any{"phase": "Up", "interface": ifname, "tunnelName": ifname, "device": ifname, "localIPv6": local, "localInterface": localIfName, "aftrName": aftrName, "aftrIPv6": remote, "mtu": mtu, "dryRun": c.DryRun}
		changed := statusChanged(c.Store.ObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name), status)
		if !c.DryRun && changed {
			if localIfName != "" {
				if err := ensureIPv6LocalEndpoint(ctx, localIfName, local); err != nil {
					failures = append(failures, fmt.Sprintf("%s: %v", resource.Metadata.Name, err))
					_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Error", "reason": "LocalEndpointApplyFailed", "interface": ifname, "localIPv6": local, "aftrIPv6": remote, "error": err.Error(), "dryRun": c.DryRun})
					continue
				}
			}
			resolvedIfName, err := ensureDSLiteTunnel(ctx, c.Router, spec, ifname, remote, local)
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", resource.Metadata.Name, err))
				_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Error", "reason": "TunnelApplyFailed", "interface": ifname, "localIPv6": local, "aftrIPv6": remote, "error": err.Error(), "dryRun": c.DryRun})
				continue
			}
			actualIfName = firstNonEmpty(resolvedIfName, ifname)
			if actualIfName != ifname {
				status["interface"] = actualIfName
				status["device"] = actualIfName
				status["aliasOf"] = actualIfName
				status["desiredTunnelName"] = ifname
			}
			if err := exec.CommandContext(ctx, "ip", "link", "set", actualIfName, "mtu", fmt.Sprintf("%d", mtu), "up").Run(); err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", resource.Metadata.Name, err))
				_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Error", "reason": "LinkApplyFailed", "interface": ifname, "localIPv6": local, "aftrIPv6": remote, "error": err.Error(), "dryRun": c.DryRun})
				continue
			}
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, status); err != nil {
			return err
		}
		if changed && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.tunnel.ds-lite.up", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"interface": ifname, "aftrIPv6": remote, "dryRun": fmt.Sprintf("%t", c.DryRun)}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

func (c DSLiteTunnelController) resolveRemote(ctx context.Context, spec api.DSLiteTunnelSpec) (string, string, error) {
	var firstName string
	candidates := []string{
		resourcequery.Value(c.Store, spec.AFTRFrom),
		spec.AFTRFQDN,
		spec.AFTRIPv6,
		spec.RemoteAddress,
	}
	var lastErr error
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if firstName == "" {
			firstName = candidate
		}
		remote, err := resolveAFTRIPv6(ctx, candidate, c.ResolverPort)
		if err == nil {
			return candidate, remote, nil
		}
		lastErr = err
	}
	if firstName == "" {
		return "", "", fmt.Errorf("missing AFTR source")
	}
	return firstName, "", lastErr
}

type IPv4RouteController struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	DryRun bool
	Logger *slog.Logger
}

func (c IPv4RouteController) Start(ctx context.Context) {
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.tunnel.ds-lite.*", "routerd.lan.route.changed"}}, 32)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					return
				}
				if err := c.reconcile(ctx); err != nil && c.Logger != nil {
					c.Logger.Warn("ipv4 route reconcile failed", "error", err)
				}
			case <-ticker.C:
				if err := c.reconcile(ctx); err != nil && c.Logger != nil {
					c.Logger.Warn("ipv4 route periodic reconcile failed", "error", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c IPv4RouteController) reconcile(ctx context.Context) error {
	var failures []string
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "IPv4Route" {
			continue
		}
		spec, err := resource.IPv4RouteSpec()
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", resource.Metadata.Name, err))
			continue
		}
		if !resourcequery.DependenciesReady(c.Store, spec.DependsOn) {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DependsOnFalse"})
			continue
		}
		device := firstNonEmpty(resourcequery.Value(c.Store, spec.DeviceFrom), strings.TrimSpace(spec.Device))
		if device == "" {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DeviceMissing"})
			continue
		}
		destination := firstNonEmpty(spec.Destination, "0.0.0.0/0")
		gateway := firstNonEmpty(resourcequery.Value(c.Store, spec.GatewayFrom), strings.TrimSpace(spec.Gateway))
		status := map[string]any{"phase": "Installed", "destination": destination, "device": device, "gateway": gateway, "metric": spec.Metric, "dryRun": c.DryRun, "installedAt": time.Now().UTC().Format(time.RFC3339Nano)}
		changed := statusChanged(c.Store.ObjectStatus(api.NetAPIVersion, "IPv4Route", resource.Metadata.Name), status)
		if !c.DryRun {
			args := []string{"route", "replace", destination, "dev", device}
			if gateway != "" {
				args = []string{"route", "replace", destination, "via", gateway, "dev", device}
			}
			if spec.Metric > 0 {
				args = append(args, "metric", fmt.Sprintf("%d", spec.Metric))
			}
			cmd := exec.CommandContext(ctx, "ip", args...)
			out, err := cmd.CombinedOutput()
			if err != nil {
				message := strings.TrimSpace(string(out))
				status := map[string]any{"phase": "Error", "reason": "ApplyFailed", "destination": destination, "device": device, "gateway": gateway, "metric": spec.Metric, "dryRun": c.DryRun, "error": err.Error(), "command": "ip " + strings.Join(args, " ")}
				if message != "" {
					status["message"] = message
				}
				_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", resource.Metadata.Name, status)
				failures = append(failures, fmt.Sprintf("%s: %s: %v", resource.Metadata.Name, status["command"], err))
				continue
			}
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", resource.Metadata.Name, status); err != nil {
			return err
		}
		if changed && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.ipv4.route.installed", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv4Route", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"destination": destination, "device": device, "gateway": gateway, "dryRun": fmt.Sprintf("%t", c.DryRun)}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

type IPv6RouterAdvertisementController struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	DryRun bool
	Logger *slog.Logger
}

func (c IPv6RouterAdvertisementController) Start(ctx context.Context) {
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.lan.address.*", "routerd.dhcpv6.info.*"}}, 32)
	go func() {
		for range ch {
			if err := c.reconcile(ctx); err != nil && c.Logger != nil {
				c.Logger.Warn("ipv6 router advertisement reconcile failed", "error", err)
			}
		}
	}()
}

func (c IPv6RouterAdvertisementController) reconcile(ctx context.Context) error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "IPv6RouterAdvertisement" {
			continue
		}
		spec, err := resource.IPv6RouterAdvertisementSpec()
		if err != nil {
			return err
		}
		if !resourcequery.DependenciesReady(c.Store, spec.DependsOn) {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DependsOnFalse"})
			continue
		}
		prefix := firstNonEmpty(prefixFromStatusOrAddress(c.Store, resourcequery.Value(c.Store, spec.PrefixFrom)), prefixFromStatusOrAddress(c.Store, spec.Prefix), prefixFromStatusOrAddress(c.Store, spec.PrefixSource))
		if prefix == "" {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "PrefixMissing"})
			continue
		}
		rdnss := append(expandServers(c.Store, spec.RDNSS), expandServerSources(c.Store, spec.RDNSSFrom)...)
		configPath := firstNonEmpty(spec.ConfigPath, "/run/routerd/radvd-phase2.conf")
		configChanged, err := writeRadvdConfig(configPath, spec.Interface, prefix, rdnss, spec.PreferredLifetime, spec.ValidLifetime)
		if err != nil {
			return err
		}
		if !c.DryRun && configChanged {
			if err := exec.CommandContext(ctx, "radvd", "-C", configPath, "-p", firstNonEmpty(spec.PIDFile, "/run/routerd/radvd-phase2.pid")).Start(); err != nil {
				return err
			}
		}
		status := map[string]any{"phase": "Applied", "interface": spec.Interface, "prefix": prefix, "rdnss": rdnss, "configPath": configPath, "dryRun": c.DryRun}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", resource.Metadata.Name, status); err != nil {
			return err
		}
		if configChanged && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.ra.applied", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"interface": spec.Interface, "prefix": prefix, "dryRun": fmt.Sprintf("%t", c.DryRun)}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

type DHCPv6ServerController struct {
	Router          *api.Router
	Bus             *bus.Bus
	Store           Store
	DryRun          bool
	Command         string
	ConfigPath      string
	PIDFile         string
	Port            int
	ListenAddresses []string
	Logger          *slog.Logger
}

func (c DHCPv6ServerController) Start(ctx context.Context) {
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.lan.address.*", "routerd.dhcpv6.info.*"}}, 32)
	go func() {
		for range ch {
			if err := c.reconcile(ctx); err != nil && c.Logger != nil {
				c.Logger.Warn("ipv6 dhcpv6 server reconcile failed", "error", err)
			}
		}
	}()
}

func (c DHCPv6ServerController) reconcile(ctx context.Context) error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DHCPv6Server" {
			continue
		}
		spec, err := resource.DHCPv6ServerSpec()
		if err != nil {
			return err
		}
		if !resourcequery.DependenciesReady(c.Store, spec.DependsOn) {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Server", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DependsOnFalse"})
			continue
		}
		dnsServers := append(expandServers(c.Store, spec.DNSServers), expandServerSources(c.Store, spec.DNSServerFrom)...)
		configPath := firstNonEmpty(c.ConfigPath, "/run/routerd/dnsmasq-phase1.conf")
		pidFile := firstNonEmpty(c.PIDFile, "/run/routerd/dnsmasq-phase1.pid")
		port := c.Port
		if port == 0 {
			port = 1053
		}
		changed, err := writeDnsmasqConfig(c.Router, c.Store, configPath, pidFile, port, c.ListenAddresses)
		if err != nil {
			return err
		}
		if c.DryRun {
			command := firstNonEmpty(c.Command, "dnsmasq")
			if err := testDnsmasqConfig(ctx, command, configPath); err != nil {
				return err
			}
		} else {
			if err := ensureDnsmasq(ctx, c.Command, configPath, pidFile, changed); err != nil {
				return err
			}
		}
		phase := "Applied"
		if c.DryRun {
			phase = "Rendered"
		}
		status := map[string]any{"phase": phase, "interface": spec.Interface, "mode": firstNonEmpty(spec.Mode, "stateless"), "dnsServers": dnsServers, "configPath": configPath, "pidFile": pidFile, "dryRun": c.DryRun}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Server", resource.Metadata.Name, status); err != nil {
			return err
		}
		if changed && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.lan.service.dhcpv6.applied", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"interface": spec.Interface, "dryRun": fmt.Sprintf("%t", c.DryRun)}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func suffixedPath(path, oldSuffix, newSuffix string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return strings.TrimSuffix(path, oldSuffix) + newSuffix
}

func postDaemonCommand(ctx context.Context, socketPath, command string) (daemonapi.CommandResult, error) {
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socketPath)
	}}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/commands/"+command, nil)
	if err != nil {
		return daemonapi.CommandResult{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return daemonapi.CommandResult{}, err
	}
	defer resp.Body.Close()
	var result daemonapi.CommandResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil && err != io.EOF {
		return result, err
	}
	if resp.StatusCode >= 400 {
		return result, fmt.Errorf("daemon command %s rejected: %s", command, result.Message)
	}
	return result, nil
}

func daemonObserved(status daemonapi.DaemonStatus, kind, name string) map[string]string {
	for _, resource := range status.Resources {
		if resource.Resource.Kind == kind && resource.Resource.Name == name {
			return resource.Observed
		}
	}
	return nil
}

func decodeStringList(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func valueFromStatusRef(store Store, ref string) string {
	ref = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(ref, "${"), "}"))
	if ref == "" || !strings.Contains(ref, ".status.") {
		return ref
	}
	parts := strings.SplitN(ref, ".status.", 2)
	left, field := parts[0], parts[1]
	segments := strings.Split(left, "/")
	if len(segments) != 2 {
		return ""
	}
	status := store.ObjectStatus(api.NetAPIVersion, segments[0], segments[1])
	value := status[field]
	switch typed := value.(type) {
	case string:
		return typed
	case []string:
		data, _ := json.Marshal(typed)
		return string(data)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		data, _ := json.Marshal(out)
		return string(data)
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
}

func valueFromStatusRefOrLiteral(store Store, value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "${") && strings.Contains(trimmed, ".status.") {
		return valueFromStatusRef(store, trimmed)
	}
	return trimmed
}

func (c DSLiteTunnelController) localAddress(spec api.DSLiteTunnelSpec) (string, string, error) {
	switch spec.LocalAddressSource {
	case "delegatedAddress":
		return c.localDelegatedAddress(spec)
	case "static":
		local := localIPv6Address(firstNonEmpty(valueFromStatusRef(c.Store, spec.LocalAddress), spec.LocalAddress))
		if local == "" {
			return "", "", fmt.Errorf("invalid static local IPv6 address")
		}
		return local, "", nil
	case "", "interface":
		raw := firstNonEmpty(valueFromStatusRef(c.Store, spec.LocalIPv6Source), valueFromStatusRef(c.Store, spec.LocalAddress), spec.LocalAddress)
		if spec.LocalAddressSuffix != "" {
			if raw == "" {
				ifname := interfaceName(c.Router, spec.Interface)
				if ifname == "" {
					return "", "", fmt.Errorf("missing Interface %q", spec.Interface)
				}
				discovered, _, err := interfaceGlobalIPv6(ifname)
				if err != nil {
					return "", "", err
				}
				raw = discovered
			}
			local, err := deriveIPv6AddressFromPrefix(raw, "", spec.LocalAddressSuffix)
			if err != nil {
				return "", "", err
			}
			return local, "", nil
		}
		if raw != "" {
			return localIPv6Address(raw), "", nil
		}
		ifname := interfaceName(c.Router, spec.Interface)
		if ifname == "" {
			return "", "", fmt.Errorf("missing Interface %q", spec.Interface)
		}
		return interfaceGlobalIPv6(ifname)
	default:
		return "", "", fmt.Errorf("unsupported localAddressSource %q", spec.LocalAddressSource)
	}
}

type ipJSONAddress struct {
	Family            string `json:"family"`
	Local             string `json:"local"`
	Scope             string `json:"scope"`
	Dynamic           bool   `json:"dynamic"`
	Temporary         bool   `json:"temporary"`
	Deprecated        bool   `json:"deprecated"`
	PreferredLifeTime int    `json:"preferred_life_time"`
}

type ipJSONLink struct {
	AddrInfo []ipJSONAddress `json:"addr_info"`
}

func interfaceGlobalIPv6(ifname string) (string, string, error) {
	out, err := exec.Command("ip", "-j", "-6", "addr", "show", "dev", ifname, "scope", "global").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("ip -j -6 addr show dev %s scope global: %w: %s", ifname, err, strings.TrimSpace(string(out)))
	}
	addr := firstUsableGlobalIPv6(out)
	if addr == "" {
		return "", "", fmt.Errorf("no usable global IPv6 address on %s", ifname)
	}
	return addr, "", nil
}

func firstUsableGlobalIPv6(data []byte) string {
	var links []ipJSONLink
	if err := json.Unmarshal(data, &links); err != nil {
		return ""
	}
	var fallback string
	for _, link := range links {
		for _, info := range link.AddrInfo {
			if info.Family != "inet6" || info.Local == "" || info.Scope != "global" || info.Deprecated || info.Temporary || info.PreferredLifeTime == 0 {
				continue
			}
			if fallback == "" {
				fallback = info.Local
			}
			if info.Dynamic {
				return info.Local
			}
		}
	}
	return fallback
}

func (c DSLiteTunnelController) localDelegatedAddress(spec api.DSLiteTunnelSpec) (string, string, error) {
	if spec.LocalDelegatedAddress == "" {
		return "", "", fmt.Errorf("localDelegatedAddress is required")
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "IPv6DelegatedAddress" || resource.Metadata.Name != spec.LocalDelegatedAddress {
			continue
		}
		delegated, err := resource.IPv6DelegatedAddressSpec()
		if err != nil {
			return "", "", err
		}
		prefix := prefixFromStatusOrAddress(c.Store, "${IPv6DelegatedAddress/"+spec.LocalDelegatedAddress+".status.address}")
		if prefix == "" {
			return "", "", fmt.Errorf("delegated prefix is not ready")
		}
		subnetID := delegated.SubnetID
		if parsed, err := netip.ParsePrefix(prefix); err == nil && parsed.Bits() == 64 {
			subnetID = ""
		}
		local, err := deriveIPv6AddressFromPrefix(prefix, subnetID, firstNonEmpty(spec.LocalAddressSuffix, delegated.AddressSuffix, "::1"))
		if err != nil {
			return "", "", err
		}
		return local, interfaceName(c.Router, delegated.Interface), nil
	}
	return "", "", fmt.Errorf("missing IPv6DelegatedAddress %q", spec.LocalDelegatedAddress)
}

func resolveAFTRIPv6(ctx context.Context, value string, resolverPort int) (string, error) {
	if addr, err := netip.ParseAddr(value); err == nil {
		if addr.Is6() {
			return addr.String(), nil
		}
		return "", fmt.Errorf("%s is not an IPv6 address", value)
	}
	if resolverPort == 0 {
		resolverPort = 1053
	}
	resolver := net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", strconv.Itoa(resolverPort)))
		},
	}
	addrs, err := resolver.LookupIPAddr(ctx, strings.TrimSuffix(value, "."))
	if err != nil {
		return "", err
	}
	for _, addr := range addrs {
		parsed, ok := netip.AddrFromSlice(addr.IP)
		if ok && parsed.Is6() {
			return parsed.String(), nil
		}
	}
	return "", fmt.Errorf("no IPv6 address for %s", value)
}

func localIPv6Address(value string) string {
	if value == "" {
		return ""
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Masked().Addr().String()
	}
	if addr, err := netip.ParseAddr(value); err == nil && addr.Is6() {
		return addr.String()
	}
	return ""
}

func prefixFromStatusOrAddress(store Store, ref string) string {
	value := valueFromStatusRef(store, ref)
	if value == "" {
		return ""
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		if prefix.Bits() == 128 {
			return netip.PrefixFrom(prefix.Addr(), 64).Masked().String()
		}
		return prefix.Masked().String()
	}
	if addr, err := netip.ParseAddr(value); err == nil && addr.Is6() {
		return netip.PrefixFrom(addr, 64).Masked().String()
	}
	return ""
}

func readyWhenAll(store Store, predicates []api.ReadyWhenSpec) bool {
	for _, predicate := range predicates {
		if !readyWhen(store, predicate) {
			return false
		}
	}
	return true
}

func readyWhen(store Store, predicate api.ReadyWhenSpec) bool {
	if len(predicate.AnyOf) > 0 {
		for _, group := range predicate.AnyOf {
			ok := len(group) > 0
			for _, item := range group {
				if !readyWhenPredicate(store, item) {
					ok = false
					break
				}
			}
			if ok {
				return true
			}
		}
		return false
	}
	return readyWhenPredicate(store, api.ReadyWhenPredicateSpec{Field: predicate.Field, Equals: predicate.Equals, NotEmpty: predicate.NotEmpty})
}

func readyWhenPredicate(store Store, predicate api.ReadyWhenPredicateSpec) bool {
	value := strings.TrimSpace(valueFromStatusRef(store, predicate.Field))
	if predicate.NotEmpty && value == "" {
		return false
	}
	if predicate.Equals != "" && value != predicate.Equals {
		return false
	}
	if predicate.Field != "" && !predicate.NotEmpty && predicate.Equals == "" {
		return value != ""
	}
	return true
}

func deriveIPv6AddressFromPrefix(value, subnetID, suffix string) (string, error) {
	prefix, err := netip.ParsePrefix(value)
	if err != nil || !prefix.Addr().Is6() {
		return "", fmt.Errorf("invalid delegated IPv6 prefix %q", value)
	}
	if prefix.Bits() > 64 {
		return "", fmt.Errorf("delegated IPv6 prefix %q is longer than /64", value)
	}
	subnet, err := parseIPv6SubnetID(subnetID)
	if err != nil {
		return "", err
	}
	subnetBits := 64 - prefix.Bits()
	if subnetBits < 64 && subnet >= (uint64(1)<<subnetBits) {
		return "", fmt.Errorf("subnetID %q does not fit in delegated prefix %s", subnetID, value)
	}
	suffixAddr, err := netip.ParseAddr(firstNonEmpty(suffix, "::1"))
	if err != nil || !suffixAddr.Is6() {
		return "", fmt.Errorf("invalid IPv6 suffix %q", suffix)
	}
	addrBytes := prefix.Masked().Addr().As16()
	first64 := binary.BigEndian.Uint64(addrBytes[:8])
	first64 |= subnet
	binary.BigEndian.PutUint64(addrBytes[:8], first64)
	suffixBytes := suffixAddr.As16()
	for i := range addrBytes {
		addrBytes[i] |= suffixBytes[i]
	}
	return netip.AddrFrom16(addrBytes).String(), nil
}

func parseIPv6SubnetID(value string) (uint64, error) {
	if value == "" {
		return 0, nil
	}
	if parsed, err := strconv.ParseUint(value, 0, 64); err == nil {
		return parsed, nil
	}
	parsed, err := strconv.ParseUint(value, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid IPv6 subnetID %q", value)
	}
	return parsed, nil
}

func interfaceName(router *api.Router, name string) string {
	if router == nil || name == "" {
		return ""
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "Interface" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.InterfaceSpec()
		if err == nil && spec.IfName != "" {
			return spec.IfName
		}
	}
	return name
}

func ensureIPv6LocalEndpoint(ctx context.Context, ifname, address string) error {
	if ifname == "" || address == "" {
		return nil
	}
	return exec.CommandContext(ctx, "ip", "-6", "addr", "replace", address+"/128", "dev", ifname).Run()
}

func ensureDSLiteTunnel(ctx context.Context, router *api.Router, spec api.DSLiteTunnelSpec, ifname, remote, local string) (string, error) {
	show, showErr := exec.CommandContext(ctx, "ip", "-6", "tunnel", "show", ifname).CombinedOutput()
	if showErr == nil && strings.Contains(string(show), "remote "+remote) && strings.Contains(string(show), "local "+local) {
		return ifname, nil
	}
	_ = exec.CommandContext(ctx, "ip", "-6", "tunnel", "del", ifname).Run()
	args := []string{"-6", "tunnel", "add", ifname, "mode", "ipip6", "remote", remote, "local", local}
	if underlay := interfaceName(router, spec.Interface); underlay != "" && underlay != ifname {
		args = append(args, "dev", underlay)
	}
	args = append(args, "encaplimit", firstNonEmpty(spec.EncapsulationLimit, "none"))
	out, err := exec.CommandContext(ctx, "ip", args...).CombinedOutput()
	if err != nil {
		if existing := existingDSLiteTunnel(ctx, remote, local); existing != "" {
			return existing, nil
		}
		return "", fmt.Errorf("ip %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return ifname, nil
}

func existingDSLiteTunnel(ctx context.Context, remote, local string) string {
	out, err := exec.CommandContext(ctx, "ip", "-6", "tunnel", "show").CombinedOutput()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "remote "+remote) || !strings.Contains(line, "local "+local) {
			continue
		}
		name, _, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(name) != "" && strings.TrimSpace(name) != "ip6tnl0" {
			return strings.TrimSpace(name)
		}
	}
	return ""
}

func writeRadvdConfig(path, ifname, prefix string, rdnss []string, preferred, valid string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	preferred = firstNonEmpty(preferred, "3600")
	valid = firstNonEmpty(valid, "7200")
	var b strings.Builder
	fmt.Fprintf(&b, "interface %s {\n", ifname)
	b.WriteString("  AdvSendAdvert on;\n")
	fmt.Fprintf(&b, "  prefix %s {\n", prefix)
	fmt.Fprintf(&b, "    AdvPreferredLifetime %s;\n", preferred)
	fmt.Fprintf(&b, "    AdvValidLifetime %s;\n", valid)
	b.WriteString("  };\n")
	if len(rdnss) > 0 {
		fmt.Fprintf(&b, "  RDNSS %s {};\n", strings.Join(rdnss, " "))
	}
	b.WriteString("};\n")
	return writeFileIfChanged(path, []byte(b.String()), 0644, false)
}

func writeDnsmasqDHCPv6Config(path, pidFile, ifname string, dnsServers []string, port int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "port=%d\nno-resolv\nno-hosts\nlisten-address=127.0.0.1\nbind-interfaces\npid-file=%s\n", port, pidFile)
	fmt.Fprintf(&b, "interface=%s\n", ifname)
	b.WriteString("enable-ra\n")
	fmt.Fprintf(&b, "dhcp-range=::,constructor:%s,ra-stateless,64,12h\n", ifname)
	for _, server := range dnsServers {
		fmt.Fprintf(&b, "dhcp-option=option6:dns-server,[%s]\n", server)
	}
	return os.WriteFile(path, []byte(b.String()), 0644)
}
