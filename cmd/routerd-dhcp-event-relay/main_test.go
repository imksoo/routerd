// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestExitCodeWithHardTimeout(t *testing.T) {
	block := make(chan struct{})
	var stderr bytes.Buffer
	code := exitCodeWithHardTimeout(func() error {
		<-block
		return nil
	}, 10*time.Millisecond, &stderr)
	close(block)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "dhcp-event-relay: hard timeout") {
		t.Fatalf("stderr missing hard timeout: %q", stderr.String())
	}
}

func TestExitCodeWithHardTimeoutReportsRunError(t *testing.T) {
	var stderr bytes.Buffer
	code := exitCodeWithHardTimeout(func() error {
		return errors.New("boom")
	}, time.Second, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("stderr missing run error: %q", stderr.String())
	}
}

func TestExitCodeWithHardTimeoutReturnsZeroOnSuccess(t *testing.T) {
	var stderr bytes.Buffer
	code := exitCodeWithHardTimeout(func() error {
		return nil
	}, time.Second, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestEventFromArgsNormalizesOnlyDnsmasqLeaseActions(t *testing.T) {
	for raw, want := range map[string]string{
		"add": daemonapi.DHCPLeaseActionAdded,
		"old": daemonapi.DHCPLeaseActionRenewed,
		"del": daemonapi.DHCPLeaseActionRemoved,
	} {
		event, err := eventFromArgs([]string{raw, "02:00:00:00:00:01", "192.0.2.10", "host"}, []string{"DNSMASQ_INTERFACE=lan0", "SECRET=value"})
		if err != nil {
			t.Fatalf("eventFromArgs(%q): %v", raw, err)
		}
		if event.Action != want || event.Interface != "lan0" {
			t.Fatalf("eventFromArgs(%q) = %#v", raw, event)
		}
	}
	if _, err := eventFromArgs([]string{"renew", "02:00:00:00:00:01", "192.0.2.10"}, nil); err == nil {
		t.Fatal("eventFromArgs accepted an unknown dnsmasq lease action")
	}
}

func TestLeaseEventCanonicalizesAddressWithoutRequiringEthernetIdentity(t *testing.T) {
	event, err := eventFromArgs([]string{"add", "client-duid", "2001:0db8:0:0::1", "host"}, nil)
	if err != nil || event.IP != "2001:db8::1" || event.MAC != "client-duid" {
		t.Fatalf("usable IPv6 callback lost: %+v %v", event, err)
	}
	if _, err := eventFromArgs([]string{"add", "client", "not-an-address"}, nil); err == nil {
		t.Fatal("invalid address was accepted for durable queue")
	}
}
