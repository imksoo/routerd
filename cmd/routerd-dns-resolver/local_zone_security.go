// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"

	"github.com/imksoo/routerd/pkg/dnsresolver"
)

func validateLocalZones(config dnsresolver.RuntimeConfig) error {
	for _, zone := range config.Zones {
		if zone.Spec.DNSSEC.Enabled {
			return fmt.Errorf("DNSZone/%s local DNSSEC signing and validation are not implemented", zone.Name)
		}
	}
	return nil
}
