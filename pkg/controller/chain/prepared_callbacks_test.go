// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"log/slog"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/ha"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"github.com/miekg/dns"
)

func TestPreparedVXLANDeadlineUsesUpdatedDeclaration(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Set("role", "master", "fresh test role"); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "accepted"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "underlay"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "fabric"}, Spec: api.VXLANTunnelSpec{VNI: 42, LocalAddress: "192.0.2.1", Peers: []string{"192.0.2.2"}, UnderlayInterface: "underlay", When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"role": {Equals: "master", MaxAge: "1h"}}}}},
	}}}
	if err := config.Validate(router); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{Router: router, Bus: bus.New(), Store: store, Opts: Options{EnabledControllers: []string{"vxlan-segment"}}}
	controller := preparedCallback(t, runner)
	deadline := controller.(interface{ NextReconcileAfter() time.Duration })
	if got := deadline.NextReconcileAfter(); got < 59*time.Minute || got > time.Hour {
		t.Fatalf("initial deadline = %s", got)
	}
	next := router.Clone()
	spec, _ := next.Spec.Resources[1].VXLANTunnelSpec()
	spec.When.State["role"] = api.StateMatchSpec{Equals: "master", MaxAge: "1m"}
	next.Spec.Resources[1].Spec = spec
	runner.runtimeSnapshot = RuntimeSnapshot{Known: true, Available: true, Router: router}
	if err := runner.UpdateRuntimeRouter(&next); err != nil {
		t.Fatal(err)
	}
	if got := deadline.NextReconcileAfter(); got < 50*time.Second || got > time.Minute {
		t.Fatalf("existing controller retained old declaration deadline = %s", got)
	}
}

func TestPreparedIngressCallbackRefreshesStatusDerivedResolver(t *testing.T) {
	first, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := first.LocalAddr().(*net.UDPAddr).Port
	second, err := net.ListenPacket("udp6", net.JoinHostPort("::1", strconv.Itoa(port)))
	if err != nil {
		first.Close()
		t.Fatal(err)
	}
	serveDNS := func(conn net.PacketConn, answer string) {
		t.Helper()
		server := &dns.Server{PacketConn: conn, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, query *dns.Msg) {
			reply := new(dns.Msg)
			reply.SetReply(query)
			if len(query.Question) != 0 && query.Question[0].Qtype == dns.TypeA {
				reply.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: query.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET}, A: net.ParseIP(answer)}}
			}
			_ = w.WriteMsg(reply)
		})}
		go func() { _ = server.ActivateAndServe() }()
		t.Cleanup(func() { _ = server.Shutdown() })
	}
	serveDNS(first, "192.0.2.11")
	serveDNS(second, "192.0.2.22")
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	loopback := "lo"
	if platform.CurrentOS() == platform.OSFreeBSD {
		loopback = "lo0"
	}
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "accepted"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "loopback"}, Spec: api.InterfaceSpec{IfName: loopback}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "resolver"}, Spec: api.DNSResolverSpec{Listen: []api.DNSResolverListenSpec{{AddressFrom: []api.StatusValueSourceSpec{{Resource: "Interface/loopback", Field: "addresses"}}, Port: port}}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSForwarder"}, Metadata: api.ObjectMeta{Name: "default"}, Spec: api.DNSForwarderSpec{Resolver: "DNSResolver/resolver", Match: []string{"."}, Upstreams: []string{"DNSUpstream/public"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSUpstream"}, Metadata: api.ObjectMeta{Name: "public"}, Spec: api.DNSUpstreamSpec{Protocol: "udp", Address: "127.0.0.1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "service"}, Spec: api.IngressServiceSpec{Listen: api.IngressListenSpec{Interface: "loopback", Address: "192.0.2.1", Protocol: "tcp", Port: 443}, Backends: []api.IngressBackendSpec{{Name: "backend", Address: "backend.test", Port: 8443}}}},
	}}}
	if err := config.Validate(router); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "loopback", map[string]any{"addresses": []string{"127.0.0.1"}}); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{Router: router, Bus: bus.New(), Store: store, Opts: Options{EnabledControllers: []string{"ingress-service"}, DryRunIngress: true}}
	controller := preparedCallback(t, runner)
	for _, point := range []struct{ listen, answer string }{{"127.0.0.1", "192.0.2.11"}, {"::1", "192.0.2.22"}} {
		if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "loopback", map[string]any{"addresses": []string{point.listen}}); err != nil {
			t.Fatal(err)
		}
		if _, err := controller.PeriodicReconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		status := store.ObjectStatus(api.FirewallAPIVersion, "IngressService", "service")
		active, _ := status["activeBackend"].(map[string]any)
		if active["address"] != point.answer {
			t.Fatalf("listen %s selected backend %+v, want %s", point.listen, active, point.answer)
		}
	}
}

func preparedCallback(t *testing.T, runner *Runner) framework.Controller {
	t.Helper()
	controllers, _, _, err := runner.frameworkControllers(context.Background(), slog.Default(), eventedStore{Store: runner.Store, Bus: runner.Bus, Router: runner.Router}, false, ha.Decision{Leader: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(controllers) != 1 {
		t.Fatalf("controllers = %d, want one", len(controllers))
	}
	return controllers[0]
}
