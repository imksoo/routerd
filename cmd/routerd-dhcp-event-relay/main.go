// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dhcpeventqueue"
	"github.com/imksoo/routerd/pkg/platform"
)

const (
	requestTimeout = 3 * time.Second
	hardTimeout    = 4 * time.Second
)

func main() {
	done := make(chan error, 1)
	go func() {
		done <- run(os.Args[1:])
	}()
	select {
	case err := <-done:
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case <-time.After(hardTimeout):
		fmt.Fprintln(os.Stderr, "dhcp-event-relay: hard timeout")
		os.Exit(2)
	}
}

func run(args []string) error {
	defaults, _ := platform.Current()
	event, err := eventFromArgs(args, os.Environ())
	if err != nil {
		return err
	}
	if event.Action == "" || event.IP == "" {
		return fmt.Errorf("usage: routerd-dhcp-event-relay [add|old|del] MAC IP HOSTNAME")
	}
	observedAt := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	return dhcpeventqueue.Store(ctx, defaults.DHCPEventQueueFile(), event, observedAt)
}

func eventFromArgs(args []string, env []string) (dhcpeventqueue.LeaseEvent, error) {
	event := dhcpeventqueue.LeaseEvent{}
	if len(args) > 0 {
		action, err := normalizeAction(args[0])
		if err != nil {
			return dhcpeventqueue.LeaseEvent{}, err
		}
		event.Action = action
	}
	if len(args) > 1 {
		event.MAC = args[1]
	}
	if len(args) > 2 {
		event.IP = args[2]
	}
	if len(args) > 3 && args[3] != "*" {
		event.Hostname = args[3]
	}
	event.Interface = dnsmasqInterface(env)
	return event, nil
}

func normalizeAction(action string) (string, error) {
	switch strings.TrimSpace(action) {
	case "add":
		return daemonapi.DHCPLeaseActionAdded, nil
	case "old":
		return daemonapi.DHCPLeaseActionRenewed, nil
	case "del":
		return daemonapi.DHCPLeaseActionRemoved, nil
	default:
		return "", fmt.Errorf("unsupported dnsmasq lease action %q", action)
	}
}

func dnsmasqInterface(env []string) string {
	for _, value := range env {
		if ifname, ok := strings.CutPrefix(value, "DNSMASQ_INTERFACE="); ok {
			return strings.TrimSpace(ifname)
		}
	}
	return ""
}
