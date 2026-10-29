// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"encoding/json"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	resolverruntime "github.com/imksoo/routerd/pkg/dnsresolver"
)

// The public forwarder field must survive expansion and the daemon's JSON
// configuration boundary; this does not assert live upstream availability.
func TestForwarderQueryTimeoutRuntimeConfig(t *testing.T) {
	router := dnsResolverRouterWithBootstrapFrom(api.RequiredFieldStatusValueSourceSpec{Resource: "DHCPv6Information/wan-info", Field: "dnsServers"})
	forwarder, err := router.Spec.Resources[1].DNSForwarderSpec()
	if err != nil {
		t.Fatal(err)
	}
	forwarder.QueryTimeout = "1s"
	forwarder.Healthcheck.Timeout = "5s"
	router.Spec.Resources[1].Spec = forwarder
	controller := Controller{Router: router, Store: mapStore{}, RuntimeDir: t.TempDir()}
	spec, err := controller.attachForwarders("lan-resolver", api.DNSResolverSpec{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, _, err := controller.runtimeConfig("lan-resolver", resolverruntime.NormalizeSpec(spec))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(runtime, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var decoded resolverruntime.RuntimeConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Spec.Sources) != 1 || decoded.Spec.Sources[0].QueryTimeout != "1s" || decoded.Spec.Sources[0].Healthcheck.Timeout != "5s" {
		t.Fatalf("runtime sources = %#v", decoded.Spec.Sources)
	}
}
