// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	resolvercontroller "github.com/imksoo/routerd/pkg/controller/dnsresolver"
	resolvercfg "github.com/imksoo/routerd/pkg/dnsresolver"
	"github.com/imksoo/routerd/pkg/state"
	"github.com/miekg/dns"
)

func TestPendingConditionalUpstreamDoesNotExportPrivateQuestions(t *testing.T) {
	var leaked atomic.Int32
	public := startFailureTestUpstream(t, func(w dns.ResponseWriter, req *dns.Msg) {
		if strings.HasSuffix(req.Question[0].Name, "private.example.") {
			leaked.Add(1)
		}
		failureTestAnswer(w, req)
	})
	private := startFailureTestUpstream(t, failureTestAnswer)
	_, publicPortText, err := net.SplitHostPort(strings.TrimPrefix(public, "udp://"))
	if err != nil {
		t.Fatal(err)
	}
	publicPort, err := strconv.Atoi(publicPortText)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	store, err := state.OpenSQLite(filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	port := freeTCPPort(t)
	resource := func(kind, name string, spec api.ResourceSpec) api.Resource {
		return api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: kind}, Metadata: api.ObjectMeta{Name: name}, Spec: spec}
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		resource("DNSResolver", "lab", api.DNSResolverSpec{Listen: []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}, Port: port, Sources: []string{"private", "default"}}}}),
		resource("DNSForwarder", "private", api.DNSForwarderSpec{Resolver: "lab", Match: []string{"private.example"}, Upstreams: []string{"dynamic"}}),
		resource("DNSForwarder", "default", api.DNSForwarderSpec{Resolver: "lab", Match: []string{"."}, Upstreams: []string{"public"}}),
		resource("DNSUpstream", "dynamic", api.DNSUpstreamSpec{AddressFrom: []api.StatusValueSourceSpec{{Resource: "DHCPv6Information/wan", Field: "dnsServers"}}}),
		resource("DNSUpstream", "public", api.DNSUpstreamSpec{Protocol: "udp", Address: "127.0.0.1", Port: publicPort}),
	}}}
	path := filepath.Join(root, "dns-resolver", "lab", "config.json")
	prepare := func(addresses []string) resolvercfg.RuntimeConfig {
		t.Helper()
		if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Information", "wan", map[string]any{"dnsServers": addresses}); err != nil {
			t.Fatal(err)
		}
		c := resolvercontroller.Controller{Router: router, Store: store, StateDir: root, RuntimeDir: filepath.Join(root, "run"), AllowStartingWorker: true}
		if err := c.Reconcile(t.Context()); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var config resolvercfg.RuntimeConfig
		if err := json.Unmarshal(data, &config); err != nil {
			t.Fatal(err)
		}
		return config
	}
	d := newTestDaemon(t, path, prepare(nil), false)
	if err := d.startDNS(t.Context()); err != nil {
		t.Fatal(err)
	}
	check := func(name string, rcode int) {
		t.Helper()
		for _, protocol := range []string{"udp", "tcp"} {
			q := new(dns.Msg)
			q.SetQuestion(name, dns.TypeA)
			response, _, err := (&dns.Client{Net: protocol, Timeout: time.Second}).ExchangeContext(t.Context(), q, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if err != nil {
				t.Fatal(err)
			}
			if response.Rcode != rcode || leaked.Load() != 0 {
				t.Fatalf("%s %s: rcode=%s private questions at public=%d", protocol, name, dns.RcodeToString[response.Rcode], leaked.Load())
			}
		}
	}
	check("secret.private.example.", dns.RcodeServerFailure)
	if !strings.Contains(d.observedStatus()["upstreams"], "private=Pending") {
		t.Fatalf("pending source absent from worker status: %v", d.observedStatus())
	}
	check("www.public.example.", dns.RcodeSuccess)
	prepare([]string{private})
	if _, err := d.reloadConfig(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	check("secret.private.example.", dns.RcodeSuccess)
	prepare(nil)
	if _, err := d.reloadConfig(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	check("secret.private.example.", dns.RcodeServerFailure)
	if !strings.Contains(d.observedStatus()["upstreams"], "private=Pending") {
		t.Fatalf("pending source absent from worker status: %v", d.observedStatus())
	}
	check("www.public.example.", dns.RcodeSuccess)
	// A sole pending source must replace the old worker configuration too.
	// Otherwise withdrawal can retain previously learned upstream authority.
	router.Spec.Resources = append(router.Spec.Resources[:2:2], router.Spec.Resources[3:]...)
	prepare(nil)
	if _, err := d.reloadConfig(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	check("secret.private.example.", dns.RcodeServerFailure)

}
