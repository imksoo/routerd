// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

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
