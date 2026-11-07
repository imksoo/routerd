// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestDNSZoneRejectsUnsupportedDNSSEC(t *testing.T) {
	for _, mode := range []string{"", "sign", "validate"} {
		resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSZone"}, Metadata: api.ObjectMeta{Name: "local"}, Spec: api.DNSZoneSpec{Zone: "example.test", DNSSEC: api.DNSZoneDNSSECSpec{Enabled: true, Mode: mode}}}
		if handled, err := validateDNSResource(resource, platform.OSLinux); !handled || err == nil {
			t.Fatalf("unsupported local DNSSEC mode %q admitted: %v", mode, err)
		}
	}
	resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSZone"}, Metadata: api.ObjectMeta{Name: "local"}, Spec: api.DNSZoneSpec{Zone: "example.test"}}
	if _, err := validateDNSResource(resource, platform.OSLinux); err != nil {
		t.Fatal(err)
	}
}
