// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/controlapi"
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
	fs := flag.NewFlagSet("routerd-dhcp-event-relay", flag.ContinueOnError)
	socket := fs.String("socket", defaults.SocketFile(), "explicit control socket: submit through the API instead of the local durable queue")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	event, err := eventFromArgs(fs.Args(), os.Environ())
	if err != nil {
		return err
	}
	if event.Action == "" || event.IP == "" {
		return fmt.Errorf("usage: routerd-dhcp-event-relay [add|old|del] MAC IP HOSTNAME")
	}
	observedAt := time.Now().UTC()
	explicitSocket := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "socket" {
			explicitSocket = true
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	if !explicitSocket {
		return dhcpeventqueue.Store(ctx, defaults.DHCPEventQueueFile(), event, observedAt)
	}
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", *socket)
	}}}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+controlapi.Prefix+"/dhcp-lease-event", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Close = true
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("routerd returned %s", resp.Status)
	}
	return nil
}

func eventFromArgs(args []string, env []string) (controlapi.DHCPLeaseEventRequest, error) {
	event := controlapi.DHCPLeaseEventRequest{
		TypeMeta: controlapi.TypeMeta{APIVersion: controlapi.APIVersion, Kind: "DHCPLeaseEvent"},
	}
	if len(args) > 0 {
		action, err := normalizeAction(args[0])
		if err != nil {
			return controlapi.DHCPLeaseEventRequest{}, err
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
