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
	"reflect"
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
	"routerd/pkg/controller/framework"
	"routerd/pkg/controller/nat44"
	"routerd/pkg/controller/pppoesession"
	"routerd/pkg/daemonapi"
	"routerd/pkg/derived"
	"routerd/pkg/egressroute"
	"routerd/pkg/eventrule"
	"routerd/pkg/healthcheck"
	"routerd/pkg/resourcequery"
	daemonsource "routerd/pkg/source/daemon"
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

func (s eventedStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	if s.Store == nil {
		return nil
	}
	current := s.Store.ObjectStatus(apiVersion, kind, name)
	if newerStatus(current, status) {
		return nil
	}
	changed := statusChanged(current, status)
	if err := s.Store.SaveObjectStatus(apiVersion, kind, name, status); err != nil {
		return err
	}
	if changed && s.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "store"}, "routerd.resource.status.changed", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: apiVersion, Kind: kind, Name: name}
		event.Attributes = map[string]string{
			"phase":         fmt.Sprint(status["phase"]),
			"previousPhase": fmt.Sprint(current["phase"]),
		}
		return s.Bus.Publish(context.Background(), event)
	}
	return nil
}

func (s eventedStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if s.Store == nil {
		return nil
	}
	return s.Store.ObjectStatus(apiVersion, kind, name)
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

func stableStatus(status map[string]any) map[string]any {
	if status == nil {
		return nil
	}
	out := map[string]any{}
	for key, value := range status {
		switch key {
		case "updatedAt", "observedAt", "installedAt", "lastCheckedAt", "consecutivePassed", "consecutiveFailed", "createdHint", "packetRing", "conditions":
			continue
		default:
			out[key] = value
		}
	}
	return out
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
	DaemonSockets          map[string]string
	DryRunAddress          bool
	DryRunDSLite           bool
	DryRunRoute            bool
	DryRunRA               bool
	DryRunDHCPv6           bool
	DryRunDHCPv4Lease      bool
	DryRunPPPoESession     bool
	DryRunDNSResolver      bool
	DryRunNAT              bool
	DryRunFirewall         bool
	DryRunPackage          bool
	DryRunNetworkAdoption  bool
	DryRunSystemdUnit      bool
	SuperviseClientDaemons bool
	FirewallDisabled       bool
	DnsmasqCommand         string
	DnsmasqConfig          string
	DnsmasqPID             string
	DnsmasqPort            int
	DnsmasqListen          []string
	NftablesPath           string
	FirewallPath           string
	NftCommand             string
	ConntrackInterval      time.Duration
	Logger                 *slog.Logger
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
	if r.Opts.SuperviseClientDaemons {
		r.superviseClientDaemons(ctx, logger)
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

	store := eventedStore{Store: r.Store, Bus: r.Bus}
	packages := PackageController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunPackage}
	sysctl := SysctlController{Router: r.Router, Bus: r.Bus, Store: store}
	adoption := NetworkAdoptionController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunNetworkAdoption}
	systemdUnits := SystemdUnitController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunSystemdUnit}
	info := DHCPv6InformationController{Router: r.Router, Bus: r.Bus, Store: store, DaemonSockets: r.Opts.DaemonSockets, Logger: logger}
	link := LinkController{Router: r.Router, Store: store, Logger: logger}
	ipv4Static := IPv4StaticAddressController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunAddress, Logger: logger}
	lan := LANAddressController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunAddress, Logger: logger}
	dslite := DSLiteTunnelController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunDSLite, ResolverPort: r.Opts.DnsmasqPort, Logger: logger}
	route := IPv4RouteController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunRoute, Logger: logger}
	ra := IPv6RouterAdvertisementController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunRA, Logger: logger}
	dhcpv6 := DHCPv6ServerController{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunDHCPv6, Command: r.Opts.DnsmasqCommand, ConfigPath: r.Opts.DnsmasqConfig, PIDFile: r.Opts.DnsmasqPID, Port: r.Opts.DnsmasqPort, ListenAddresses: r.Opts.DnsmasqListen, Logger: logger}
	dhcp4Lease := dhcpv4lease.Controller{Router: r.Router, Bus: r.Bus, Store: store, DaemonSockets: r.Opts.DaemonSockets, DryRun: r.Opts.DryRunDHCPv4Lease, Logger: logger}
	pppoeSession := pppoesession.Controller{Router: r.Router, Bus: r.Bus, Store: store, DaemonSockets: r.Opts.DaemonSockets, DryRun: r.Opts.DryRunPPPoESession, Logger: logger}
	dnsResolver := dnsresolvercontroller.Controller{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunDNSResolver}
	daemonStatusSync := DaemonStatusController{Router: r.Router, Bus: r.Bus, Store: store, DaemonSockets: r.Opts.DaemonSockets, Logger: logger}
	wan := egressroute.Controller{Router: r.Router, Bus: r.Bus, Store: store, Logger: logger}
	rules := eventrule.Controller{Router: r.Router, Bus: r.Bus, Store: store, Logger: logger}
	derivedEvents := derived.Controller{Router: r.Router, Bus: r.Bus, Store: store, Logger: logger}
	health := healthcheck.Controller{Router: r.Router, Bus: r.Bus, Store: store, Logger: logger}
	nat := nat44.Controller{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunNAT, NftablesPath: r.Opts.NftablesPath, NftCommand: r.Opts.NftCommand, Logger: logger}
	firewall := firewallcontroller.Controller{Router: r.Router, Bus: r.Bus, Store: store, DryRun: r.Opts.DryRunFirewall, NftablesPath: firstNonEmpty(r.Opts.FirewallPath, "/run/routerd/firewall.nft"), NftCommand: r.Opts.NftCommand, Logger: logger}
	conntrackObs := conntrackobserver.Controller{Bus: r.Bus, Store: store, Paths: conntrack.DefaultPaths(), Interval: r.Opts.ConntrackInterval, Logger: logger}
	rules.Start(ctx)
	derivedEvents.Start(ctx)
	health.Start(ctx)
	conntrackObs.Start(ctx)
	controllers := []framework.Controller{
		framework.FuncController{ControllerName: "daemon-status", Every: 5 * time.Second, Subs: []bus.Subscription{{Topics: []string{"routerd.dhcpv6.client.**", "routerd.dhcpv4.client.**", "routerd.healthcheck.**", "routerd.pppoe.client.**"}}}, PeriodicFunc: daemonStatusSync.Reconcile},
		framework.FuncController{ControllerName: "package", Every: 5 * time.Minute, PeriodicFunc: packages.Reconcile},
		framework.FuncController{ControllerName: "sysctl", Every: 30 * time.Second, PeriodicFunc: sysctl.Reconcile},
		framework.FuncController{ControllerName: "network-adoption", Every: 5 * time.Minute, PeriodicFunc: adoption.Reconcile},
		framework.FuncController{ControllerName: "systemd-unit", Every: 5 * time.Minute, PeriodicFunc: systemdUnits.Reconcile},
		framework.FuncController{ControllerName: "link", Every: 30 * time.Second, PeriodicFunc: link.Reconcile},
		framework.FuncController{ControllerName: "ipv4-static-address", PeriodicFunc: ipv4Static.Reconcile},
		framework.FuncController{ControllerName: "dhcpv6-information", Subs: statusSubscriptions("DHCPv6PrefixDelegation"), ReconcileFunc: func(ctx context.Context, event daemonapi.DaemonEvent) error {
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
		framework.FuncController{ControllerName: "lan-address", Subs: statusSubscriptions("DHCPv6PrefixDelegation", "Link", "Interface"), ReconcileFunc: func(ctx context.Context, _ daemonapi.DaemonEvent) error {
			for _, resource := range r.Router.Spec.Resources {
				if resource.Kind == "DHCPv6PrefixDelegation" {
					if err := lan.reconcile(ctx, resource.Metadata.Name); err != nil {
						return err
					}
				}
			}
			return nil
		}},
		framework.FuncController{ControllerName: "dslite", Subs: statusSubscriptions("DHCPv6Information", "IPv6DelegatedAddress", "DNSResolver"), PeriodicFunc: dslite.reconcile},
		framework.FuncController{ControllerName: "ipv4-route", Subs: statusSubscriptions("DSLiteTunnel", "EgressRoutePolicy"), PeriodicFunc: route.reconcile},
		framework.FuncController{ControllerName: "ipv6-ra", Subs: statusSubscriptions("IPv6DelegatedAddress", "DHCPv6Information"), PeriodicFunc: ra.reconcile},
		framework.FuncController{ControllerName: "dhcpv6-server", Subs: statusSubscriptions("IPv6DelegatedAddress", "DHCPv6Information"), PeriodicFunc: dhcpv6.reconcile},
		framework.FuncController{ControllerName: "dhcpv4-lease", Subs: []bus.Subscription{{Topics: []string{"routerd.dhcpv4.client.**"}}}, ReconcileFunc: func(ctx context.Context, _ daemonapi.DaemonEvent) error {
			for _, resource := range r.Router.Spec.Resources {
				if resource.Kind == "DHCPv4Lease" {
					if err := dhcp4Lease.Reconcile(ctx, resource.Metadata.Name); err != nil {
						return err
					}
				}
			}
			return nil
		}},
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
		framework.FuncController{ControllerName: "dns-resolver", Subs: []bus.Subscription{{Topics: []string{"routerd.resource.status.changed", "routerd.dhcp.lease.**"}}}, ReconcileFunc: dnsResolver.HandleEvent, PeriodicFunc: dnsResolver.Reconcile},
		framework.FuncController{ControllerName: "egress-route-policy", Subs: statusSubscriptions("HealthCheck", "DSLiteTunnel", "Interface", "DHCPv4Lease", "PPPoESession"), PeriodicFunc: wan.Reconcile},
		framework.FuncController{ControllerName: "nat44", Subs: statusSubscriptions("EgressRoutePolicy"), PeriodicFunc: nat.Reconcile},
	}
	if !r.Opts.FirewallDisabled {
		controllers = append(controllers, framework.FuncController{ControllerName: "firewall", Subs: []bus.Subscription{{Topics: []string{"routerd.resource.status.changed", "routerd.firewall.**"}}}, PeriodicFunc: firewall.Reconcile})
	}
	r.warmDaemonStatuses(ctx, daemonStatusSync, logger)
	go func() {
		loop := framework.Runner{Bus: r.Bus, Logger: logger, Interval: 30 * time.Second}
		if err := loop.Run(ctx, controllers...); err != nil && ctx.Err() == nil {
			logger.Warn("controller event loop stopped", "error", err)
		}
	}()
	return nil
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
			args := []string{"daemon", "--resource", resource.Metadata.Name, "--interface", ifname}
			if spec.IAID != "" {
				args = append(args, "--iaid", spec.IAID)
			}
			r.startSupervisedDaemon(ctx, logger, resource.Metadata.Name, "routerd-dhcpv6-client", args)
		case "DHCPv4Lease":
			spec, err := resource.DHCPv4LeaseSpec()
			if err != nil {
				continue
			}
			ifname := interfaceIfName(r.Router, spec.Interface)
			if ifname == "" {
				ifname = spec.Interface
			}
			args := []string{"daemon", "--resource", resource.Metadata.Name, "--interface", ifname}
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
	switch binary {
	case "routerd-dhcpv6-client":
		return filepath.Join("/run/routerd/dhcpv6-client", resource+".sock")
	case "routerd-dhcpv4-client":
		return filepath.Join("/run/routerd/dhcpv4-client", resource+".sock")
	case "routerd-pppoe-client":
		return filepath.Join("/run/routerd/pppoe-client", resource+".sock")
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
	Router  *api.Router
	Bus     *bus.Bus
	Store   Store
	DryRun  bool
	Logger  *slog.Logger
	Command commandFunc
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
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "Link" {
			continue
		}
		spec, err := resource.LinkSpec()
		if err != nil {
			return err
		}
		ifname := spec.IfName
		if ifname == "" {
			ifname = interfaceIfName(c.Router, resource.Metadata.Name)
		}
		status := map[string]any{"phase": "Down", "ifname": ifname}
		if ifname == "" {
			status["reason"] = "InterfaceMissing"
		} else if ifi, err := net.InterfaceByName(ifname); err == nil {
			status["index"] = ifi.Index
			status["flags"] = ifi.Flags.String()
			if ifi.Flags&net.FlagUp != 0 {
				status["phase"] = "Up"
			}
		} else {
			status["reason"] = "InterfaceNotFound"
			status["error"] = err.Error()
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "Link", resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	return nil
}

type IPv4StaticAddressController struct {
	Router  *api.Router
	Bus     *bus.Bus
	Store   Store
	DryRun  bool
	Logger  *slog.Logger
	Command commandFunc
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
				socket = filepath.Join("/run/routerd/dhcpv6-client", resource.Metadata.Name+".sock")
			}
			add(socket)
		case "DHCPv4Lease":
			socket := c.DaemonSockets[resource.Metadata.Name]
			if socket == "" {
				socket = filepath.Join("/run/routerd/dhcpv4-client", resource.Metadata.Name+".sock")
			}
			add(socket)
		case "HealthCheck":
			spec, err := resource.HealthCheckSpec()
			if err != nil || spec.SocketSource == "embedded" || (spec.Daemon == "" && spec.SocketSource == "") {
				continue
			}
			socket := spec.SocketSource
			if socket == "" {
				socket = filepath.Join("/run/routerd/healthcheck", resource.Metadata.Name+".sock")
			}
			add(socket)
		case "PPPoESession":
			spec, err := resource.PPPoESessionSpec()
			if err != nil {
				continue
			}
			socket := spec.SocketSource
			if socket == "" {
				socket = c.DaemonSockets[resource.Metadata.Name]
			}
			if socket == "" {
				socket = filepath.Join("/run/routerd/pppoe-client", resource.Metadata.Name+".sock")
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
		if !c.DryRun {
			command := c.Command
			if command == nil {
				command = runCommandContext
			}
			if err := command(ctx, "ip", "-4", "addr", "replace", spec.Address, "dev", ifname); err != nil {
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
		status := map[string]any{
			"phase":     "Applied",
			"interface": spec.Interface,
			"ifname":    ifname,
			"address":   spec.Address,
			"dryRun":    c.DryRun,
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", resource.Metadata.Name, status); err != nil {
			return err
		}
		if c.Bus != nil {
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
		if !resourcequery.DependenciesReady(c.Store, spec.DependsOn) {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DependsOnFalse"})
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
			ifname := interfaceIfName(c.Router, spec.Interface)
			if ifname == "" {
				ifname = spec.Interface
			}
			command := c.Command
			if command == nil {
				command = runCommandContext
			}
			if err := command(ctx, "ip", "-6", "addr", "replace", addr, "dev", ifname); err != nil {
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
	ifname := interfaceIfName(c.Router, name)
	if ifname == "" {
		ifname = name
	}
	ifi, err := net.InterfaceByName(ifname)
	if err == nil && ifi.Flags&net.FlagUp != 0 {
		_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "Link", name, map[string]any{"phase": "Up", "ifname": ifname})
		return true
	}
	return false
}

func interfaceIfName(router *api.Router, name string) string {
	if router == nil {
		return name
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
		for _, server := range append(expandServers(store, spec.DNSServers), expandServerSources(store, spec.DNSServerFrom)...) {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option6:dns-server,[%s]", tag, dnsmasqIPv6Address(server)))
		}
		if len(spec.DomainSearch) > 0 {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option6:domain-search,%s", tag, strings.Join(spec.DomainSearch, ",")))
		}
		for _, server := range append(expandServers(store, spec.SNTPServers), expandServerSources(store, spec.SNTPServerFrom)...) {
			lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option6:sntp-server,[%s]", tag, dnsmasqIPv6Address(server)))
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
		for _, server := range append(expandServers(store, spec.RDNSS), expandServerSources(store, spec.RDNSSFrom)...) {
			lines = append(lines, fmt.Sprintf("dhcp-option=option6:dns-server,[%s]", dnsmasqIPv6Address(server)))
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

func expandServerSources(store Store, sources []api.StatusValueSourceSpec) []string {
	var out []string
	for _, source := range sources {
		out = append(out, resourcequery.Values(store, source)...)
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
	proc, alive := dnsmasqProcess(pidFile)
	if alive && changed {
		return proc.Signal(syscall.SIGHUP)
	}
	if alive {
		return nil
	}
	return startDnsmasq(ctx, command, configPath, pidFile)
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
