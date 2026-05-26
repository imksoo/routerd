// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
)

func TestNTPClientControllerUsesDHCPv6SNTPServers(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/DHCPv6Information/wan-info": {
			"sntpServers": []any{"2001:db8::123", "2001:db8::124"},
		},
	}
	configPath := filepath.Join(t.TempDir(), "routerd.conf")
	var commands [][]string
	controller := NTPClientController{
		Router: ntpRouter(api.NTPClientSpec{
			Provider:        "systemd-timesyncd",
			Managed:         true,
			Source:          "auto",
			ServerFrom:      []api.StatusValueSourceSpec{{Resource: "DHCPv6Information/wan-info", Field: "sntpServers"}},
			FallbackServers: []string{"ntp.jst.mfeed.ad.jp"},
		}),
		Store:      store,
		ConfigPath: configPath,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			return nil, nil
		},
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if got, want := string(data), "NTP=2001:db8::123 2001:db8::124\n"; !strings.Contains(got, want) {
		t.Fatalf("config missing %q:\n%s", want, got)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "NTPClient", "system-time")
	if status["source"] != "auto" {
		t.Fatalf("unexpected source status: %#v", status)
	}
	if !reflect.DeepEqual(status["servers"], []string{"2001:db8::123", "2001:db8::124"}) {
		t.Fatalf("unexpected servers status: %#v", status)
	}
	if len(commands) != 2 {
		t.Fatalf("expected timedatectl + restart, got %#v", commands)
	}
}

func TestNTPClientControllerFallsBackWhenDynamicSourceMissing(t *testing.T) {
	store := mapStore{}
	configPath := filepath.Join(t.TempDir(), "routerd.conf")
	controller := NTPClientController{
		Router: ntpRouter(api.NTPClientSpec{
			Provider:        "systemd-timesyncd",
			Managed:         true,
			Source:          "auto",
			ServerFrom:      []api.StatusValueSourceSpec{{Resource: "DHCPv6Information/wan-info", Field: "sntpServers"}},
			FallbackServers: []string{"ntp.jst.mfeed.ad.jp", "ntp.nict.jp"},
		}),
		Store:      store,
		ConfigPath: configPath,
		Command: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return nil, nil
		},
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if got, want := string(data), "NTP=ntp.jst.mfeed.ad.jp ntp.nict.jp\n"; !strings.Contains(got, want) {
		t.Fatalf("config missing %q:\n%s", want, got)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "NTPClient", "system-time")
	if status["source"] != "fallback" {
		t.Fatalf("unexpected source status: %#v", status)
	}
}

func TestNTPClientControllerReportsTimesyncdDisableForChrony(t *testing.T) {
	store := mapStore{}
	eventBus := bus.New()
	configPath := filepath.Join(t.TempDir(), "chrony.conf")
	var commands []string
	controller := NTPClientController{
		Router: ntpRouter(api.NTPClientSpec{
			Provider: "chrony",
			Managed:  true,
			Servers:  []string{"ntp.example.net"},
		}),
		Bus:        eventBus,
		Store:      store,
		ConfigPath: configPath,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			commands = append(commands, line)
			return []byte("ok"), nil
		},
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := strings.Join(commands, "\n")
	if !strings.Contains(got, "systemctl disable --now systemd-timesyncd.service") {
		t.Fatalf("timesyncd disable command missing:\n%s", got)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "NTPClient", "system-time")
	if status["disabledUnit"] != "systemd-timesyncd.service" || status["disableReason"] != "TimesyncdDisabledForChrony" {
		t.Fatalf("status = %#v", status)
	}
	events := eventBus.Recent("routerd.system.ntp.provider_conflict_resolved")
	if len(events) != 1 || events[0].Resource == nil || events[0].Resource.Name != "system-time" {
		t.Fatalf("events = %#v", events)
	}
}

func TestRenderNTPDConfig(t *testing.T) {
	data := renderNTPDConfig([]string{"ntp.jst.mfeed.ad.jp", "ntp.nict.jp"})
	if got, want := string(data), "server ntp.jst.mfeed.ad.jp iburst\n"; !strings.Contains(got, want) {
		t.Fatalf("config missing %q:\n%s", want, got)
	}
	if got, want := string(data), "server ntp.nict.jp iburst\n"; !strings.Contains(got, want) {
		t.Fatalf("config missing %q:\n%s", want, got)
	}
}

func TestRenderNTPDConfigWithListenAddresses(t *testing.T) {
	data := renderNTPDConfig([]string{"ntp.jst.mfeed.ad.jp"}, []string{"192.168.160.4", "2409:10:3d60:1250::4/64"})
	for _, want := range []string{
		"interface ignore all\n",
		"interface listen 127.0.0.1\n",
		"interface listen ::1\n",
		"interface listen 192.168.160.4\n",
		"interface listen 2409:10:3d60:1250::4\n",
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("config missing %q:\n%s", want, string(data))
		}
	}
}

func TestNTPServerControllerResolvesAllowCIDRFromDelegatedAddress(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/IPv6DelegatedAddress/lan-base": {
			"address": "2001:db8:1234:5601::1/64",
		},
	}
	configPath := filepath.Join(t.TempDir(), "chrony.conf")
	controller := NTPServerController{
		Router: ntpServerRouter(api.NTPServerSpec{
			Provider:        "chrony",
			Managed:         true,
			Servers:         []string{"ntp.example.net"},
			AllowCIDRs:      []string{"172.18.0.0/16"},
			AllowCIDRFrom:   []api.StatusValueSourceSpec{{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"}},
			ListenAddresses: []string{"172.18.0.1"},
		}),
		Store:      store,
		ConfigPath: configPath,
		Command: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return nil, nil
		},
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	for _, want := range []string{
		"allow 172.18.0.0/16\n",
		"allow 2001:db8:1234:5601::/64\n",
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("config missing %q:\n%s", want, data)
		}
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "NTPServer", "lan-time")
	if status["phase"] != "Applied" {
		t.Fatalf("unexpected status: %#v", status)
	}
	if !reflect.DeepEqual(status["allowCIDRs"], []string{"172.18.0.0/16", "2001:db8:1234:5601::/64"}) {
		t.Fatalf("unexpected allowCIDRs: %#v", status)
	}
}

func TestNTPServerControllerResolvesAllowCIDRFromPrefixDelegation(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/DHCPv6PrefixDelegation/wan-pd": {
			"currentPrefix": "2001:db8:1234:5601::/64",
		},
	}
	configPath := filepath.Join(t.TempDir(), "chrony.conf")
	controller := NTPServerController{
		Router: ntpServerRouter(api.NTPServerSpec{
			Provider:      "chrony",
			Managed:       true,
			Servers:       []string{"ntp.example.net"},
			AllowCIDRFrom: []api.StatusValueSourceSpec{{Resource: "DHCPv6PrefixDelegation/wan-pd", Field: "currentPrefix"}},
		}),
		Store:      store,
		ConfigPath: configPath,
		Command: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return nil, nil
		},
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if got, want := string(data), "allow 2001:db8:1234:5601::/64\n"; !strings.Contains(got, want) {
		t.Fatalf("config missing %q:\n%s", want, got)
	}
}

func TestNTPServerControllerMarksInvalidAllowCIDRFromPending(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/IPv6DelegatedAddress/lan-base": {
			"address": "2001:db8:1234:5601::1",
		},
	}
	controller := NTPServerController{
		Router: ntpServerRouter(api.NTPServerSpec{
			Provider:      "chrony",
			Managed:       true,
			Servers:       []string{"ntp.example.net"},
			AllowCIDRFrom: []api.StatusValueSourceSpec{{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"}},
		}),
		Store:      store,
		ConfigPath: filepath.Join(t.TempDir(), "chrony.conf"),
		DryRun:     true,
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "NTPServer", "lan-time")
	if status["phase"] != "Pending" || status["reason"] != "AllowCIDRFromInvalid" {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestNTPServerControllerMarksMissingAllowCIDRFromPending(t *testing.T) {
	store := mapStore{}
	controller := NTPServerController{
		Router: ntpServerRouter(api.NTPServerSpec{
			Provider:      "chrony",
			Managed:       true,
			Servers:       []string{"ntp.example.net"},
			AllowCIDRFrom: []api.StatusValueSourceSpec{{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"}},
		}),
		Store:      store,
		ConfigPath: filepath.Join(t.TempDir(), "chrony.conf"),
		DryRun:     true,
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "NTPServer", "lan-time")
	if status["phase"] != "Pending" || status["reason"] != "AllowCIDRFromPending" {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func ntpRouter(spec api.NTPClientSpec) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPClient"},
			Metadata: api.ObjectMeta{Name: "system-time"},
			Spec:     spec,
		},
	}}}
}

func ntpServerRouter(spec api.NTPServerSpec) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPServer"},
			Metadata: api.ObjectMeta{Name: "lan-time"},
			Spec:     spec,
		},
	}}}
}
