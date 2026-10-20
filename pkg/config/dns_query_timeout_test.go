// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

// Config validation must reject invalid per-query deadlines before rendering.
func TestValidateDNSForwarderQueryTimeout(t *testing.T) {
	for _, tt := range []struct {
		value string
		valid bool
	}{
		{"", true}, {"1s", true}, {"250ms", true},
		{"0s", false}, {"-1s", false}, {"invalid", false}, {" ", false},
	} {
		t.Run(tt.value, func(t *testing.T) {
			resource := testResource(api.NetAPIVersion, "DNSForwarder", "default", api.DNSForwarderSpec{
				Resolver: "DNSResolver/lan", Match: []string{"."},
				Upstreams: []string{"DNSUpstream/primary"}, QueryTimeout: tt.value,
			})
			matched, err := validateDNSResource(resource, platform.OSLinux)
			if !matched || (err == nil) != tt.valid {
				t.Fatalf("validate queryTimeout %q: matched=%v err=%v", tt.value, matched, err)
			}
			if err != nil && !strings.Contains(err.Error(), "spec.queryTimeout must be a positive duration") {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}
