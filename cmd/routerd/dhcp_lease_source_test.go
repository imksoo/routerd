// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
)

func TestDHCPLeaseSourceUsesActualRangeTagsAndStartupScope(t *testing.T) {
	server := func(name, start, end string) api.Resource {
		return api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: name}, Spec: api.DHCPv4ServerSpec{AddressPool: api.DHCPAddressPoolSpec{Start: start, End: end}, Scopes: []api.DHCPv4ServerScopeSpec{{Name: "iot", AddressPool: api.DHCPAddressPoolSpec{Start: "198.51.100.10", End: "198.51.100.20"}}}}}
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{server("lan", "192.0.2.10", "192.0.2.20")}}}
	for _, tags := range [][]string{{"lan"}, {"lan-iot"}, nil} {
		request := controlapi.DHCPLeaseEventRequest{Action: "renewed", MAC: "client", IP: "192.0.2.15", Tags: tags, ExpiresAt: time.Now().Add(time.Hour)}
		if len(tags) > 0 && tags[0] == "lan-iot" {
			request.IP = "198.51.100.15"
		}
		if disposition := prepareDHCPLeaseSource(router, &request); disposition != "" || request.Source != "DHCPv4Server/lan" {
			t.Fatalf("tags=%v: source=%s disposition=%s", tags, request.Source, disposition)
		}
	}
	// A missing startup tag must not select between overlapping named pools.
	router.Spec.Resources = append(router.Spec.Resources, server("other", "192.0.2.10", "192.0.2.20"))
	request := controlapi.DHCPLeaseEventRequest{Action: "renewed", MAC: "client", IP: "192.0.2.15", Infinite: true}
	if disposition := prepareDHCPLeaseSource(router, &request); !strings.Contains(disposition, "unverified") || request.Source != "" {
		t.Fatalf("ambiguous startup source accepted: %+v %s", request, disposition)
	}
	request.Tags = []string{"lan"}
	if disposition := prepareDHCPLeaseSource(router, &request); disposition != "" || request.Source != "DHCPv4Server/lan" {
		t.Fatalf("observed tag rejected: %+v %s", request, disposition)
	}
}

func TestDHCPLeaseSourceRejectsSanitizedTagCollisionAndUnknownDeadline(t *testing.T) {
	var resources []api.Resource
	for _, name := range []string{"lan.a", "lan-a"} {
		resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: name}, Spec: api.DHCPv4ServerSpec{}})
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: resources}}
	request := controlapi.DHCPLeaseEventRequest{Action: "added", MAC: "client", IP: "192.0.2.15", Tags: []string{"lan-a"}, Infinite: true}
	if disposition := prepareDHCPLeaseSource(router, &request); !strings.Contains(disposition, "ambiguous") {
		t.Fatalf("tag collision granted source: %+v %s", request, disposition)
	}
	router.Spec.Resources = resources[:1]
	request.Infinite = false
	if disposition := prepareDHCPLeaseSource(router, &request); !strings.Contains(disposition, "deadline") {
		t.Fatalf("unknown deadline became infinite: %+v %s", request, disposition)
	}
}
