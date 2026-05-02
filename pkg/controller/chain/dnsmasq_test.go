package chain

import (
	"reflect"
	"testing"

	"routerd/pkg/api"
)

type mapStore map[string]map[string]any

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if status := s[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func TestDNSResolverUpstreamLinesExpandStatusReferences(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolverUpstream"},
			Metadata: api.ObjectMeta{Name: "ngn"},
			Spec: api.DNSResolverUpstreamSpec{
				Zones: []api.DNSResolverZoneSpec{
					{Zone: "transix.jp.", Servers: []string{"${DHCPv6Information/wan-info.status.dnsServers}"}},
				},
				Default: api.DNSResolverDefaultSpec{Servers: []string{"2001:4860:4860::8888"}},
			},
		},
	}}}
	store := mapStore{
		api.NetAPIVersion + "/DHCPv6Information/wan-info": {
			"dnsServers": []any{"2409:10:3d60:1200:1eb1:7fff:fe73:76d8"},
		},
	}

	got := dnsmasqResolverLines(router, store)
	want := []string{
		"server=2001:4860:4860::8888",
		"server=/transix.jp/2409:10:3d60:1200:1eb1:7fff:fe73:76d8",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolver lines = %#v, want %#v", got, want)
	}
}

func TestDSLiteTunnelResolveRemoteDirectIPv6SkipsDNS(t *testing.T) {
	controller := DSLiteTunnelController{ResolverPort: 9}
	name, remote, err := controller.resolveRemote(t.Context(), api.DSLiteTunnelSpec{AFTRIPv6: "2404:8e00::feed:100"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "2404:8e00::feed:100" || remote != "2404:8e00::feed:100" {
		t.Fatalf("resolved name=%q remote=%q", name, remote)
	}
}
