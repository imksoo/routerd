// SPDX-License-Identifier: BSD-3-Clause

// routerd-vrrp-vmac is invoked only by the single authoritative keepalived
// instance. It keeps the WAN macvlan present but down on a BACKUP node, and
// raises it only after that same instance enters MASTER.
package main

import (
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/net/ipv6"
	"golang.org/x/sys/unix"
)

type options struct {
	parent            string
	ifname            string
	mac               string
	action            string
	resource          string
	guardResource     string
	deferredAddress   string
	deferredInterface string
	reconcile         bool
	vmacs             []vmac
}

type vmac struct {
	parent, ifname, mac, linkLocal string
	withdraw                       bool
}

type vmacRuntimeState struct {
	exists                 bool
	up                     bool
	macMatches             bool
	hasLinkLocal           bool
	configuredLinkLocalSet bool
}

const conntrackdRoleStatePath = "/run/routerd/vrrp-vmac-conntrackd-role"
const vrrpRuntimeDir = "/run/routerd"

type commandRunner func(name string, args ...string) ([]byte, error)

type runHooks struct {
	command                    commandRunner
	inspectVMAC                func(vmac) (vmacRuntimeState, error)
	withdrawRA                 func(string) error
	requestRA                  func(string) error
	conntrackdTransitionNeeded func(string) (bool, error)
	reconcileConntrackdRole    func(string) error
	writeConntrackdRole        func(string) error
	lockElection               func(string) (func() error, error)
	readElectionRole           func(string) (string, error)
	electionTransitionNeeded   func(string, string) (bool, error)
	writeElectionRole          func(string, string) error
	withdrawDeferredVIP        func(options) error
	notifyRouterd              func() error
	warn                       func(error)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return runWithHooks(args, productionRunHooks())
}

func productionRunHooks() runHooks {
	return runHooks{
		command:                    runCommand,
		inspectVMAC:                inspectVMACRuntimeState,
		withdrawRA:                 withdrawRouterAdvertisement,
		requestRA:                  requestRouterAdvertisement,
		conntrackdTransitionNeeded: conntrackdRoleTransitionNeeded,
		reconcileConntrackdRole:    reconcileConntrackdRole,
		writeConntrackdRole:        writeConntrackdRole,
		lockElection:               lockElection,
		readElectionRole:           readElectionRole,
		electionTransitionNeeded:   electionRoleTransitionNeeded,
		writeElectionRole:          writeElectionRole,
		withdrawDeferredVIP: func(opts options) error {
			return withdrawDeferredVIP(opts, runCommand)
		},
		notifyRouterd: func() error {
			return notifyRouterdVRRPTransition(runCommand)
		},
		warn: func(err error) {
			fmt.Fprintln(os.Stderr, err)
		},
	}
}

func runCommand(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func inspectVMACRuntimeState(entry vmac) (vmacRuntimeState, error) {
	ifi, err := net.InterfaceByName(entry.ifname)
	if err != nil {
		message := strings.ToLower(err.Error())
		if errors.Is(err, os.ErrNotExist) || strings.Contains(message, "no such network interface") {
			return vmacRuntimeState{}, nil
		}
		return vmacRuntimeState{}, err
	}
	state := vmacRuntimeState{
		exists:     true,
		up:         ifi.Flags&net.FlagUp != 0,
		macMatches: strings.EqualFold(ifi.HardwareAddr.String(), entry.mac),
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return vmacRuntimeState{}, err
	}
	wantLinkLocal := net.ParseIP(entry.linkLocal)
	for _, addr := range addrs {
		ip, _, parseErr := net.ParseCIDR(addr.String())
		if parseErr != nil || ip.To4() != nil || !ip.IsLinkLocalUnicast() {
			continue
		}
		state.hasLinkLocal = true
		if wantLinkLocal != nil && ip.Equal(wantLinkLocal) {
			state.configuredLinkLocalSet = true
		}
	}
	return state, nil
}

func vmacStateReadyForAction(action string, entry vmac, state vmacRuntimeState) bool {
	if !state.exists || !state.macMatches {
		return false
	}
	switch action {
	case "activate":
		if !state.up {
			return false
		}
		if entry.linkLocal != "" {
			return state.configuredLinkLocalSet
		}
		// Router Solicitation requires a usable link-local source. Treat a WAN
		// VMAC without one as incomplete so the slow path can raise it again.
		return state.hasLinkLocal
	case "deactivate":
		if state.up {
			return false
		}
		return entry.linkLocal == "" || !state.hasLinkLocal
	default:
		return false
	}
}

func runWithHooks(args []string, hooks runHooks) (runErr error) {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	guardResource := opts.resource
	if guardResource == "" {
		guardResource = opts.guardResource
	}
	if guardResource != "" && hooks.lockElection != nil {
		unlock, lockErr := hooks.lockElection(guardResource)
		if lockErr != nil {
			return fmt.Errorf("lock VRRP election for %s: %w", guardResource, lockErr)
		}
		defer func() {
			if unlockErr := unlock(); unlockErr != nil {
				runErr = errors.Join(runErr, fmt.Errorf("unlock VRRP election for %s: %w", guardResource, unlockErr))
			}
		}()
	}
	if opts.reconcile && guardResource != "" && hooks.readElectionRole != nil {
		role, readErr := hooks.readElectionRole(guardResource)
		if readErr != nil {
			return readErr
		}
		// The controller may have observed a role immediately before a
		// keepalived notification acquired the transition lock. Re-read the
		// durable marker after acquiring that lock and discard the stale repair
		// rather than undoing the completed MASTER/BACKUP transition.
		if want := conntrackdRoleForAction(opts.action); role != "" && role != want {
			return nil
		}
	}
	conntrackdTransition, err := hooks.conntrackdTransitionNeeded(opts.action)
	if err != nil {
		return err
	}
	electionTransition := false
	if opts.resource != "" {
		electionTransition, err = hooks.electionTransitionNeeded(opts.resource, opts.action)
		if err != nil {
			return err
		}
	}
	notified := false
	if opts.action == "deactivate" && opts.resource != "" {
		// Withdraw the client-facing VIP before RA, VMAC and conntrack role
		// changes. This closes the window in which clients can select a node
		// whose DS-Lite data path is already being dismantled.
		withdrawErr := hooks.withdrawDeferredVIP(opts)
		// Always refresh the BACKUP marker and wake routerd. If the immediate
		// delete failed, the controller must see BACKUP and retry withdrawal;
		// conntrack remains primary until that retry can complete.
		if err := hooks.writeElectionRole(opts.resource, opts.action); err != nil {
			return err
		}
		if hooks.notifyRouterd != nil && (electionTransition || !opts.reconcile) {
			if err := hooks.notifyRouterd(); err != nil && hooks.warn != nil {
				hooks.warn(fmt.Errorf("notify routerd of VRRP transition: %w", err))
			}
			notified = true
		}
		if withdrawErr != nil {
			return withdrawErr
		}
	}
	ready := make([]bool, len(opts.vmacs))
	if hooks.inspectVMAC != nil {
		for index, entry := range opts.vmacs {
			state, inspectErr := hooks.inspectVMAC(entry)
			if inspectErr != nil {
				return fmt.Errorf("inspect VMAC %s: %w", entry.ifname, inspectErr)
			}
			ready[index] = vmacStateReadyForAction(opts.action, entry, state)
		}
	}
	if opts.action == "deactivate" {
		for index, vmac := range opts.vmacs {
			if vmac.withdraw && !ready[index] {
				if err := hooks.withdrawRA(vmac.parent); err != nil && !isMissingRAInterface(err) {
					return fmt.Errorf("withdraw router advertisement on %s: %w", vmac.parent, err)
				}
			}
		}
	}
	// A promoted router must install the replica's external cache before it
	// advertises the WAN VMAC or solicits an RA.  Otherwise return traffic can
	// reach the newly elected router during the VMAC/RA work and be classified
	// as INVALID before the replicated NAT state exists in the kernel.
	if opts.action == "activate" && conntrackdTransition {
		if err := hooks.reconcileConntrackdRole(opts.action); err != nil {
			return err
		}
	}
	for index, entry := range opts.vmacs {
		single := opts
		single.vmacs = []vmac{entry}
		commands := commandsFor(single)
		if ready[index] {
			commands = steadyStateCommands(entry)
		}
		if err := runVMACCommands(opts.action, commands, hooks.command); err != nil {
			return err
		}
	}
	if opts.action == "activate" {
		for index, vmac := range opts.vmacs {
			if vmac.withdraw {
				continue
			}
			output, err := hooks.command("ip", "-6", "route", "show", "default", "dev", vmac.ifname)
			if err != nil {
				return fmt.Errorf("read IPv6 default route for %s: %w: %s", vmac.ifname, err, strings.TrimSpace(string(output)))
			}
			addressOutput, _ := hooks.command("ip", "-6", "-o", "addr", "show", "dev", vmac.ifname, "scope", "global")
			source := preferredVMACGlobalAddress(string(addressOutput))
			// A healthy MASTER already has both an eligible SLAAC address and an
			// RA default route. Re-soliciting on every reconcile needlessly resets
			// neighbour/router state and, combined with repeated link updates,
			// makes systemd-networkd re-acquire IPv6LL continuously. Keep RS as a
			// repair path only when the link was changed or RA state is incomplete.
			if !ready[index] || source == "" || !hasVMACDefaultRoute(string(output), vmac.ifname) {
				if err := hooks.requestRA(vmac.ifname); err != nil {
					return err
				}
			}
			// Linux installs RA routes per interface. The physical WAN's RA route
			// normally has a lower metric, which would send packets sourced from
			// the shared VMAC over the standby-specific physical MAC. Promote the
			// VMAC route after RA is learned; repeated MASTER reconciliation makes
			// this converge even when RA arrives after the link is raised.
			if command, ok := preferVMACDefaultCommand(string(output), vmac.ifname, source); ok {
				if output, err := hooks.command(command[0], command[1:]...); err != nil {
					return fmt.Errorf("%s: %w: %s", strings.Join(command, " "), err, strings.TrimSpace(string(output)))
				}
			}
		}
	}
	if opts.action != "activate" && conntrackdTransition {
		if err := hooks.reconcileConntrackdRole(opts.action); err != nil {
			return err
		}
	}
	if opts.action == "activate" && electionTransition {
		// Election ownership becomes visible to routerd only after conntrack
		// state, VMAC identity and the initial RA solicitation are complete.
		// routerd can then reconcile PD-derived addresses and DS-Lite while the
		// deferred LAN VIP is still absent.
		if err := hooks.writeElectionRole(opts.resource, opts.action); err != nil {
			return err
		}
	}
	if conntrackdTransition {
		if err := hooks.writeConntrackdRole(opts.action); err != nil {
			return err
		}
	}
	if (conntrackdTransition || electionTransition) && !notified {
		if hooks.notifyRouterd != nil {
			if err := hooks.notifyRouterd(); err != nil && hooks.warn != nil {
				hooks.warn(fmt.Errorf("notify routerd of VRRP transition: %w", err))
			}
		}
	}
	return nil
}

// notifyRouterdVRRPTransition is a best-effort fast path. routerd retains its
// periodic kernel role observation, so a missing or restarting service cannot
// make keepalived's transition fail.
func notifyRouterdVRRPTransition(command commandRunner) error {
	output, err := command("systemctl", "kill", "--kill-whom=main", "--signal=USR1", "routerd.service")
	if err != nil {
		return fmt.Errorf("systemctl kill routerd.service: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func runVMACCommands(action string, commands [][]string, command commandRunner) error {
	for _, args := range commands {
		output, err := command(args[0], args[1:]...)
		if err == nil {
			continue
		}
		if len(args) > 2 && args[0] == "ip" && args[1] == "link" && args[2] == "add" && strings.Contains(string(output), "File exists") {
			continue
		}
		if action == "deactivate" && isIPRouteMissingDevice(args, output) {
			// A missing parent or child is local to this VMAC. Keep processing
			// remaining VMACs, then complete the conntrackd BACKUP transition.
			return nil
		}
		return fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func isMissingRAInterface(err error) bool {
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such network interface")
}

func isIPRouteMissingDevice(args []string, output []byte) bool {
	return len(args) > 0 && args[0] == "ip" && strings.Contains(strings.ToLower(string(output)), "cannot find device")
}

func conntrackdRoleTransitionNeeded(action string) (bool, error) {
	want := conntrackdRoleForAction(action)
	if want == "" {
		return false, nil
	}
	data, err := os.ReadFile(conntrackdRoleStatePath)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(data)) != want, nil
}

func conntrackdRoleForAction(action string) string {
	switch action {
	case "activate":
		return "master"
	case "deactivate":
		return "backup"
	default:
		return ""
	}
}

func writeConntrackdRole(action string) error {
	role := conntrackdRoleForAction(action)
	if role == "" {
		return nil
	}
	if err := os.MkdirAll("/run/routerd", 0755); err != nil {
		return err
	}
	temporary := conntrackdRoleStatePath + ".tmp"
	if err := os.WriteFile(temporary, []byte(role+"\n"), 0644); err != nil {
		return err
	}
	return os.Rename(temporary, conntrackdRoleStatePath)
}

func electionStatePath(resource string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(resource)))
	return fmt.Sprintf("%s/vrrp-election-%x.role", vrrpRuntimeDir, sum[:8])
}

func electionLockPath(resource string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(resource)))
	return fmt.Sprintf("%s/vrrp-election-%x.lock", vrrpRuntimeDir, sum[:8])
}

func lockElection(resource string) (func() error, error) {
	if err := os.MkdirAll(vrrpRuntimeDir, 0755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(electionLockPath(resource), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() error {
		return errors.Join(unix.Flock(int(file.Fd()), unix.LOCK_UN), file.Close())
	}, nil
}

func readElectionRole(resource string) (string, error) {
	data, err := os.ReadFile(electionStatePath(resource))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	role := strings.TrimSpace(string(data))
	if role != "master" && role != "backup" {
		return "", nil
	}
	return role, nil
}

func electionRoleTransitionNeeded(resource, action string) (bool, error) {
	want := conntrackdRoleForAction(action)
	if want == "" || strings.TrimSpace(resource) == "" {
		return false, nil
	}
	role, err := readElectionRole(resource)
	if err != nil {
		return false, err
	}
	return role != want, nil
}

func writeElectionRole(resource, action string) error {
	role := conntrackdRoleForAction(action)
	if role == "" || strings.TrimSpace(resource) == "" {
		return nil
	}
	if err := os.MkdirAll(vrrpRuntimeDir, 0755); err != nil {
		return err
	}
	path := electionStatePath(resource)
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(role+"\n"), 0644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func withdrawDeferredVIP(opts options, command commandRunner) error {
	if opts.resource == "" {
		return nil
	}
	output, err := command("ip", "addr", "del", opts.deferredAddress, "dev", opts.deferredInterface)
	if err == nil {
		return nil
	}
	message := strings.ToLower(string(output))
	if strings.Contains(message, "cannot assign requested address") || strings.Contains(message, "not found") {
		return nil
	}
	return fmt.Errorf("ip addr del %s dev %s: %w: %s", opts.deferredAddress, opts.deferredInterface, err, strings.TrimSpace(string(output)))
}

// reconcileConntrackdRole follows conntrackd's documented primary/backup
// sequence. State stays in the BACKUP external cache and is committed only
// when this node takes ownership, avoiding stale-kernel-entry clashes during
// a second hand-over.
func reconcileConntrackdRole(action string) error {
	const binary = "/usr/sbin/conntrackd"
	const config = "/etc/conntrackd/routerd-dslite-sessions.conf"
	if _, err := os.Stat(binary); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if _, err := os.Stat(config); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	commands := conntrackdRoleCommands(action)
	for _, args := range commands {
		args = append([]string{"-C", config}, args...)
		if out, err := exec.Command(binary, args...).CombinedOutput(); err != nil {
			return fmt.Errorf("conntrackd %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func conntrackdRoleCommands(action string) [][]string {
	switch action {
	case "activate":
		// -f without a cache selector flushes both caches.  Flushing the
		// external cache immediately after -c discards the replica state that
		// must remain available for a subsequent transition.  Commit it, then
		// publish the local kernel state without destroying either cache.
		return [][]string{{"-c"}, {"-R"}, {"-B"}}
	case "deactivate":
		return [][]string{{"-t"}, {"-n"}}
	default:
		return nil
	}
}

// requestRouterAdvertisement solicits an immediate RA after raising the WAN
// VMAC. A BACKUP VMAC is intentionally down, so it cannot rely on a periodic
// RA having arrived before MASTER transition.
func requestRouterAdvertisement(ifname string) error {
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return err
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return err
	}
	var source net.IP
	for _, addr := range addrs {
		ip, _, parseErr := net.ParseCIDR(addr.String())
		if parseErr == nil && ip.IsLinkLocalUnicast() && ip.To4() == nil {
			source = ip
			break
		}
	}
	if source == nil {
		return fmt.Errorf("no IPv6 link-local address on %s", ifname)
	}
	conn, err := net.ListenIP("ip6:ipv6-icmp", &net.IPAddr{IP: source, Zone: ifname})
	if err != nil {
		return err
	}
	defer conn.Close()
	packet := ipv6.NewPacketConn(conn)
	if err := packet.SetControlMessage(ipv6.FlagInterface|ipv6.FlagHopLimit, true); err != nil {
		return err
	}
	// ICMPv6 Router Solicitation: type, code, checksum, reserved.
	payload := []byte{133, 0, 0, 0, 0, 0, 0, 0}
	_, err = packet.WriteTo(payload, &ipv6.ControlMessage{IfIndex: ifi.Index, HopLimit: 255}, &net.IPAddr{IP: net.ParseIP("ff02::2"), Zone: ifname})
	return err
}

func preferVMACDefaultCommand(routes, ifname, source string) ([]string, bool) {
	if strings.TrimSpace(source) == "" {
		return nil, false
	}
	for _, line := range strings.Split(routes, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "default" {
			continue
		}
		var gateway, device, metric, routeSource string
		for i := 0; i+1 < len(fields); i++ {
			switch fields[i] {
			case "via":
				gateway = fields[i+1]
			case "dev":
				device = fields[i+1]
			case "metric":
				metric = fields[i+1]
			case "src":
				routeSource = fields[i+1]
			}
		}
		if gateway != "" && device == ifname {
			if metric == "50" && routeSource == source {
				return nil, false
			}
			command := []string{"ip", "-6", "route", "replace", "default", "via", gateway, "dev", ifname, "metric", "50", "src", source}
			return command, true
		}
	}
	return nil, false
}

func hasVMACDefaultRoute(routes, ifname string) bool {
	for _, line := range strings.Split(routes, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "default" {
			continue
		}
		for index := 0; index+1 < len(fields); index++ {
			if fields[index] == "dev" && fields[index+1] == ifname {
				return true
			}
		}
	}
	return false
}

func preferredVMACGlobalAddress(output string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		ineligible := false
		for _, field := range fields {
			if field == "tentative" || field == "deprecated" || field == "dadfailed" {
				ineligible = true
				break
			}
		}
		if ineligible {
			continue
		}
		for i, field := range fields {
			if field != "inet6" || i+1 >= len(fields) {
				continue
			}
			prefix, err := netip.ParsePrefix(fields[i+1])
			if err == nil && prefix.Addr().Is6() && !prefix.Addr().IsLinkLocalUnicast() && prefix.Bits() < 128 {
				return prefix.Addr().String()
			}
		}
	}
	return ""
}

func parseOptions(args []string) (options, error) {
	if len(args) == 0 {
		return options{}, errors.New("action is required: activate or deactivate")
	}
	opts := options{action: args[0]}
	fs := flag.NewFlagSet("routerd-vrrp-vmac", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&opts.parent, "parent", "", "physical parent interface")
	fs.StringVar(&opts.ifname, "interface", "", "VMAC interface")
	fs.StringVar(&opts.mac, "mac", "", "VMAC address")
	fs.StringVar(&opts.resource, "resource", "", "VirtualAddress resource name")
	fs.StringVar(&opts.guardResource, "guard-resource", "", "VirtualAddress election role guarding an idempotent VMAC repair")
	fs.StringVar(&opts.deferredAddress, "deferred-address", "", "VIP managed after readiness")
	fs.StringVar(&opts.deferredInterface, "deferred-interface", "", "interface for the deferred VIP")
	fs.BoolVar(&opts.reconcile, "reconcile", false, "idempotent routerd reconciliation rather than a keepalived notification")
	fs.Func("vmac", "parent,interface,mac[,link-local]", func(value string) error {
		parts := strings.Split(value, ",")
		if len(parts) < 3 || len(parts) > 5 {
			return errors.New("--vmac must be parent,interface,mac[,link-local[,withdraw-ra]]")
		}
		entry := vmac{parent: parts[0], ifname: parts[1], mac: parts[2]}
		if len(parts) >= 4 {
			entry.linkLocal = parts[3]
		}
		if len(parts) == 5 {
			value, err := strconv.ParseBool(parts[4])
			if err != nil {
				return err
			}
			entry.withdraw = value
		}
		opts.vmacs = append(opts.vmacs, entry)
		return nil
	})
	if err := fs.Parse(args[1:]); err != nil {
		return options{}, err
	}
	if opts.action != "activate" && opts.action != "deactivate" {
		return options{}, errors.New("action must be activate or deactivate")
	}
	if len(opts.vmacs) == 0 && (opts.parent != "" || opts.ifname != "" || opts.mac != "") {
		opts.vmacs = append(opts.vmacs, vmac{parent: opts.parent, ifname: opts.ifname, mac: opts.mac})
	}
	if len(opts.vmacs) == 0 && opts.resource == "" {
		return options{}, errors.New("a VMAC or graceful VirtualAddress resource is required")
	}
	if opts.resource != "" {
		if strings.ContainsAny(opts.resource, "\x00\r\n") {
			return options{}, errors.New("resource must not contain control characters")
		}
		prefix, err := netip.ParsePrefix(opts.deferredAddress)
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
			return options{}, errors.New("deferred-address must be an IPv4 /32 prefix")
		}
		if !validInterface(opts.deferredInterface) {
			return options{}, errors.New("deferred-interface must be a Linux interface name")
		}
	}
	if opts.guardResource != "" {
		if strings.ContainsAny(opts.guardResource, "\x00\r\n") {
			return options{}, errors.New("guard-resource must not contain control characters")
		}
		if !opts.reconcile {
			return options{}, errors.New("guard-resource requires --reconcile")
		}
		if opts.resource != "" && opts.resource != opts.guardResource {
			return options{}, errors.New("resource and guard-resource must match when both are set")
		}
	}
	for _, entry := range opts.vmacs {
		if !validInterface(entry.parent) || !validInterface(entry.ifname) {
			return options{}, errors.New("parent and interface must be Linux interface names")
		}
		mac, err := net.ParseMAC(entry.mac)
		if err != nil || len(mac) != 6 {
			return options{}, errors.New("mac must be an Ethernet MAC address")
		}
		if entry.linkLocal != "" {
			ip := net.ParseIP(entry.linkLocal)
			if ip == nil || !ip.IsLinkLocalUnicast() {
				return options{}, errors.New("link-local must be an IPv6 link-local address")
			}
		}
	}
	return opts, nil
}

// withdrawRouterAdvertisement advertises router lifetime zero from the
// former physical LAN source. This removes a pre-VMAC default route from
// existing clients while preserving their delegated addresses.
func withdrawRouterAdvertisement(ifname string) error {
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return err
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return err
	}
	var source net.IP
	for _, addr := range addrs {
		ip, _, parseErr := net.ParseCIDR(addr.String())
		if parseErr == nil && ip.IsLinkLocalUnicast() && ip.To4() == nil {
			source = ip
			break
		}
	}
	if source == nil {
		// A LAN without a link-local source has no RA to withdraw.  This must
		// not prevent the following VMAC links from being brought down during
		// a role transition.
		return nil
	}
	conn, err := net.ListenIP("ip6:ipv6-icmp", &net.IPAddr{IP: source, Zone: ifname})
	if err != nil {
		return err
	}
	defer conn.Close()
	packet := ipv6.NewPacketConn(conn)
	if err := packet.SetControlMessage(ipv6.FlagInterface|ipv6.FlagHopLimit, true); err != nil {
		return err
	}
	// ICMPv6 Router Advertisement: type, code, checksum, current hop limit,
	// flags, router lifetime=0, reachable time, retrans timer.
	payload := []byte{134, 0, 0, 0, 64, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	_, err = packet.WriteTo(payload, &ipv6.ControlMessage{IfIndex: ifi.Index, HopLimit: 255}, &net.IPAddr{IP: net.ParseIP("ff02::1"), Zone: ifname})
	return err
}

func validInterface(value string) bool {
	return value != "" && len(value) <= 15 && !strings.ContainsAny(value, "/ \t\r\n")
}

// steadyStateCommands repairs policy knobs without touching the link identity.
// In particular, setting an unchanged MAC address on an UP macvlan still emits
// a link-change notification and makes IPv6 regenerate its link-local address.
// Structural commands therefore remain reserved for a missing or mismatched
// runtime state.
func steadyStateCommands(entry vmac) [][]string {
	commands := [][]string{
		{"sysctl", "-w", "net.ipv4.conf." + entry.parent + ".arp_ignore=1"},
		{"sysctl", "-w", "net.ipv4.conf." + entry.ifname + ".arp_ignore=1"},
	}
	if entry.withdraw {
		commands = append(commands,
			[]string{"sysctl", "-w", "net.ipv6.conf." + entry.parent + ".accept_ra=0"},
			[]string{"sysctl", "-w", "net.ipv6.conf." + entry.ifname + ".accept_ra=0"},
		)
	} else {
		commands = append(commands, []string{"sysctl", "-w", "net.ipv6.conf." + entry.ifname + ".accept_ra=2"})
	}
	return append(commands, []string{"sysctl", "-w", "net.ipv6.conf." + entry.ifname + ".keep_addr_on_down=1"})
}

func commandsFor(opts options) [][]string {
	var commands [][]string
	for _, entry := range opts.vmacs {
		if opts.action == "deactivate" {
			commands = append(commands, []string{"ip", "link", "add", "link", entry.parent, "name", entry.ifname, "type", "macvlan", "mode", "private"}, []string{"ip", "link", "set", "dev", entry.ifname, "address", entry.mac})
			// A LAN VMAC owns the client router identity.  Clear its shared or
			// automatically generated link-local identity before it goes DOWN,
			// while retaining the PD-derived global address for the next MASTER
			// reconciliation.
			// A DOWN interface otherwise drops static delegated IPv6 addresses on
			// Linux.  WAN and LAN VMACs both retain those addresses across a role
			// transition.
			if entry.linkLocal != "" {
				commands = append(commands, []string{"ip", "link", "set", "dev", entry.ifname, "addrgenmode", "none"})
			}
			// Linux otherwise answers requests received on the physical parent for
			// IPv4 addresses owned by its macvlan child.  Apply the restriction to
			// both sides so the shared VMAC remains the only advertised L2 identity.
			commands = append(commands,
				[]string{"sysctl", "-w", "net.ipv4.conf." + entry.parent + ".arp_ignore=1"},
				[]string{"sysctl", "-w", "net.ipv4.conf." + entry.ifname + ".arp_ignore=1"},
			)
			if entry.withdraw {
				// A LAN parent or LAN VMAC must never learn an upstream RA.  Leaving
				// either enabled can create a second dnsmasq RA source.
				commands = append(commands,
					[]string{"sysctl", "-w", "net.ipv6.conf." + entry.parent + ".accept_ra=0"},
					[]string{"sysctl", "-w", "net.ipv6.conf." + entry.ifname + ".accept_ra=0"},
				)
			} else {
				commands = append(commands, []string{"sysctl", "-w", "net.ipv6.conf." + entry.ifname + ".accept_ra=2"})
			}
			if entry.linkLocal != "" {
				commands = append(commands, []string{"ip", "-6", "addr", "flush", "dev", entry.ifname, "scope", "link"})
			}
			commands = append(commands, []string{"sysctl", "-w", "net.ipv6.conf." + entry.ifname + ".keep_addr_on_down=1"})
			commands = append(commands, []string{"ip", "link", "set", "dev", entry.ifname, "down"})
			continue
		}
		commands = append(commands, []string{"ip", "link", "add", "link", entry.parent, "name", entry.ifname, "type", "macvlan", "mode", "private"}, []string{"ip", "link", "set", "dev", entry.ifname, "address", entry.mac})
		if entry.linkLocal != "" {
			commands = append(commands, []string{"ip", "link", "set", "dev", entry.ifname, "addrgenmode", "none"})
		}
		commands = append(commands,
			[]string{"sysctl", "-w", "net.ipv4.conf." + entry.parent + ".arp_ignore=1"},
			[]string{"sysctl", "-w", "net.ipv4.conf." + entry.ifname + ".arp_ignore=1"},
		)
		if entry.withdraw {
			commands = append(commands,
				[]string{"sysctl", "-w", "net.ipv6.conf." + entry.parent + ".accept_ra=0"},
				[]string{"sysctl", "-w", "net.ipv6.conf." + entry.ifname + ".accept_ra=0"},
			)
		} else {
			commands = append(commands, []string{"sysctl", "-w", "net.ipv6.conf." + entry.ifname + ".accept_ra=2"})
		}
		commands = append(commands, []string{"sysctl", "-w", "net.ipv6.conf." + entry.ifname + ".keep_addr_on_down=1"})
		if entry.withdraw {
			// Remove only kernel-learned SLAAC/temporary addresses.  Delegated
			// addresses installed by routerd are permanent and deliberately stay
			// staged across the role transition.
			commands = append(commands,
				[]string{"ip", "-6", "addr", "flush", "dev", entry.ifname, "scope", "global", "dynamic"},
				[]string{"ip", "-6", "addr", "flush", "dev", entry.parent, "scope", "global", "dynamic"},
			)
		}
		commands = append(commands, []string{"ip", "link", "set", "dev", entry.ifname, "up"})
		if entry.linkLocal != "" {
			// Reconciliation may run while delegated global addresses are already
			// installed on the LAN VMAC.  Only clear automatic or stale
			// link-local identities; delegated global addresses are owned by the
			// PD-derived address controller and must survive this operation.
			commands = append(commands, []string{"ip", "-6", "addr", "flush", "dev", entry.ifname, "scope", "link"}, []string{"ip", "-6", "addr", "replace", entry.linkLocal + "/64", "dev", entry.ifname, "nodad"})
		}
	}
	return commands
}

// commandsForVMACs keeps each VMAC's commands as one failure boundary. A
// missing parent on a BACKUP must not skip the remaining VMACs or the
// conntrackd role transition.
func commandsForVMACs(opts options) [][][]string {
	commands := make([][][]string, 0, len(opts.vmacs))
	for _, entry := range opts.vmacs {
		single := opts
		single.vmacs = []vmac{entry}
		commands = append(commands, commandsFor(single))
	}
	return commands
}
