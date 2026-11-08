// SPDX-License-Identifier: BSD-3-Clause

package servicemgr

import (
	"strings"
	"testing"
)

func TestValidateServiceRejectsInvalidNames(t *testing.T) {
	tests := []struct {
		name    string
		manager Manager
		service Service
	}{
		{name: "empty", manager: Systemd{}, service: Service{}},
		{name: "nul", manager: RCD{}, service: Service{RCDName: "bad\x00name"}},
		{name: "tooLong", manager: Systemd{}, service: Service{SystemdName: strings.Repeat("a", 65)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateService(tt.manager, tt.service); err == nil {
				t.Fatalf("ValidateService(%s, %#v) succeeded, want error", tt.manager.Name(), tt.service)
			}
		})
	}
}

func TestServiceNameEdgeCasesAcrossManagers(t *testing.T) {
	tests := []Service{
		{SystemdName: "routerd-test.service", RCDName: "routerd_test"},
		{SystemdName: "routerd.special-name@lan.service", RCDName: "routerd_special_name_lan"},
		{SystemdName: strings.Repeat("a", 56) + ".service", RCDName: strings.Repeat("c", 64)},
	}
	for _, service := range tests {
		for _, manager := range []Manager{Systemd{}, RCD{}} {
			t.Run(manager.Name()+"/"+manager.ServiceName(service), func(t *testing.T) {
				if err := ValidateService(manager, service); err != nil {
					t.Fatalf("valid edge service rejected: %v", err)
				}
			})
		}
	}
	unicodeService := Service{SystemdName: "routerd-測試.service", RCDName: "routerd_測試"}
	for _, manager := range []Manager{Systemd{}, RCD{}} {
		if err := ValidateService(manager, unicodeService); err != nil {
			t.Fatalf("%s rejected valid unicode service name: %v", manager.Name(), err)
		}
	}
}
