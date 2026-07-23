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
		"ExecStart=/usr/bin/tailscale up --hostname=homert02 --auth-key=${TS_AUTHKEY} --advertise-exit-node --advertise-routes=172.18.0.0/16,192.168.123.0/24 --accept-dns=false",
		"Wants=network-online.target tailscaled.service",
		"After=network-online.target tailscaled.service",
		"Restart=no",
		"TimeoutStartSec=45s",
		"StandardOutput=null",
		"StandardError=null",
	} {
		if !strings.Contains(data, want) {
			t.Fatalf("unit missing %q:\n%s", want, data)
		}
	}
}

func TestTailscaleSystemdSpecCanRemoveUnit(t *testing.T) {
	spec := TailscaleSystemdSpec("home", api.TailscaleNodeSpec{State: "absent"})
	if spec.State != "absent" || spec.UnitName != "routerd-tailscale-home.service" {
		t.Fatalf("unexpected absent spec: %#v", spec)
	}
}
