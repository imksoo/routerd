// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	resolvercontroller "github.com/imksoo/routerd/pkg/controller/dnsresolver"
	resolvercfg "github.com/imksoo/routerd/pkg/dnsresolver"
	"github.com/imksoo/routerd/pkg/resourcequery"
	"github.com/imksoo/routerd/pkg/state"
	"github.com/miekg/dns"
)

func TestWhenFilteredListenerCannotSelectAnotherDNSView(t *testing.T) {
	root := t.TempDir()
	store, err := state.OpenSQLite(filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	publicPort, privatePort := freeTCPPort(t), freeTCPPort(t)
	for privatePort == publicPort {
		privatePort = freeTCPPort(t)
	}
	resource := func(kind, name string, spec api.ResourceSpec) api.Resource {
		return api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: kind}, Metadata: api.ObjectMeta{Name: name}, Spec: spec}
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		resource("DNSResolver", "lab", api.DNSResolverSpec{Listen: []api.DNSResolverListenSpec{
			{Addresses: []string{"127.0.0.1"}, Port: publicPort, Sources: []string{"public"}},
			{Addresses: []string{"127.0.0.1"}, Port: privatePort, Sources: []string{"private"}},
		}}),
		resource("DNSZone", "public", api.DNSZoneSpec{Zone: "public.example", Records: []api.DNSZoneRecordSpec{{Hostname: "www", IPv4: "192.0.2.1"}}}),
		resource("DNSZone", "private", api.DNSZoneSpec{Zone: "private.example", Records: []api.DNSZoneRecordSpec{{Hostname: "secret", IPv4: "192.0.2.99"}}}),
		resource("DNSForwarder", "public", api.DNSForwarderSpec{Resolver: "lab", Match: []string{"public.example"}, ZoneRefs: []string{"public"}, When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/vip.role": {Equals: "master"}}}}),
		resource("DNSForwarder", "private", api.DNSForwarderSpec{Resolver: "lab", Match: []string{"private.example"}, ZoneRefs: []string{"private"}}),
	}}}
	configPath := filepath.Join(root, "dns-resolver", "lab", "config.json")
	prepare := func(role string) resolvercfg.RuntimeConfig {
		t.Helper()
		if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip", map[string]any{"role": role}); err != nil {
			t.Fatal(err)
		}
		filtered, err := resourcequery.FilterRouterByWhen(router, store)
		if err != nil {
			t.Fatal(err)
		}
		controller := resolvercontroller.Controller{Router: filtered, DeclaredRouter: router, Store: store, StateDir: root, RuntimeDir: filepath.Join(root, "run"), AllowStartingWorker: true}
		if err := controller.Reconcile(t.Context()); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		var config resolvercfg.RuntimeConfig
		if err := json.Unmarshal(data, &config); err != nil {
			t.Fatal(err)
		}
		return config
	}
	d := newTestDaemon(t, configPath, prepare("master"), false)
	if err := d.startDNS(t.Context()); err != nil {
		t.Fatal(err)
	}
	check := func(port int, name string, wantAnswer bool) {
		t.Helper()
		for _, protocol := range []string{"udp", "tcp"} {
			req := new(dns.Msg)
			req.SetQuestion(name, dns.TypeA)
			answer, _, err := (&dns.Client{Net: protocol, Timeout: time.Second}).ExchangeContext(t.Context(), req, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if err != nil {
				t.Fatal(err)
			}
			if (len(answer.Answer) > 0) != wantAnswer {
				t.Fatalf("%s %s on port %d: unexpected answers %v", protocol, name, port, answer.Answer)
			}
		}
	}
	check(publicPort, "www.public.example.", true)
	check(publicPort, "secret.private.example.", false)
	prepare("backup")
	if _, err := d.reloadConfig(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	check(publicPort, "secret.private.example.", false)
	check(privatePort, "secret.private.example.", true)
	prepare("master")
	if _, err := d.reloadConfig(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	check(publicPort, "www.public.example.", true)
	check(publicPort, "secret.private.example.", false)
}
