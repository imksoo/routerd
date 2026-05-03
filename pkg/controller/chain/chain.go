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
	"syscall"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/conntrack"
	"routerd/pkg/controller/conntrackobserver"
	"routerd/pkg/controller/dhcp4lease"
	"routerd/pkg/controller/nat44"
	"routerd/pkg/controller/pppoesession"
	"routerd/pkg/daemonapi"
	"routerd/pkg/derived"
	"routerd/pkg/eventrule"
	"routerd/pkg/healthcheck"
	daemonsource "routerd/pkg/source/daemon"
	"routerd/pkg/wanegress"
)

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
	DryRunDHCP4Lease   bool
	DryRunPPPoESession bool
	DryRunNAT          bool
	DnsmasqCommand     string
	DnsmasqConfig      string
	DnsmasqPID         string
	DnsmasqPort        int
	NftablesPath       string
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
		if resource.Kind != "IPv6PrefixDelegation" {
			continue
		}
		name := resource.Metadata.Name
		socket := r.Opts.DaemonSockets[name]
		if socket == "" {
			socket = filepath.Join("/run/routerd/dhcp6-client", name+".sock")
		}
		source := daemonsource.DaemonSource{
			Daemon:    daemonapi.DaemonRef{Name: "routerd-dhcp6-client-" + name, Kind: "routerd-dhcp6-client", Instance: name},
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
			socket = filepath.Join("/run/routerd/dhcp4-client", name+".sock")
		}
		source := daemonsource.DaemonSource{
			Daemon:    daemonapi.DaemonRef{Name: "routerd-dhcp4-client-" + name, Kind: "routerd-dhcp4-client", Instance: name},
			Socket:    socket,
			Publisher: r.Bus,
		}
		go func() {
			if err := source.Run(ctx); err != nil && ctx.Err() == nil {
				logger.Warn("dhcp4 daemon source stopped", "resource", name, "error", err)
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
	resolver := DNSResolverUpstreamController{Router: r.Router, Bus: r.Bus, Store: r.Store, Command: r.Opts.DnsmasqCommand, ConfigPath: r.Opts.DnsmasqConfig, PIDFile: r.Opts.DnsmasqPID, Port: r.Opts.DnsmasqPort, Logger: logger}
	dslite := DSLiteTunnelController{Router: r.Router, Bus: r.Bus, Store: r.Store, DryRun: r.Opts.DryRunDSLite, ResolverPort: r.Opts.DnsmasqPort, Logger: logger}
	route := IPv4RouteController{Router: r.Router, Bus: r.Bus, Store: r.Store, DryRun: r.Opts.DryRunRoute, Logger: logger}
	ra := IPv6RouterAdvertisementController{Router: r.Router, Bus: r.Bus, Store: r.Store, DryRun: r.Opts.DryRunRA, Logger: logger}
	dhcp6 := IPv6DHCPv6ServerController{Router: r.Router, Bus: r.Bus, Store: r.Store, DryRun: r.Opts.DryRunDHCPv6, Command: r.Opts.DnsmasqCommand, ConfigPath: r.Opts.DnsmasqConfig, PIDFile: r.Opts.DnsmasqPID, Port: r.Opts.DnsmasqPort, Logger: logger}
	dhcp4Lease := dhcp4lease.Controller{Router: r.Router, Bus: r.Bus, Store: r.Store, DaemonSockets: r.Opts.DaemonSockets, DryRun: r.Opts.DryRunDHCP4Lease, Logger: logger}
	pppoeSession := pppoesession.Controller{Router: r.Router, Bus: r.Bus, Store: r.Store, DaemonSockets: r.Opts.DaemonSockets, DryRun: r.Opts.DryRunPPPoESession, Logger: logger}
	dns := DNSAnswerController{Router: r.Router, Bus: r.Bus, Store: r.Store, Command: r.Opts.DnsmasqCommand, ConfigPath: r.Opts.DnsmasqConfig, PIDFile: r.Opts.DnsmasqPID, Port: r.Opts.DnsmasqPort, Logger: logger}
	wan := wanegress.Controller{Router: r.Router, Bus: r.Bus, Store: r.Store, Logger: logger}
	rules := eventrule.Controller{Router: r.Router, Bus: r.Bus, Store: r.Store, Logger: logger}
	derivedEvents := derived.Controller{Router: r.Router, Bus: r.Bus, Store: r.Store, Logger: logger}
	health := healthcheck.Controller{Router: r.Router, Bus: r.Bus, Store: r.Store, Logger: logger}
	nat := nat44.Controller{Router: r.Router, Bus: r.Bus, Store: r.Store, DryRun: r.Opts.DryRunNAT, NftablesPath: r.Opts.NftablesPath, NftCommand: r.Opts.NftCommand, Logger: logger}
	conntrackObs := conntrackobserver.Controller{Bus: r.Bus, Store: r.Store, Paths: conntrack.DefaultPaths(), Interval: r.Opts.ConntrackInterval, Logger: logger}
	rules.Start(ctx)
	derivedEvents.Start(ctx)
	health.Start(ctx)
	pd.Start(ctx)
	info.Start(ctx)
	lan.Start(ctx)
	resolver.Start(ctx)
	dslite.Start(ctx)
	route.Start(ctx)
	ra.Start(ctx)
	dhcp6.Start(ctx)
	dhcp4Lease.Start(ctx)
	pppoeSession.Start(ctx)
	dns.Start(ctx)
	wan.Start(ctx)
	nat.Start(ctx)
	conntrackObs.Start(ctx)
	go func() {
		for _, resource := range r.Router.Spec.Resources {
			if resource.Kind != "DHCPv4Lease" {
				continue
			}
			if err := dhcp4Lease.Reconcile(ctx, resource.Metadata.Name); err != nil && logger != nil && ctx.Err() == nil {
				logger.Warn("initial dhcp4 lease reconcile failed", "resource", resource.Metadata.Name, "error", err)
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
			if err := renderAndEnsureDnsmasq(ctx, r.Router, r.Store, r.Opts.DnsmasqCommand, r.Opts.DnsmasqConfig, r.Opts.DnsmasqPID, r.Opts.DnsmasqPort); err != nil && logger != nil && ctx.Err() == nil {
				logger.Warn("initial dnsmasq reconcile failed", "error", err)
			}
			if err := dns.reconcile(ctx, ""); err != nil && logger != nil && ctx.Err() == nil {
				logger.Warn("initial dns answer reconcile failed", "error", err)
			}
		}()
	}
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
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.dhcp6.client.prefix.*"}}, 32)
	go func() {
		for event := range ch {
			if event.Resource == nil || event.Resource.Kind != "IPv6PrefixDelegation" {
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
		if resource.Resource.Kind != "IPv6PrefixDelegation" || resource.Resource.Name != event.Resource.Name {
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
	return fmt.Errorf("daemon status did not include IPv6PrefixDelegation/%s", event.Resource.Name)
}

func (c PrefixDelegationController) socketFor(resource string) string {
	if socket := c.DaemonSockets[resource]; socket != "" {
		return socket
	}
	return filepath.Join("/run/routerd/dhcp6-client", resource+".sock")
}

type LANAddressController struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	DryRun bool
	Logger *slog.Logger
}

func (c LANAddressController) Start(ctx context.Context) {
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.dhcp6.client.prefix.*"}}, 32)
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
	pdStatus := c.Store.ObjectStatus(api.NetAPIVersion, "IPv6PrefixDelegation", pdName)
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
		if source == "" && strings.Contains(spec.PrefixSource, "IPv6PrefixDelegation/"+pdName+".status.currentPrefix") {
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

type DNSAnswerController struct {
	Router     *api.Router
	Bus        *bus.Bus
	Store      Store
	Command    string
	ConfigPath string
	PIDFile    string
	Port       int
	Logger     *slog.Logger
}

type DNSResolverUpstreamController struct {
	Router     *api.Router
	Bus        *bus.Bus
	Store      Store
	Command    string
	ConfigPath string
	PIDFile    string
	Port       int
	Logger     *slog.Logger
}

func (c DNSResolverUpstreamController) Start(ctx context.Context) {
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.dhcp6.info.*"}}, 32)
	go func() {
		for range ch {
			if err := c.reconcile(ctx); err != nil && c.Logger != nil {
				c.Logger.Warn("dns resolver upstream reconcile failed", "error", err)
			}
		}
	}()
}

func (c DNSResolverUpstreamController) reconcile(ctx context.Context) error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DNSResolverUpstream" {
			continue
		}
		if _, err := resource.DNSResolverUpstreamSpec(); err != nil {
			return err
		}
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
		status := map[string]any{"phase": "Applied", "configPath": configPath, "pidFile": pidFile, "port": port}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DNSResolverUpstream", resource.Metadata.Name, status); err != nil {
			return err
		}
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.dns.resolver.applied", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DNSResolverUpstream", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{"configPath": configPath, "port": fmt.Sprintf("%d", port)}
		if err := c.Bus.Publish(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (c DNSAnswerController) Start(ctx context.Context) {
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.lan.address.*"}}, 32)
	go func() {
		for event := range ch {
			if event.Resource == nil {
				continue
			}
			if err := c.reconcile(ctx, event.Resource.Name); err != nil && c.Logger != nil {
				c.Logger.Warn("dns answer reconcile failed", "address", event.Resource.Name, "error", err)
			}
		}
	}()
}

func (c DNSAnswerController) reconcile(ctx context.Context, delegatedAddress string) error {
	var address string
	if delegatedAddress != "" {
		addressStatus := c.Store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", delegatedAddress)
		if addressStatus["phase"] != "Applied" {
			return nil
		}
		address, _ = addressStatus["address"].(string)
		if address == "" {
			return nil
		}
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DNSAnswerScope" {
			continue
		}
		spec, err := resource.DNSAnswerScopeSpec()
		if err != nil {
			return err
		}
		if spec.DelegatedAddress != "" && spec.DelegatedAddress != delegatedAddress {
			continue
		}
		if spec.DelegatedAddress == "" && delegatedAddress != "" {
			continue
		}
		if spec.Family != "" && spec.Family != "ipv6" {
			continue
		}
		hostname := spec.Hostname
		if hostname == "" {
			hostname = resource.Metadata.Name + ".routerd.test"
		}
		configPath := firstNonEmpty(spec.ConfigPath, c.ConfigPath, "/run/routerd/dnsmasq-phase1.conf")
		pidFile := firstNonEmpty(spec.PIDFile, c.PIDFile, "/run/routerd/dnsmasq-phase1.pid")
		port := spec.Port
		if port == 0 {
			port = c.Port
		}
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
		status := map[string]any{"phase": "Applied", "hostname": hostname, "address": address, "port": port, "configPath": configPath, "pidFile": pidFile}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DNSAnswerScope", resource.Metadata.Name, status); err != nil {
			return err
		}
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.dns.answer.applied", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DNSAnswerScope", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{"hostname": hostname, "address": address, "port": fmt.Sprintf("%d", port)}
		if err := c.Bus.Publish(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func renderAndEnsureDnsmasq(ctx context.Context, router *api.Router, store Store, command, configPath, pidFile string, port int) error {
	configPath = firstNonEmpty(configPath, "/run/routerd/dnsmasq-phase1.conf")
	pidFile = firstNonEmpty(pidFile, "/run/routerd/dnsmasq-phase1.pid")
	if port == 0 {
		port = 1053
	}
	changed, err := writeDnsmasqConfig(router, store, configPath, pidFile, port)
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
		case "DNSResolverUpstream", "DNSAnswerScope", "IPv4DHCPServer", "IPv6DHCPv6Server", "IPv6RouterAdvertisement", "DHCPRelay":
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

func writeDnsmasqConfig(router *api.Router, store Store, path, pidFile string, port int) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "port=%d\nno-resolv\nno-hosts\nlisten-address=127.0.0.1\nbind-interfaces\npid-file=%s\n", port, pidFile)
	for _, line := range dnsmasqResolverLines(router, store) {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, line := range dnsmasqLANServiceLines(router, store) {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, line := range dnsmasqHostRecordLines(router, store) {
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

func dnsmasqLANServiceLines(router *api.Router, store Store) []string {
	aliases := chainInterfaceAliases(router)
	var lines []string
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "IPv4DHCPServer" {
			continue
		}
		spec, err := resource.IPv4DHCPServerSpec()
		if err != nil || spec.Interface == "" {
			continue
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			continue
		}
		tag := sanitizeChainTag(resource.Metadata.Name)
		lines = append(lines, "interface="+ifname)
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
			lines = append(lines, "dhcp-option=tag:"+tag+","+dnsmasqDHCPOption(option))
		}
		for _, reservation := range router.Spec.Resources {
			if reservation.Kind != "IPv4DHCPReservation" {
				continue
			}
			reservationSpec, err := reservation.IPv4DHCPReservationSpec()
			if err != nil {
				continue
			}
			if reservationSpec.Server != "" && reservationSpec.Server != resource.Metadata.Name {
				continue
			}
			reservationTag := sanitizeChainTag(reservation.Metadata.Name)
			lines = append(lines, "dhcp-host="+dnsmasqIPv4Reservation(reservationSpec, reservationTag))
			for _, option := range reservationSpec.Options {
				lines = append(lines, "dhcp-option=tag:"+reservationTag+","+dnsmasqDHCPOption(option))
			}
		}
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "IPv6DHCPv6Server" {
			continue
		}
		spec, err := resource.IPv6DHCPv6ServerSpec()
		if err != nil {
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
		if resource.Kind != "DHCPRelay" {
			continue
		}
		spec, err := resource.DHCPRelaySpec()
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

func dnsmasqResolverLines(router *api.Router, store Store) []string {
	var lines []string
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DNSResolverUpstream" {
			continue
		}
		spec, err := resource.DNSResolverUpstreamSpec()
		if err != nil {
			continue
		}
		for _, server := range expandServers(store, spec.Default.Servers) {
			lines = append(lines, "server="+server)
		}
		for _, zone := range spec.Zones {
			cleanZone := strings.Trim(strings.TrimSpace(zone.Zone), ".")
			if cleanZone == "" {
				continue
			}
			for _, server := range expandServers(store, zone.Servers) {
				lines = append(lines, fmt.Sprintf("server=/%s/%s", cleanZone, server))
			}
		}
	}
	return lines
}

func dnsmasqHostRecordLines(router *api.Router, store Store) []string {
	var lines []string
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DNSAnswerScope" {
			continue
		}
		spec, err := resource.DNSAnswerScopeSpec()
		if err != nil || (spec.Family != "" && spec.Family != "ipv6") {
			continue
		}
		if spec.DelegatedAddress == "" {
			for _, record := range spec.HostRecords {
				addresses := compactStrings(record.IPv4, record.IPv6)
				if record.Hostname != "" && len(addresses) > 0 {
					lines = append(lines, fmt.Sprintf("host-record=%s,%s", record.Hostname, strings.Join(addresses, ",")))
				}
			}
			if spec.LocalDomain != "" {
				domain := strings.Trim(spec.LocalDomain, ".")
				lines = append(lines, "domain="+domain, "local=/"+domain+"/")
				if spec.DDNS {
					lines = append(lines, "dhcp-fqdn")
				}
			}
			if spec.DNSSEC {
				lines = append(lines, "dnssec")
				lines = append(lines, dnsmasqRootTrustAnchor)
			}
			continue
		}
		addressStatus := store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", spec.DelegatedAddress)
		if addressStatus["phase"] != "Applied" {
			continue
		}
		address, _ := addressStatus["address"].(string)
		if address == "" {
			continue
		}
		hostAddress := strings.TrimSuffix(address, "/64")
		if strings.Contains(hostAddress, "/") {
			hostAddress = strings.Split(hostAddress, "/")[0]
		}
		hostname := spec.Hostname
		if hostname == "" {
			hostname = resource.Metadata.Name + ".routerd.test"
		}
		lines = append(lines, fmt.Sprintf("host-record=%s,%s", hostname, hostAddress))
		for _, record := range spec.HostRecords {
			addresses := compactStrings(record.IPv4, record.IPv6)
			if record.Hostname != "" && len(addresses) > 0 {
				lines = append(lines, fmt.Sprintf("host-record=%s,%s", record.Hostname, strings.Join(addresses, ",")))
			}
		}
		if spec.LocalDomain != "" {
			domain := strings.Trim(spec.LocalDomain, ".")
			lines = append(lines, "domain="+domain, "local=/"+domain+"/")
			if spec.DDNS {
				lines = append(lines, "dhcp-fqdn")
			}
		}
		if spec.DNSSEC {
			lines = append(lines, "dnssec")
			lines = append(lines, dnsmasqRootTrustAnchor)
		}
	}
	return lines
}

const dnsmasqRootTrustAnchor = "trust-anchor=.,20326,8,2,E06D44B80B8F1D39A95C0B0D7C65D08458E880409BBC683457104237C7F8EC8D"

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

func dnsmasqDHCPOption(option api.DHCPOptionSpec) string {
	key := option.Name
	if key == "" {
		key = fmt.Sprintf("%d", option.Code)
	} else {
		key = "option:" + key
	}
	return key + "," + option.Value
}

func dnsmasqIPv4Reservation(spec api.IPv4DHCPReservationSpec, tag string) string {
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

func compactStrings(values ...string) []string {
	var out []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
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
		return nil, false
	}
	return proc, true
}

func startDnsmasq(ctx context.Context, command, configPath, pidFile string) error {
	_ = os.Remove(pidFile)
	cmd := exec.CommandContext(ctx, firstNonEmpty(command, "dnsmasq"), "--conf-file="+configPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	if !waitForFile(pidFile, time.Second) {
		select {
		case <-done:
		default:
			_ = cmd.Process.Kill()
			<-done
		}
		return fmt.Errorf("dnsmasq did not create pid file %s", pidFile)
	}
	_, alive := dnsmasqProcess(pidFile)
	if !alive {
		select {
		case <-done:
		default:
		}
		return fmt.Errorf("dnsmasq is not alive")
	}
	return nil
}

func waitForFile(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
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
