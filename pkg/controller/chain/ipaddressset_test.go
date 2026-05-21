// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"routerd/pkg/api"
)

type fakeIPAddressSetResolver map[string][]ipAddressSetRecord

func (r fakeIPAddressSetResolver) ResolveIP(_ context.Context, name string) ([]ipAddressSetRecord, error) {
	if records := r[name]; len(records) > 0 {
		return records, nil
	}
	return nil, os.ErrNotExist
}

type ipAddressSetResolverFunc func(context.Context, string) ([]ipAddressSetRecord, error)

func (f ipAddressSetResolverFunc) ResolveIP(ctx context.Context, name string) ([]ipAddressSetRecord, error) {
	return f(ctx, name)
}

func TestIPAddressSetControllerRefreshesReferencedNftSet(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "dns-google"}, Spec: api.IPAddressSetSpec{
			Addresses: []string{"8.8.8.8"},
			Names:     []string{"dns.google"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "LocalServiceRedirect"}, Metadata: api.ObjectMeta{Name: "lan-local-services"}, Spec: api.LocalServiceRedirectSpec{
			Interface: "lan",
			Rules: []api.LocalServiceRedirectRuleSpec{{
				Name:              "dns-google",
				Protocols:         []string{"udp", "tcp"},
				DestinationSetRef: "IPAddressSet/dns-google",
				DestinationPort:   53,
				RedirectPort:      53,
			}},
		}},
	}}}
	store := mapStore{}
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	var script string
	controller := IPAddressSetController{
		Router:     router,
		Store:      store,
		RuntimeDir: t.TempDir(),
		NftCommand: "nft-test",
		Resolver: fakeIPAddressSetResolver{
			"dns.google": {
				{Address: "8.8.4.4", TTL: 300 * time.Second},
				{Address: "2001:4860:4860::8844", TTL: 600 * time.Second},
			},
		},
		Now: func() time.Time { return now },
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "nft-test" {
				t.Fatalf("command name = %s", name)
			}
			switch strings.Join(args, " ") {
			case "list set ip routerd_nat ip_address_set_dns_google",
				"list set ip6 routerd_nat ip_address_set_dns_google":
				return []byte("set exists"), nil
			}
			if len(args) == 2 && args[0] == "-f" {
				data, err := os.ReadFile(args[1])
				if err != nil {
					t.Fatal(err)
				}
				script += string(data)
				return nil, nil
			}
			t.Fatalf("unexpected nft args: %v", args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, want := range []string{
		"add element ip routerd_nat ip_address_set_dns_google { 8.8.4.4, 8.8.8.8 }",
		"add element ip6 routerd_nat ip_address_set_dns_google { 2001:4860:4860::8844 }",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("nft script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "flush set ") {
		t.Fatalf("nft set refresh must not flush live sets:\n%s", script)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPAddressSet", "dns-google")
	if status["phase"] != "Applied" {
		t.Fatalf("status = %#v", status)
	}
	if status["nextRefreshAt"] != now.Add(150*time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("nextRefreshAt = %#v", status["nextRefreshAt"])
	}
}

func TestIPAddressSetControllerRefreshesPolicyRouteNftSet(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "cloud-service"}, Spec: api.IPAddressSetSpec{
			Names: []string{"service.example.test"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "cloud-via-alt"}, Spec: api.EgressRoutePolicySpec{
			Mode:               "mark",
			DestinationSetRefs: []string{"IPAddressSet/cloud-service"},
			Candidates: []api.EgressRoutePolicyCandidate{{
				Interface: "wan-alt",
				Table:     200,
				Priority:  1200,
				Mark:      0x120,
			}},
		}},
	}}}
	store := mapStore{}
	var script string
	controller := IPAddressSetController{
		Router:     router,
		Store:      store,
		RuntimeDir: t.TempDir(),
		NftCommand: "nft-test",
		Resolver: fakeIPAddressSetResolver{
			"service.example.test": {{Address: "203.0.113.10", TTL: 300 * time.Second}},
		},
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "nft-test" {
				t.Fatalf("command name = %s", name)
			}
			if strings.Join(args, " ") == "list set ip routerd_policy ip_address_set_cloud_service" {
				return []byte("set exists"), nil
			}
			if len(args) == 2 && args[0] == "-f" {
				data, err := os.ReadFile(args[1])
				if err != nil {
					t.Fatal(err)
				}
				script = string(data)
				return nil, nil
			}
			t.Fatalf("unexpected nft args: %v", args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, want := range []string{
		"add element ip routerd_policy ip_address_set_cloud_service { 203.0.113.10 }",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("nft script missing %q:\n%s", want, script)
		}
	}
}

func TestIPAddressSetControllerRefreshesFirewallNftSets(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "cloud-service"}, Spec: api.IPAddressSetSpec{
			Names: []string{"service.example.test"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "lan-to-cloud"}, Spec: api.FirewallRuleSpec{
			FromZone:           "lan",
			ToZone:             "wan",
			Protocol:           "tcp",
			Port:               443,
			DestinationSetRefs: []string{"IPAddressSet/cloud-service"},
			Action:             "accept",
		}},
	}}}
	store := mapStore{}
	var script string
	controller := IPAddressSetController{
		Router:     router,
		Store:      store,
		RuntimeDir: t.TempDir(),
		NftCommand: "nft-test",
		Resolver: fakeIPAddressSetResolver{
			"service.example.test": {
				{Address: "203.0.113.10", TTL: 300 * time.Second},
				{Address: "2001:db8::10", TTL: 300 * time.Second},
			},
		},
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "nft-test" {
				t.Fatalf("command name = %s", name)
			}
			switch strings.Join(args, " ") {
			case "list set inet routerd_filter ip_address_set_cloud_service_v4",
				"list set inet routerd_filter ip_address_set_cloud_service_v6":
				return []byte("set exists"), nil
			}
			if len(args) == 2 && args[0] == "-f" {
				data, err := os.ReadFile(args[1])
				if err != nil {
					t.Fatal(err)
				}
				script += string(data)
				return nil, nil
			}
			t.Fatalf("unexpected nft args: %v", args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, want := range []string{
		"add element inet routerd_filter ip_address_set_cloud_service_v4 { 203.0.113.10 }",
		"add element inet routerd_filter ip_address_set_cloud_service_v6 { 2001:db8::10 }",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("nft script missing %q:\n%s", want, script)
		}
	}
}

func TestIPAddressSetControllerDiffsNftSetElements(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "cloud-service"}, Spec: api.IPAddressSetSpec{
			Addresses: []string{"203.0.113.10", "203.0.113.11"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "cloud-via-alt"}, Spec: api.EgressRoutePolicySpec{
			Mode:               "mark",
			DestinationSetRefs: []string{"IPAddressSet/cloud-service"},
			Candidates: []api.EgressRoutePolicyCandidate{{
				Interface: "wan-alt",
				Table:     200,
				Priority:  1200,
				Mark:      0x120,
			}},
		}},
	}}}
	store := mapStore{}
	var script string
	controller := IPAddressSetController{
		Router:     router,
		Store:      store,
		RuntimeDir: t.TempDir(),
		NftCommand: "nft-test",
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "nft-test" {
				t.Fatalf("command name = %s", name)
			}
			if strings.Join(args, " ") == "list set ip routerd_policy ip_address_set_cloud_service" {
				return []byte("set ip_address_set_cloud_service { type ipv4_addr; elements = { 203.0.113.10, 203.0.113.12 } }"), nil
			}
			if len(args) == 2 && args[0] == "-f" {
				data, err := os.ReadFile(args[1])
				if err != nil {
					t.Fatal(err)
				}
				script = string(data)
				return nil, nil
			}
			t.Fatalf("unexpected nft args: %v", args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, want := range []string{
		"delete element ip routerd_policy ip_address_set_cloud_service { 203.0.113.12 }",
		"add element ip routerd_policy ip_address_set_cloud_service { 203.0.113.11 }",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("nft script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "flush set ") {
		t.Fatalf("nft set diff must not flush live sets:\n%s", script)
	}
}

func TestIPAddressSetControllerRefreshFloorAndConfiguredCap(t *testing.T) {
	if got := nextIPAddressSetRefresh(30*time.Second, ""); got != time.Minute {
		t.Fatalf("short TTL refresh = %v, want 1m", got)
	}
	if got := nextIPAddressSetRefresh(10*time.Minute, "2m"); got != 2*time.Minute {
		t.Fatalf("configured cap refresh = %v, want 2m", got)
	}
	if got := nextIPAddressSetRefresh(10*time.Minute, "10m"); got != 5*time.Minute {
		t.Fatalf("long configured cap refresh = %v, want 5m", got)
	}
}

func TestIPAddressSetControllerRefreshesBeforeDueWhenNftSetLostElements(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "dns-google"}, Spec: api.IPAddressSetSpec{
			Names: []string{"dns.google"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "LocalServiceRedirect"}, Metadata: api.ObjectMeta{Name: "lan-local-services"}, Spec: api.LocalServiceRedirectSpec{
			Interface: "lan",
			Rules: []api.LocalServiceRedirectRuleSpec{{
				Name:              "dns-google",
				Protocols:         []string{"udp"},
				DestinationSetRef: "IPAddressSet/dns-google",
				DestinationPort:   53,
				RedirectPort:      53,
			}},
		}},
	}}}
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	store := mapStore{}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", "dns-google", map[string]any{
		"phase":         "Applied",
		"addresses":     []any{"8.8.8.8"},
		"ipv4Addresses": []any{"8.8.8.8"},
		"nextRefreshAt": now.Add(time.Hour).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	var script string
	controller := IPAddressSetController{
		Router:     router,
		Store:      store,
		RuntimeDir: t.TempDir(),
		NftCommand: "nft-test",
		Resolver: fakeIPAddressSetResolver{
			"dns.google": {{Address: "8.8.8.8", TTL: 300 * time.Second}},
		},
		Now: func() time.Time { return now },
		Command: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			switch strings.Join(args, " ") {
			case "list set ip routerd_nat ip_address_set_dns_google":
				return []byte("set ip_address_set_dns_google { type ipv4_addr; }"), nil
			case "list set ip6 routerd_nat ip_address_set_dns_google":
				return []byte("set ip_address_set_dns_google { type ipv6_addr; }"), nil
			}
			if len(args) == 2 && args[0] == "-f" {
				data, err := os.ReadFile(args[1])
				if err != nil {
					t.Fatal(err)
				}
				script += string(data)
				return nil, nil
			}
			t.Fatalf("unexpected nft args: %v", args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !strings.Contains(script, "add element ip routerd_nat ip_address_set_dns_google { 8.8.8.8 }") {
		t.Fatalf("nft set was not restored before nextRefreshAt:\n%s", script)
	}
}

func TestIPAddressSetControllerSkipsBeforeDueWhenNftSetContainsCachedElements(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "dns-google"}, Spec: api.IPAddressSetSpec{
			Names: []string{"dns.google"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "LocalServiceRedirect"}, Metadata: api.ObjectMeta{Name: "lan-local-services"}, Spec: api.LocalServiceRedirectSpec{
			Interface: "lan",
			Rules: []api.LocalServiceRedirectRuleSpec{{
				Name:              "dns-google",
				Protocols:         []string{"udp"},
				DestinationSetRef: "IPAddressSet/dns-google",
				DestinationPort:   53,
				RedirectPort:      53,
			}},
		}},
	}}}
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	store := mapStore{}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", "dns-google", map[string]any{
		"phase":         "Applied",
		"addresses":     []any{"8.8.8.8"},
		"ipv4Addresses": []any{"8.8.8.8"},
		"nextRefreshAt": now.Add(time.Hour).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	controller := IPAddressSetController{
		Router:     router,
		Store:      store,
		RuntimeDir: t.TempDir(),
		NftCommand: "nft-test",
		Resolver: ipAddressSetResolverFunc(func(context.Context, string) ([]ipAddressSetRecord, error) {
			t.Fatal("resolver should not be called before nextRefreshAt when nft set is current")
			return nil, nil
		}),
		Now: func() time.Time { return now },
		Command: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			switch strings.Join(args, " ") {
			case "list set ip routerd_nat ip_address_set_dns_google":
				return []byte("set ip_address_set_dns_google { type ipv4_addr; elements = { 8.8.8.8 } }"), nil
			case "list set ip6 routerd_nat ip_address_set_dns_google":
				return []byte("set ip_address_set_dns_google { type ipv6_addr; }"), nil
			}
			t.Fatalf("unexpected nft args: %v", args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

func TestIPAddressSetControllerRespectsTargetDryRunModes(t *testing.T) {
	targets := []ipAddressSetTarget{
		{TableFamily: "ip", AddressFamily: "ip", Table: "routerd_nat", SetName: "set_nat", Controller: "nat"},
		{TableFamily: "ip", AddressFamily: "ip", Table: "routerd_policy", SetName: "set_route", Controller: "route"},
		{TableFamily: "inet", AddressFamily: "ip", Table: "routerd_filter", SetName: "set_filter", Controller: "firewall"},
	}
	controller := IPAddressSetController{DryRunNAT: true, DryRunRoute: false, DryRunFirewall: true}
	active := controller.activeIPAddressSetTargets(targets)
	if len(active) != 1 || active[0].Controller != "route" {
		t.Fatalf("active targets = %#v, want only route", active)
	}
	dryRun := dryRunIPAddressSetTargets(targets, controller)
	dryRunSet := map[string]bool{}
	for _, target := range dryRun {
		dryRunSet[target] = true
	}
	for _, want := range []string{"ip/routerd_nat/set_nat", "inet/routerd_filter/set_filter"} {
		if !dryRunSet[want] {
			t.Fatalf("dry-run targets missing %q: %#v", want, dryRun)
		}
	}
}
