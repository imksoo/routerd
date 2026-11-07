// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

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

func ntpServerRouter(spec api.NTPServerSpec) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPServer"},
			Metadata: api.ObjectMeta{Name: "lan-time"},
			Spec:     spec,
		},
	}}}
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
