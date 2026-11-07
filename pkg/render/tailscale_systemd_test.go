// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestTailscaleSystemdSpecRendersExitNodeAndSubnetRoutes(t *testing.T) {
	acceptDNS := false
	spec := TailscaleSystemdSpec("home", api.TailscaleNodeSpec{
		Hostname:          "homert02",
		AuthKeyEnv:        "TS_AUTHKEY",
		AuthKeyFile:       "/usr/local/etc/routerd/secrets/tailscale.env",
		AdvertiseExitNode: true,
		AdvertiseRoutes:   []string{"172.18.0.0/16", "192.168.123.0/24"},
		AcceptDNS:         &acceptDNS,
	})
	data := string(SystemdUnit(spec.UnitName, spec))
	for _, want := range []string{
		"Type=oneshot",
		"RemainAfterExit=yes",
		"EnvironmentFile=/usr/local/etc/routerd/secrets/tailscale.env",
		"ExecStart=/usr/bin/tailscale up --timeout=30s --reset --hostname=homert02 --auth-key=${TS_AUTHKEY} --advertise-exit-node --advertise-routes=172.18.0.0/16,192.168.123.0/24 --accept-dns=false",
		"Wants=network-online.target tailscaled.service",
		"After=network-online.target tailscaled.service",
		"Restart=no",
	} {
		if !strings.Contains(data, want) {
			t.Fatalf("unit missing %q:\n%s", want, data)
		}
	}
	if count := strings.Count(data, "--timeout=30s"); count != 1 {
		t.Fatalf("timeout argument count = %d, want 1:\n%s", count, data)
	}
}

func TestTailscaleSystemdSpecCanRemoveUnit(t *testing.T) {
	spec := TailscaleSystemdSpec("home", api.TailscaleNodeSpec{State: "absent"})
	if spec.State != "absent" || spec.UnitName != "routerd-tailscale-home.service" {
		t.Fatalf("unexpected absent spec: %#v", spec)
	}
}

func TestTailscaleSystemdStopDisconnectsWithoutLogout(t *testing.T) {
	for _, binary := range []string{"/usr/bin/tailscale", "/opt/lab/tailscale"} {
		spec := TailscaleSystemdSpec("owned", api.TailscaleNodeSpec{BinaryPath: binary})
		unit := string(SystemdUnit(spec.UnitName, spec))
		if !strings.Contains(unit, "ExecStopPost="+binary+" down\n") {
			t.Fatalf("owned Tailscale service has no disconnect on stop:\n%s", unit)
		}
		if strings.Contains(unit, "logout") {
			t.Fatal("withdrawal must retain node identity")
		}
	}
}

func TestTailscaleDeclaredConfigurationResetsRemovedOptions(t *testing.T) {
	args := TailscaleUpArgs(api.TailscaleNodeSpec{Hostname: "test-router"})
	count := 0
	for _, arg := range args {
		if arg == "--reset" {
			count++
		}
		if arg == "--force-reauth" {
			t.Fatal("configuration updates must retain node authentication")
		}
	}
	if count != 1 {
		t.Fatalf("complete declared settings need one --reset: %v", args)
	}
}
