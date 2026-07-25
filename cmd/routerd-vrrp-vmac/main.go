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
	"strings"
)

type options struct {
	parent string
	ifname string
	mac    string
	action string
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
		// Linux installs RA routes per interface. The physical WAN's RA route
		// normally has a lower metric, which would send packets sourced from
		// the shared VMAC over the standby-specific physical MAC. Promote the
		// VMAC route after RA is learned; repeated MASTER reconciliation makes
		// this converge even when RA arrives after the link is raised.
		output, err := exec.Command("ip", "-6", "route", "show", "default", "dev", opts.ifname).CombinedOutput()
		if err != nil {
			return fmt.Errorf("read IPv6 default route for %s: %w: %s", opts.ifname, err, strings.TrimSpace(string(output)))
		}
		if command, ok := preferVMACDefaultCommand(string(output), opts.ifname); ok {
			if output, err := exec.Command(command[0], command[1:]...).CombinedOutput(); err != nil {
				return fmt.Errorf("%s: %w: %s", strings.Join(command, " "), err, strings.TrimSpace(string(output)))
			}
		}
	}
	return nil
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
	if err := fs.Parse(args[1:]); err != nil {
		return options{}, err
	}
	if opts.action != "activate" && opts.action != "deactivate" {
		return options{}, errors.New("action must be activate or deactivate")
	}
	if !validInterface(opts.parent) || !validInterface(opts.ifname) {
		return options{}, errors.New("parent and interface must be Linux interface names")
	}
	mac, err := net.ParseMAC(opts.mac)
	if err != nil || len(mac) != 6 {
		return options{}, errors.New("mac must be an Ethernet MAC address")
	}
	return opts, nil
}

func validInterface(value string) bool {
	return value != "" && len(value) <= 15 && !strings.ContainsAny(value, "/ \t\r\n")
}

func commandsFor(opts options) [][]string {
	if opts.action == "deactivate" {
		return [][]string{{"ip", "link", "set", "dev", opts.ifname, "down"}}
	}
	return [][]string{
		{"ip", "link", "add", "link", opts.parent, "name", opts.ifname, "type", "macvlan", "mode", "private"},
		{"ip", "link", "set", "dev", opts.ifname, "address", opts.mac},
		{"ip", "link", "set", "dev", opts.ifname, "up"},
		{"sysctl", "-w", "net.ipv6.conf." + opts.ifname + ".accept_ra=2"},
	}
}
