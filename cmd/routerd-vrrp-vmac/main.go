// SPDX-License-Identifier: BSD-3-Clause

// routerd-vrrp-vmac is invoked only by the single authoritative keepalived
// instance. It keeps the WAN macvlan present but down on a BACKUP node, and
// raises it only after that same instance enters MASTER.
package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/net/ipv6"
)

type options struct {
	parent string
	ifname string
	mac    string
	action string
	vmacs  []vmac
}

type vmac struct {
	parent, ifname, mac, linkLocal string
	withdraw                       bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	if opts.action == "withdraw-ra" {
		return withdrawRouterAdvertisement(opts.parent)
	}
	if opts.action == "deactivate" {
		for _, vmac := range opts.vmacs {
			if vmac.withdraw {
				if err := withdrawRouterAdvertisement(vmac.parent); err != nil {
					return err
				}
			}
		}
	}
	for _, command := range commandsFor(opts) {
		if output, err := exec.Command(command[0], command[1:]...).CombinedOutput(); err != nil {
			if opts.action == "activate" && len(command) > 2 && command[2] == "add" && strings.Contains(string(output), "File exists") {
				continue
			}
			if opts.action == "deactivate" && strings.Contains(string(output), "Cannot find device") {
				return nil
			}
			return fmt.Errorf("%s: %w: %s", strings.Join(command, " "), err, strings.TrimSpace(string(output)))
		}
	}
	if opts.action == "activate" {
		for _, vmac := range opts.vmacs {
			if vmac.withdraw {
				continue
			}
			if err := requestRouterAdvertisement(vmac.ifname); err != nil {
				return err
			}
		}
		// Linux installs RA routes per interface. The physical WAN's RA route
		// normally has a lower metric, which would send packets sourced from
		// the shared VMAC over the standby-specific physical MAC. Promote the
		// VMAC route after RA is learned; repeated MASTER reconciliation makes
		// this converge even when RA arrives after the link is raised.
		for _, vmac := range opts.vmacs {
			output, err := exec.Command("ip", "-6", "route", "show", "default", "dev", vmac.ifname).CombinedOutput()
			if err != nil {
				return fmt.Errorf("read IPv6 default route for %s: %w: %s", vmac.ifname, err, strings.TrimSpace(string(output)))
			}
			if command, ok := preferVMACDefaultCommand(string(output), vmac.ifname); ok {
				if output, err := exec.Command(command[0], command[1:]...).CombinedOutput(); err != nil {
					return fmt.Errorf("%s: %w: %s", strings.Join(command, " "), err, strings.TrimSpace(string(output)))
				}
			}
		}
	}
	return nil
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

func preferVMACDefaultCommand(routes, ifname string) ([]string, bool) {
	for _, line := range strings.Split(routes, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "default" {
			continue
		}
		var gateway string
		for i := 0; i+1 < len(fields); i++ {
			switch fields[i] {
			case "via":
				gateway = fields[i+1]
			case "dev":
				if fields[i+1] != ifname {
					gateway = ""
				}
			}
		}
		if gateway != "" {
			return []string{"ip", "-6", "route", "replace", "default", "via", gateway, "dev", ifname, "metric", "50"}, true
		}
	}
	return nil, false
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
	fs.Func("vmac", "parent,interface,mac[,link-local]", func(value string) error {
		parts := strings.Split(value, ",")
		if len(parts) < 3 || len(parts) > 5 {
			return errors.New("--vmac must be parent,interface,mac[,link-local[,withdraw-ra]]")
		}
		entry := vmac{parent: parts[0], ifname: parts[1], mac: parts[2]}
		if len(parts) == 4 {
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
	if opts.action != "activate" && opts.action != "deactivate" && opts.action != "withdraw-ra" {
		return options{}, errors.New("action must be activate, deactivate, or withdraw-ra")
	}
	if opts.action == "withdraw-ra" {
		if !validInterface(opts.parent) {
			return options{}, errors.New("--parent must be a Linux interface name")
		}
		return opts, nil
	}
	if len(opts.vmacs) == 0 {
		opts.vmacs = append(opts.vmacs, vmac{parent: opts.parent, ifname: opts.ifname, mac: opts.mac})
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

func commandsFor(opts options) [][]string {
	var commands [][]string
	for _, entry := range opts.vmacs {
		if opts.action == "deactivate" {
			commands = append(commands, []string{"ip", "link", "set", "dev", entry.ifname, "down"})
			continue
		}
		commands = append(commands, []string{"ip", "link", "add", "link", entry.parent, "name", entry.ifname, "type", "macvlan", "mode", "private"}, []string{"ip", "link", "set", "dev", entry.ifname, "address", entry.mac}, []string{"ip", "link", "set", "dev", entry.ifname, "up"}, []string{"sysctl", "-w", "net.ipv6.conf." + entry.ifname + ".accept_ra=2"})
		if entry.linkLocal != "" {
			commands = append(commands, []string{"ip", "-6", "addr", "replace", entry.linkLocal + "/64", "dev", entry.ifname})
		}
	}
	return commands
}
