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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/conntrack"
	"routerd/pkg/controller/conntrackobserver"
	"routerd/pkg/controller/dhcpv4lease"
	dnsresolvercontroller "routerd/pkg/controller/dnsresolver"
	firewallcontroller "routerd/pkg/controller/firewall"
	"routerd/pkg/controller/nat44"
	"routerd/pkg/controller/pppoesession"
	"routerd/pkg/daemonapi"
	"routerd/pkg/derived"
	"routerd/pkg/eventrule"
	"routerd/pkg/healthcheck"
	daemonsource "routerd/pkg/source/daemon"
	"routerd/pkg/wanegress"
)

var dnsmasqMu sync.Mutex

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type Options struct {
	DaemonSockets      map[string]string
	DryRunAddress      bool
	DryRunDSLite       bool
	DryRunRoute        bool
	DryRunRA           bool
	DryRunDHCPv6       bool
	DryRunDHCPv4Lease  bool
	DryRunPPPoESession bool
	DryRunDNSResolver  bool
	DryRunNAT          bool
	DryRunFirewall     bool
	FirewallDisabled   bool
	DnsmasqCommand     string
	DnsmasqConfig      string
	DnsmasqPID         string
	DnsmasqPort        int
	DnsmasqListen      []string
	NftablesPath       string
	FirewallPath       string
	NftCommand         string
	ConntrackInterval  time.Duration
	Logger             *slog.Logger
}

type Runner struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	Opts   Options
}

func (r *Runner) Start(ctx context.Context) error {
	if r.Router == nil || r.Bus == nil || r.Store == nil {
		return fmt.Errorf("router, bus, and store are required")
	}
	logger := r.Opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	for _, resource := range r.Router.Spec.Resources {
		if resource.Kind != "DHCPv6PrefixDelegation" {
			continue
		}
		name := resource.Metadata.Name
		socket := r.Opts.DaemonSockets[name]
		if socket == "" {
			socket = filepath.Join("/run/routerd/dhcpv6-client", name+".sock")
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
		if resource.Kind != "DHCPv4Lease" {
			continue
		}
		name := resource.Metadata.Name
		socket := r.Opts.DaemonSockets[name]
		if socket == "" {
			socket = filepath.Join("/run/routerd/dhcpv4-client", name+".sock")
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
		if err != nil || spec.SocketSource == "embedded" || (spec.Daemon == "" && spec.SocketSource == "") {
			continue
		}
		name := resource.Metadata.Name
		socket := spec.SocketSource
		if socket == "" {
			socket = filepath.Join("/run/routerd/healthcheck", name+".sock")
		}
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
		spec, err := resource.PPPoESessionSpec()
		if err != nil {
			continue
		}
		name := resource.Metadata.Name
		socket := spec.SocketSource
		if socket == "" {
			socket = r.Opts.DaemonSockets[name]
		}
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

	pd := PrefixDelegationController{Router: r.Router, Bus: r.Bus, Store: r.Store, DaemonSockets: r.Opts.DaemonSockets, Logger: logger}
	info := DHCPv6InformationController{Router: r.Router, Bus: r.Bus, Store: r.Store, DaemonSockets: r.Opts.DaemonSockets, Logger: logger}
	lan := LANAddressController{Router: r.Router, Bus: r.Bus, Store: r.Store, DryRun: r.Opts.DryRunAddress, Logger: logger}
	dslite := DSLiteTunnelController{Router: r.Router, Bus: r.Bus, Store: r.Store, DryRun: r.Opts.DryRunDSLite, ResolverPort: r.Opts.DnsmasqPort, Logger: logger}
	route := IPv4RouteController{Router: r.Router, Bus: r.Bus, Store: r.Store, DryRun: r.Opts.DryRunRoute, Logger: logger}
	ra := IPv6RouterAdvertisementController{Router: r.Router, Bus: r.Bus, Store: r.Store, DryRun: r.Opts.DryRunRA, Logger: logger}
	dhcpv6 := DHCPv6ServerController{Router: r.Router, Bus: r.Bus, Store: r.Store, DryRun: r.Opts.DryRunDHCPv6, Command: r.Opts.DnsmasqCommand, ConfigPath: r.Opts.DnsmasqConfig, PIDFile: r.Opts.DnsmasqPID, Port: r.Opts.DnsmasqPort, ListenAddresses: r.Opts.DnsmasqListen, Logger: logger}
	dhcp4Lease := dhcpv4lease.Controller{Router: r.Router, Bus: r.Bus, Store: r.Store, DaemonSockets: r.Opts.DaemonSockets, DryRun: r.Opts.DryRunDHCPv4Lease, Logger: logger}
	pppoeSession := pppoesession.Controller{Router: r.Router, Bus: r.Bus, Store: r.Store, DaemonSockets: r.Opts.DaemonSockets, DryRun: r.Opts.DryRunPPPoESession, Logger: logger}
	dnsResolver := dnsresolvercontroller.Controller{Router: r.Router, Bus: r.Bus, Store: r.Store, DryRun: r.Opts.DryRunDNSResolver}
	wan := wanegress.Controller{Router: r.Router, Bus: r.Bus, Store: r.Store, Logger: logger}
	rules := eventrule.Controller{Router: r.Router, Bus: r.Bus, Store: r.Store, Logger: logger}
	derivedEvents := derived.Controller{Router: r.Router, Bus: r.Bus, Store: r.Store, Logger: logger}
	health := healthcheck.Controller{Router: r.Router, Bus: r.Bus, Store: r.Store, Logger: logger}
	nat := nat44.Controller{Router: r.Router, Bus: r.Bus, Store: r.Store, DryRun: r.Opts.DryRunNAT, NftablesPath: r.Opts.NftablesPath, NftCommand: r.Opts.NftCommand, Logger: logger}
	firewall := firewallcontroller.Controller{Router: r.Router, Bus: r.Bus, Store: r.Store, DryRun: r.Opts.DryRunFirewall, NftablesPath: firstNonEmpty(r.Opts.FirewallPath, "/run/routerd/firewall.nft"), NftCommand: r.Opts.NftCommand, Logger: logger}
	conntrackObs := conntrackobserver.Controller{Bus: r.Bus, Store: r.Store, Paths: conntrack.DefaultPaths(), Interval: r.Opts.ConntrackInterval, Logger: logger}
	rules.Start(ctx)
	derivedEvents.Start(ctx)
	health.Start(ctx)
	pd.Start(ctx)
	info.Start(ctx)
	lan.Start(ctx)
	dslite.Start(ctx)
	route.Start(ctx)
	ra.Start(ctx)
	dhcpv6.Start(ctx)
	dhcp4Lease.Start(ctx)
	pppoeSession.Start(ctx)
	wan.Start(ctx)
	nat.Start(ctx)
	if !r.Opts.FirewallDisabled {
		firewall.Start(ctx)
	}
	conntrackObs.Start(ctx)
	go func() {
		for _, resource := range r.Router.Spec.Resources {
			if resource.Kind != "DHCPv4Lease" {
				continue
			}
			if err := dhcp4Lease.Reconcile(ctx, resource.Metadata.Name); err != nil && logger != nil && ctx.Err() == nil {
				logger.Warn("initial dhcpv4 lease reconcile failed", "resource", resource.Metadata.Name, "error", err)
			}
		}
	}()
	go func() {
		for _, resource := range r.Router.Spec.Resources {
			if resource.Kind != "PPPoESession" {
				continue
			}
			if err := pppoeSession.Reconcile(ctx, resource.Metadata.Name); err != nil && logger != nil && ctx.Err() == nil {
				logger.Warn("initial pppoe session reconcile failed", "resource", resource.Metadata.Name, "error", err)
			}
		}
	}()
	if routerNeedsDnsmasq(r.Router) {
		go func() {
			if err := renderAndEnsureDnsmasq(ctx, r.Router, r.Store, r.Opts.DnsmasqCommand, r.Opts.DnsmasqConfig, r.Opts.DnsmasqPID, r.Opts.DnsmasqPort, r.Opts.DnsmasqListen); err != nil && logger != nil && ctx.Err() == nil {
				logger.Warn("initial dnsmasq reconcile failed", "error", err)
			}
			dnsResolver.Start(ctx)
		}()
	} else {
		dnsResolver.Start(ctx)
	}
	go func() {
		for _, resource := range r.Router.Spec.Resources {
			if resource.Kind != "DHCPv6PrefixDelegation" {
				continue
			}
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.controller.bootstrap", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation", Name: resource.Metadata.Name}
			if err := pd.reconcile(ctx, event); err != nil {
				if logger != nil && ctx.Err() == nil {
					logger.Warn("initial prefix delegation reconcile failed", "resource", resource.Metadata.Name, "error", err)
				}
				continue
			}
			if err := lan.reconcile(ctx, resource.Metadata.Name); err != nil && logger != nil && ctx.Err() == nil {
				logger.Warn("initial lan address reconcile failed", "pd", resource.Metadata.Name, "error", err)
			}
			if err := info.reconcile(ctx, resource.Metadata.Name, true); err != nil && logger != nil && ctx.Err() == nil {
				logger.Warn("initial dhcpv6 information reconcile failed", "pd", resource.Metadata.Name, "error", err)
			}
		}
		if err := dslite.reconcile(ctx); err != nil && logger != nil && ctx.Err() == nil {
			logger.Warn("initial dslite tunnel reconcile failed", "error", err)
		}
		if err := route.reconcile(ctx); err != nil && logger != nil && ctx.Err() == nil {
			logger.Warn("initial ipv4 route reconcile failed", "error", err)
		}
		if err := wan.Reconcile(ctx); err != nil && logger != nil && ctx.Err() == nil {
			logger.Warn("initial wan egress reconcile failed", "error", err)
		}
		if err := nat.Reconcile(ctx); err != nil && logger != nil && ctx.Err() == nil {
			logger.Warn("initial nat44 reconcile failed", "error", err)
		}
	}()
	return nil
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
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	DryRun bool
	Logger *slog.Logger
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
		source := spec.PrefixDelegation
		if source == "" && strings.Contains(spec.PrefixSource, "DHCPv6PrefixDelegation/"+pdName+".status.currentPrefix") {
			source = pdName
		}
		if source != pdName {
			continue
		}
		if !c.linkReady(spec.Interface) {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", resource.Metadata.Name, map[string]any{"phase": "Pending"})
			continue
		}
		addr, err := DeriveIPv6Address(prefix, spec.SubnetID, spec.AddressSuffix)
		if err != nil {
			return err
		}
		if !c.DryRun {
			if err := exec.CommandContext(ctx, "ip", "-6", "addr", "replace", addr, "dev", spec.Interface).Run(); err != nil {
				return err
			}
		}
		status := map[string]any{
			"phase":        "Applied",
			"address":      addr,
			"interface":    spec.Interface,
			"prefixSource": pdName,
			"dryRun":       c.DryRun,
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", resource.Metadata.Name, status); err != nil {
			return err
		}
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.lan.address.applied", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{"address": addr, "interface": spec.Interface, "dryRun": fmt.Sprintf("%t", c.DryRun)}
		if err := c.Bus.Publish(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (c LANAddressController) linkReady(name string) bool {
	if status := c.Store.ObjectStatus(api.NetAPIVersion, "Link", name); status != nil {
		return status["phase"] == "Up"
	}
	ifi, err := net.InterfaceByName(name)
	if err == nil && ifi.Flags&net.FlagUp != 0 {
		_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "Link", name, map[string]any{"phase": "Up", "ifname": name})
		return true
	}
	return false
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
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socketPath)
	}}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/status", nil)
	if err != nil {
		return daemonapi.DaemonStatus{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return daemonapi.DaemonStatus{}, err
	}
	defer resp.Body.Close()
	var status daemonapi.DaemonStatus
	return status, json.NewDecoder(resp.Body).Decode(&status)
}

func writeDnsmasqConfig(router *api.Router, store Store, path, pidFile string, port int, listenAddresses []string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "port=0\nno-resolv\nno-hosts\nbind-interfaces\npid-file=%s\n", pidFile)
	for _, line := range dnsmasqLANServiceLines(router, store) {
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

func dnsmasqLANServiceLines(router *api.Router, store Store) []string {
	aliases := chainInterfaceAliases(router)
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
		tag := sanitizeChainTag(resource.Metadata.Name)
		lines = append(lines, "interface="+ifname)
		lines = append(lines, "dhcp-script=/usr/local/libexec/routerd/dhcp-event-relay")
		leaseTime := firstNonEmpty(spec.AddressPool.LeaseTime, "12h")
		lines = append(lines, fmt.Sprintf("dhcp-range=set:%s,%s,%s,%s", tag, spec.AddressPool.Start, spec.AddressPool.End, leaseTime))
		if spec.Gateway != "" {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:router,%s", tag, spec.Gateway))
		}
		if len(spec.DNSServers) > 0 {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:dns-server,%s", tag, strings.Join(spec.DNSServers, ",")))
		}
		if len(spec.NTPServers) > 0 {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:ntp-server,%s", tag, strings.Join(spec.NTPServers, ",")))
		}
		if spec.Domain != "" {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:domain-name,%s", tag, spec.Domain))
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
		for _, server := range expandServers(store, spec.DNSServers) {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option6:dns-server,[%s]", tag, strings.Trim(server, "[]")))
		}
		if len(spec.DomainSearch) > 0 {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option6:domain-search,%s", tag, strings.Join(spec.DomainSearch, ",")))
		}
		for _, server := range expandServers(store, spec.SNTPServers) {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option6:sntp-server,[%s]", tag, strings.Trim(server, "[]")))
		}
		if spec.RapidCommit {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option6:rapid-commit", tag))
		}
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
		if spec.MTU != 0 {
			params = append(params, fmt.Sprintf("mtu:%d", spec.MTU))
		}
		switch spec.PRFPreference {
		case "high", "low":
			params = append(params, spec.PRFPreference)
		}
		if spec.ValidLifetime != "" {
			params = append(params, "0", spec.ValidLifetime)
		} else if spec.MTU != 0 && (spec.PRFPreference == "high" || spec.PRFPreference == "low") {
			params = append(params, "0")
		}
		if len(params) > 0 {
			lines = append(lines, fmt.Sprintf("ra-param=%s,%s", ifname, strings.Join(params, ",")))
		}
		for _, server := range expandServers(store, spec.RDNSS) {
			lines = append(lines, fmt.Sprintf("dhcp-option=option6:dns-server,[%s]", strings.Trim(server, "[]")))
		}
		if len(spec.DNSSL) > 0 {
			lines = append(lines, "dhcp-option=option6:domain-search,"+strings.Join(spec.DNSSL, ","))
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
	return lines
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

func ensureDnsmasq(ctx context.Context, command, configPath, pidFile string, changed bool) error {
	dnsmasqMu.Lock()
	defer dnsmasqMu.Unlock()

	proc, alive := dnsmasqProcess(pidFile)
	if alive && changed {
		return proc.Signal(syscall.SIGHUP)
	}
	if alive {
		return nil
	}
	return startDnsmasq(ctx, command, configPath, pidFile)
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

func atoi(value string) int {
	var out int
	_, _ = fmt.Sscanf(value, "%d", &out)
	return out
}
