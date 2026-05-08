package chain

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"routerd/pkg/api"
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

func ntpRouter(spec api.NTPClientSpec) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPClient"},
			Metadata: api.ObjectMeta{Name: "system-time"},
			Spec:     spec,
		},
	}}}
}
