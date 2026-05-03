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

	"routerd/pkg/controlapi"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	socket := flag.String("socket", "/run/routerd/routerd.sock", "routerd control socket")
	fs := flag.CommandLine
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	event := eventFromArgs(fs.Args(), os.Environ())
	if event.Action == "" || event.IP == "" {
		return fmt.Errorf("usage: routerd-dhcp-event-relay [add|old|del] MAC IP HOSTNAME")
	}
	body, _ := json.Marshal(event)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", *socket)
	}}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+controlapi.Prefix+"/dhcp-lease-event", bytes.NewReader(body))
	if err != nil {
		return err
	}
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

func eventFromArgs(args []string, env []string) controlapi.DHCPLeaseEventRequest {
	event := controlapi.DHCPLeaseEventRequest{
		TypeMeta: controlapi.TypeMeta{APIVersion: controlapi.APIVersion, Kind: "DHCPLeaseEvent"},
		Env:      map[string]string{},
	}
	for _, pair := range env {
		if k, v, ok := strings.Cut(pair, "="); ok {
			event.Env[k] = v
		}
	}
	if len(args) > 0 {
		event.Action = normalizeAction(args[0])
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
	return event
}

func normalizeAction(action string) string {
	switch action {
	case "add":
		return "added"
	case "old":
		return "renewed"
	case "del":
		return "removed"
	default:
		return action
	}
}
