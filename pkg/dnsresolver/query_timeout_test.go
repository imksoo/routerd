// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

// Normalization preserves the previous ordinary-query timeout when omitted;
// an explicit timeout must not change background health-check deadlines.
func TestNormalizeQueryTimeout(t *testing.T) {
	for _, tt := range []struct {
		name, query, health, want string
	}{
		{"default", "", "", "3s"},
		{"inherit", "", "5s", "5s"},
		{"legacy-zero-healthcheck", "", "0s", "3s"},
		{"legacy-negative-healthcheck", "", "-1s", "3s"},
		{"independent", "1s", "5s", "1s"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			spec := NormalizeSpec(api.DNSResolverSpec{Sources: []api.DNSResolverSourceSpec{{
				Kind: "upstream", Match: []string{"."}, Upstreams: []string{"udp://192.0.2.53:53"},
				QueryTimeout: tt.query, Healthcheck: api.DNSResolverHealthcheckSpec{Timeout: tt.health},
			}}})
			if got := spec.Sources[0].QueryTimeout; got != tt.want {
				t.Fatalf("queryTimeout = %q, want %q", got, tt.want)
			}
			if tt.health != "" && spec.Sources[0].Healthcheck.Timeout != tt.health {
				t.Fatalf("healthcheck.timeout changed: %#v", spec.Sources[0].Healthcheck)
			}
			if err := Validate(spec); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateQueryTimeout(t *testing.T) {
	for _, value := range []string{"0s", "-1s", "invalid", " "} {
		t.Run(value, func(t *testing.T) {
			err := Validate(api.DNSResolverSpec{Sources: []api.DNSResolverSourceSpec{{
				Kind: "upstream", Match: []string{"."}, Upstreams: []string{"udp://192.0.2.53:53"}, QueryTimeout: value,
			}}})
			if err == nil || !strings.Contains(err.Error(), "queryTimeout must be a positive duration") {
				t.Fatalf("Validate(%q) = %v", value, err)
			}
		})
	}
}
