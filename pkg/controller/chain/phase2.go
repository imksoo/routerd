package chain

import (
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
	"strconv"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
)

type DHCPv6InformationController struct {
	Router        *api.Router
	Bus           *bus.Bus
	Store         Store
	DaemonSockets map[string]string
	Logger        *slog.Logger
}

func (c DHCPv6InformationController) Start(ctx context.Context) {
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.dhcp6.client.prefix.*", "routerd.dhcp6.client.info.*"}}, 32)
	go func() {
		for event := range ch {
			if event.Resource == nil {
				continue
			}
			request := !strings.HasPrefix(event.Type, "routerd.dhcp6.client.info.")
			if err := c.reconcile(ctx, event.Resource.Name, request); err != nil && c.Logger != nil {
				c.Logger.Warn("dhcpv6 information reconcile failed", "pd", event.Resource.Name, "error", err)
			}
		}
	}()
}

func (c DHCPv6InformationController) reconcile(ctx context.Context, pdName string, request bool) error {
	pdStatus := c.Store.ObjectStatus(api.NetAPIVersion, "IPv6PrefixDelegation", pdName)
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
		observed := daemonObserved(status, "IPv6PrefixDelegation", pdName)
		next := map[string]any{
			"phase":        "Ready",
			"aftrName":     observed["aftrName"],
			"dnsServers":   decodeStringList(observed["dnsServers"]),
			"sntpServers":  decodeStringList(observed["sntpServers"]),
			"domainSearch": decodeStringList(observed["domainSearch"]),
			"source":       pdName,
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Information", resource.Metadata.Name, next); err != nil {
			return err
		}
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.dhcp6.info.updated", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Information", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{"aftrName": observed["aftrName"], "source": pdName}
		if err := c.Bus.Publish(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (c DHCPv6InformationController) matchesPD(resource api.Resource, spec api.DHCPv6InformationSpec, pdName string) bool {
	for _, owner := range resource.Metadata.OwnerRefs {
		if owner.Kind == "IPv6PrefixDelegation" && owner.Name == pdName {
			return true
		}
	}
	for _, candidate := range c.Router.Spec.Resources {
		if candidate.Kind != "IPv6PrefixDelegation" || candidate.Metadata.Name != pdName {
			continue
		}
		pdSpec, err := candidate.IPv6PrefixDelegationSpec()
		return err == nil && pdSpec.Interface == spec.Interface
	}
	return false
}

func (c DHCPv6InformationController) socketFor(resource string) string {
	if socket := c.DaemonSockets[resource]; socket != "" {
		return socket
	}
	return filepath.Join("/run/routerd/dhcp6-client", resource+".sock")
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
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.dhcp6.info.*", "routerd.dhcp6.client.prefix.*", "routerd.dns.resolver.*"}}, 32)
	go func() {
		for range ch {
			if err := c.reconcile(ctx); err != nil && c.Logger != nil {
				c.Logger.Warn("dslite tunnel reconcile failed", "error", err)
			}
		}
	}()
}

func (c DSLiteTunnelController) reconcile(ctx context.Context) error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DSLiteTunnel" {
			continue
		}
		spec, err := resource.DSLiteTunnelSpec()
		if err != nil {
			return err
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
		local := firstNonEmpty(valueFromStatusRef(c.Store, spec.LocalIPv6Source), valueFromStatusRef(c.Store, spec.LocalAddress), spec.LocalAddress)
		local = localIPv6Address(local)
		if local == "" {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "LocalIPv6Missing", "aftrName": aftrName, "aftrIPv6": remote})
			continue
		}
		ifname := firstNonEmpty(spec.TunnelName, spec.Interface, resource.Metadata.Name)
		mtu := spec.MTU
		if mtu == 0 {
			mtu = 1460
		}
		if !c.DryRun {
			if err := exec.CommandContext(ctx, "ip", "-6", "tunnel", "replace", ifname, "mode", "ip4ip6", "remote", remote, "local", local, "ttl", "64").Run(); err != nil {
				return err
			}
			if err := exec.CommandContext(ctx, "ip", "link", "set", ifname, "mtu", fmt.Sprintf("%d", mtu), "up").Run(); err != nil {
				return err
			}
		}
		status := map[string]any{"phase": "Up", "interface": ifname, "localIPv6": local, "aftrName": aftrName, "aftrIPv6": remote, "mtu": mtu, "dryRun": c.DryRun}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, status); err != nil {
			return err
		}
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.tunnel.ds-lite.up", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{"interface": ifname, "aftrIPv6": remote, "dryRun": fmt.Sprintf("%t", c.DryRun)}
		if err := c.Bus.Publish(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (c DSLiteTunnelController) resolveRemote(ctx context.Context, spec api.DSLiteTunnelSpec) (string, string, error) {
	var firstName string
	candidates := []string{
		valueFromStatusRef(c.Store, spec.AFTRSource),
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
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.tunnel.ds-lite.*"}}, 32)
	go func() {
		for range ch {
			if err := c.reconcile(ctx); err != nil && c.Logger != nil {
				c.Logger.Warn("ipv4 route reconcile failed", "error", err)
			}
		}
	}()
}

func (c IPv4RouteController) reconcile(ctx context.Context) error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "IPv4Route" {
			continue
		}
		spec, err := resource.IPv4RouteSpec()
		if err != nil {
			return err
		}
		device := firstNonEmpty(valueFromStatusRef(c.Store, spec.Device), spec.Device)
		if device == "" {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DeviceMissing"})
			continue
		}
		destination := firstNonEmpty(spec.Destination, "0.0.0.0/0")
		if !c.DryRun {
			args := []string{"route", "replace", destination, "dev", device}
			if spec.Metric > 0 {
				args = append(args, "metric", fmt.Sprintf("%d", spec.Metric))
			}
			if err := exec.CommandContext(ctx, "ip", args...).Run(); err != nil {
				return err
			}
		}
		status := map[string]any{"phase": "Installed", "destination": destination, "device": device, "metric": spec.Metric, "dryRun": c.DryRun, "installedAt": time.Now().UTC().Format(time.RFC3339Nano)}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", resource.Metadata.Name, status); err != nil {
			return err
		}
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.ipv4.route.installed", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv4Route", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{"destination": destination, "device": device, "dryRun": fmt.Sprintf("%t", c.DryRun)}
		if err := c.Bus.Publish(ctx, event); err != nil {
			return err
		}
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
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.lan.address.*", "routerd.dhcp6.info.*"}}, 32)
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
		prefix := prefixFromStatusOrAddress(c.Store, spec.PrefixSource)
		if prefix == "" {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "PrefixMissing"})
			continue
		}
		rdnss := expandServers(c.Store, spec.RDNSS)
		configPath := firstNonEmpty(spec.ConfigPath, "/run/routerd/radvd-phase2.conf")
		if err := writeRadvdConfig(configPath, spec.Interface, prefix, rdnss, spec.PreferredLifetime, spec.ValidLifetime); err != nil {
			return err
		}
		if !c.DryRun {
			if err := exec.CommandContext(ctx, "radvd", "-C", configPath, "-p", firstNonEmpty(spec.PIDFile, "/run/routerd/radvd-phase2.pid")).Start(); err != nil {
				return err
			}
		}
		status := map[string]any{"phase": "Applied", "interface": spec.Interface, "prefix": prefix, "rdnss": rdnss, "configPath": configPath, "dryRun": c.DryRun}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", resource.Metadata.Name, status); err != nil {
			return err
		}
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.ra.applied", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{"interface": spec.Interface, "prefix": prefix, "dryRun": fmt.Sprintf("%t", c.DryRun)}
		if err := c.Bus.Publish(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

type IPv6DHCPv6ServerController struct {
	Router     *api.Router
	Bus        *bus.Bus
	Store      Store
	DryRun     bool
	Command    string
	ConfigPath string
	PIDFile    string
	Port       int
	Logger     *slog.Logger
}

func (c IPv6DHCPv6ServerController) Start(ctx context.Context) {
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.lan.address.*", "routerd.dhcp6.info.*"}}, 32)
	go func() {
		for range ch {
			if err := c.reconcile(ctx); err != nil && c.Logger != nil {
				c.Logger.Warn("ipv6 dhcpv6 server reconcile failed", "error", err)
			}
		}
	}()
}

func (c IPv6DHCPv6ServerController) reconcile(ctx context.Context) error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "IPv6DHCPv6Server" {
			continue
		}
		spec, err := resource.IPv6DHCPv6ServerSpec()
		if err != nil {
			return err
		}
		dnsServers := expandServers(c.Store, spec.DNSServers)
		configPath := firstNonEmpty(c.ConfigPath, "/run/routerd/dnsmasq-phase1.conf")
		pidFile := firstNonEmpty(c.PIDFile, "/run/routerd/dnsmasq-phase1.pid")
		port := c.Port
		if port == 0 {
			port = 1053
		}
		changed, err := writeDnsmasqConfig(c.Router, c.Store, configPath, pidFile, port)
		if err != nil {
			return err
		}
		if err := ensureDnsmasq(ctx, c.Command, configPath, pidFile, changed); err != nil {
			return err
		}
		status := map[string]any{"phase": "Applied", "interface": spec.Interface, "mode": firstNonEmpty(spec.Mode, "stateless"), "dnsServers": dnsServers, "configPath": configPath, "pidFile": pidFile, "dryRun": c.DryRun}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6DHCPv6Server", resource.Metadata.Name, status); err != nil {
			return err
		}
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.lan.service.dhcpv6.applied", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6DHCPv6Server", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{"interface": spec.Interface, "dryRun": fmt.Sprintf("%t", c.DryRun)}
		if err := c.Bus.Publish(ctx, event); err != nil {
			return err
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

func writeRadvdConfig(path, ifname, prefix string, rdnss []string, preferred, valid string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
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
	return os.WriteFile(path, []byte(b.String()), 0644)
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
