// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"fmt"
	"strings"
)

func ExpandClusterNetworkRoutes(router *Router) *Router {
	if router == nil {
		return nil
	}
	existing := map[string]bool{}
	for _, res := range router.Spec.Resources {
		existing[res.APIVersion+"/"+res.Kind+"/"+res.Metadata.Name] = true
	}
	out := *router
	out.Spec.Resources = make([]Resource, 0, len(router.Spec.Resources))
	for _, res := range router.Spec.Resources {
		out.Spec.Resources = append(out.Spec.Resources, res)
		if res.APIVersion != NetAPIVersion || res.Kind != "ClusterNetworkRoute" {
			continue
		}
		spec, err := res.ClusterNetworkRouteSpec()
		if err != nil {
			continue
		}
		for _, generated := range clusterNetworkStaticRoutes(res.Metadata.Name, spec) {
			key := generated.APIVersion + "/" + generated.Kind + "/" + generated.Metadata.Name
			if existing[key] {
				continue
			}
			out.Spec.Resources = append(out.Spec.Resources, generated)
			existing[key] = true
		}
	}
	return &out
}

func clusterNetworkStaticRoutes(owner string, spec ClusterNetworkRouteSpec) []Resource {
	var out []Resource
	for _, item := range []struct {
		group string
		cidrs []string
	}{
		{group: "pods", cidrs: spec.Pods.CIDRs},
		{group: "services", cidrs: spec.Services.CIDRs},
	} {
		group := item.group
		for cidrIndex, cidr := range item.cidrs {
			cidr = strings.TrimSpace(cidr)
			if cidr == "" {
				continue
			}
			for viaIndex, via := range spec.Via {
				nextHop := strings.TrimSpace(via.NextHop)
				if nextHop == "" {
					continue
				}
				out = append(out, Resource{
					TypeMeta: TypeMeta{APIVersion: NetAPIVersion, Kind: "IPv4StaticRoute"},
					Metadata: ObjectMeta{Name: fmt.Sprintf("%s-%s-%02d-via-%02d", owner, group, cidrIndex+1, viaIndex+1)},
					Spec: IPv4StaticRouteSpec{
						Destination: cidr,
						Via:         nextHop,
						Interface:   via.Interface,
						Metric:      clusterNetworkRouteMetric(via.Weight),
					},
				})
			}
		}
	}
	return out
}

func clusterNetworkRouteMetric(weight int) int {
	if weight <= 0 {
		return 100
	}
	if weight > 999 {
		weight = 999
	}
	return 1000 - weight
}
