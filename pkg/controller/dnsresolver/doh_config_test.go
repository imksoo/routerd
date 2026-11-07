// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	configfile "github.com/imksoo/routerd/pkg/config"
)

func TestDoHTLSNameSurvivesOperatorConfigToWorkerInput(t *testing.T) {
	router, err := configfile.LoadBytes([]byte(`
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: dns-test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSResolver
      metadata:
        name: lan
      spec:
        listen:
          - addresses: [127.0.0.1]
            port: 53
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSForwarder
      metadata:
        name: default
      spec:
        resolver: DNSResolver/lan
        match: ["."]
        upstreams: [DNSUpstream/secure]
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSUpstream
      metadata:
        name: secure
      spec:
        protocol: doh
        address: 192.0.2.53
        tlsName: resolver.example
        path: /dns-query
`), "doh.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var resolver api.DNSResolverSpec
	for _, resource := range router.Spec.Resources {
		if resource.Kind == "DNSResolver" {
			resolver, err = resource.DNSResolverSpec()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	resolved, err := (Controller{Router: router}).attachForwarders("lan", resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Sources) != 1 || len(resolved.Sources[0].Upstreams) != 1 || resolved.Sources[0].Upstreams[0] != "https://192.0.2.53:443/dns-query?serverName=resolver.example" {
		t.Fatalf("configured TLS identity lost before worker: %+v", resolved.Sources)
	}
}
