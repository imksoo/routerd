// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type dsliteNativeStore struct {
	path string
	*routerstate.SQLiteStore
	failPending, failComplete, failRead bool
	afterPending                        func()
}

func (s *dsliteNativeStore) ReadObjectStatus(version, kind, name string) (map[string]any, error) {
	if name == dsliteNativeStatusName && s.failRead {
		return nil, errors.New("native receipt read failed")
	}
	return s.SQLiteStore.ReadObjectStatus(version, kind, name)
}

func (s *dsliteNativeStore) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if name == dsliteNativeStatusName {
		var records []dsliteTunnelReceipt
		if !decodeAppliedDataplaneStatus(status["tunnels"], &records) {
			return errors.New("malformed native receipt write")
		}
		for _, record := range records {
			if record.Pending && s.failPending || !record.Pending && s.failComplete {
				return errors.New("native receipt write failed")
			}
		}
		if err := s.SQLiteStore.SaveObjectStatus(version, kind, name, status); err != nil {
			return err
		}
		if s.afterPending != nil && len(records) > 0 && records[0].Pending {
			action := s.afterPending
			s.afterPending = nil
			action()
		}
		return nil
	}
	return s.SQLiteStore.SaveObjectStatus(version, kind, name, status)
}

// This fixture supplies only command observations and effects. The real actor
// parses them, owns receipts, chooses operations and decides recovery/publication.
type dsliteNativeFixture struct {
	t                                                                    *testing.T
	store                                                                func() Store
	links                                                                map[string]dsliteTunnelObservation
	defaultRoute                                                         *ordinaryIPv4RouteObservation
	calls, writes                                                        []string
	nextIndex                                                            int
	failCreate, loseCreateReply, failDelete, failLinkRead, failRouteRead bool
	onLinkSet                                                            func()
}

func newDSLiteNativeFixture(t *testing.T, store func() Store) *dsliteNativeFixture {
	t.Helper()
	return &dsliteNativeFixture{t: t, store: store, links: map[string]dsliteTunnelObservation{}, nextIndex: 7}
}

func (f *dsliteNativeFixture) index(name string) (int, error) {
	link, exists := f.links[name]
	if !exists {
		return 0, errors.New("interface absent")
	}
	return link.Index, nil
}

func (f *dsliteNativeFixture) pendingBeforeWrite(line string) {
	f.t.Helper()
	reader := f.store().(ipv4RouteStatusReader)
	status, err := reader.ReadObjectStatus(api.RouterAPIVersion, "Router", dsliteNativeStatusName)
	var records []dsliteTunnelReceipt
	if err != nil || !decodeAppliedDataplaneStatus(status["tunnels"], &records) || len(records) != 1 || !records[0].Pending {
		f.t.Fatalf("native mutation %q preceded durable pending receipt: %+v %v", line, status, err)
	}
	f.writes = append(f.writes, line)
}

func (f *dsliteNativeFixture) setAliasByIndex(index int, name, proof string) error {
	link, exists := f.links[name]
	if !exists || link.Index != index || link.Kind != "ip6tnl" || link.Token != "" {
		return errors.New("acknowledged DS-Lite identity changed before cookie installation")
	}
	f.pendingBeforeWrite("netlink alias " + name)
	link.Token = strings.TrimPrefix(proof, "routerd-dslite:")
	f.links[name] = link
	return nil
}

func (f *dsliteNativeFixture) command(_ context.Context, name string, args ...string) ([]byte, error) {
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	value := func(key string) string {
		for i, arg := range args {
			if arg == key && i+1 < len(args) {
				return args[i+1]
			}
		}
		return ""
	}
	if line == "ip -j -d link show" {
		if f.failLinkRead {
			return nil, errors.New("native link observation unavailable")
		}
		links := []map[string]any{}
		for name, link := range f.links {
			flags := []string{}
			if link.Up {
				flags = append(flags, "UP")
			}
			data := map[string]any{"proto": "ipip6", "local": link.Local, "remote": link.Remote, "link": link.Underlay}
			if link.EncapLimit == "none" {
				data["ip6_tnl_f_ign_encap_limit"] = true
			} else {
				limit, _ := strconv.Atoi(link.EncapLimit)
				data["encap_limit"] = limit
			}
			links = append(links, map[string]any{"ifname": name, "ifindex": link.Index, "ifalias": "routerd-dslite:" + link.Token, "mtu": link.MTU, "flags": flags, "linkinfo": map[string]any{"info_kind": link.Kind, "info_data": data}})
		}
		return json.Marshal(links)
	}
	if len(args) > 3 && strings.Join(args[:3], " ") == "-j -4 addr" {
		link := f.links[value("dev")]
		if link.InnerLocal == "" {
			return []byte(`[]`), nil
		}
		entries := []map[string]any{}
		if link.InnerLocal != "" {
			entries = append(entries, map[string]any{"family": "inet", "local": link.InnerLocal, "prefixlen": 32, "address": dsliteInnerRemoteIPv4})
		}
		return json.Marshal([]map[string]any{{"addr_info": entries}})
	}
	if line == "ifconfig -l" {
		if f.failLinkRead {
			return nil, errors.New("native interface listing failed")
		}
		list := "wan0"
		for name := range f.links {
			list += " " + name
		}
		return []byte(list), nil
	}
	if name == "ifconfig" && len(args) == 1 {
		link, exists := f.links[args[0]]
		if !exists {
			return nil, errors.New("interface does not exist")
		}
		flags := "POINTOPOINT,MULTICAST"
		if link.Up {
			flags = "UP," + flags
		}
		out := fmt.Sprintf("%s: flags=8051<%s> metric 0 mtu %d\n\tdescription: routerd-dslite:%s\n\tgroups: %s\n", args[0], flags, link.MTU, link.Token, link.Kind)
		if link.Local != "" {
			out += fmt.Sprintf("\ttunnel inet6 %s --> %s\n", link.Local, link.Remote)
		}
		if link.InnerLocal != "" {
			out += fmt.Sprintf("\tinet %s --> %s netmask 0xffffffff\n", link.InnerLocal, dsliteInnerRemoteIPv4)
		}
		return []byte(out), nil
	}
	if line == "ip -j -d -4 route show default" {
		if f.failRouteRead {
			return nil, errors.New("route observation unavailable")
		}
		routes := []ordinaryIPv4RouteObservation{}
		if f.defaultRoute != nil {
			routes = append(routes, *f.defaultRoute)
		}
		return json.Marshal(routes)
	}
	if line == "route -n get -inet default" {
		if f.failRouteRead {
			return nil, errors.New("route observation unavailable")
		}
		if f.defaultRoute == nil {
			return []byte("route: route has not been found"), errors.New("not found")
		}
		return []byte(fmt.Sprintf("destination: default\ngateway: %s\ninterface: %s\nflags: <UP,GATEWAY,DONE,STATIC>\n", f.defaultRoute.Gateway, f.defaultRoute.Device)), nil
	}
	f.pendingBeforeWrite(line)
	if name == "ip" && len(args) > 1 && args[0] == "link" && args[1] == "add" || name == "ifconfig" && len(args) > 1 && args[1] == "create" {
		if f.failCreate {
			return nil, errors.New("create rejected")
		}
		native := value("name")
		if _, exists := f.links[native]; exists {
			return nil, errors.New("File exists")
		}
		f.nextIndex++
		link := dsliteTunnelObservation{Exists: true, Index: f.nextIndex, MTU: 1280, Kind: "ip6tnl", Local: value("local"), Remote: value("remote"), Underlay: value("dev"), EncapLimit: value("encaplimit")}
		if name == "ifconfig" {
			link.Kind, link.Token = "gif", strings.TrimPrefix(value("description"), "routerd-dslite:")
		} else {
			link.Index, _ = strconv.Atoi(value("index"))
			if link.Index < 1 {
				return nil, errors.New("DS-Lite creation did not request an exact index")
			}
		}
		f.links[native] = link
		if f.loseCreateReply {
			return nil, errors.New("create reply lost")
		}
		return nil, nil
	}
	if name == "ip" && len(args) > 1 && args[0] == "link" && args[1] == "delete" || name == "ifconfig" && len(args) == 2 && args[1] == "destroy" {
		if f.failDelete {
			return nil, errors.New("destroy rejected")
		}
		native := value("dev")
		if name == "ifconfig" {
			native = args[0]
		}
		delete(f.links, native)
		return nil, nil
	}
	if name == "ip" && len(args) > 1 && args[0] == "link" && args[1] == "set" {
		native := value("dev")
		link := f.links[native]
		link.MTU, _ = strconv.Atoi(value("mtu"))
		link.Up = true
		f.links[native] = link
		if f.onLinkSet != nil {
			action := f.onLinkSet
			f.onLinkSet = nil
			action()
		}
		return nil, nil
	}
	if name == "ip" && len(args) > 2 && args[0] == "-4" && args[1] == "addr" && args[2] == "replace" {
		native := value("dev")
		link := f.links[native]
		link.InnerLocal = strings.TrimSuffix(args[3], "/32")
		f.links[native] = link
		return nil, nil
	}
	if name == "ifconfig" && len(args) > 2 {
		link := f.links[args[0]]
		switch args[1] {
		case "inet6":
			link.Local, link.Remote = args[3], args[4]
		case "inet":
			link.InnerLocal = args[2]
		case "mtu":
			link.MTU, _ = strconv.Atoi(args[2])
			link.Up = true
		default:
			f.t.Fatalf("unexpected FreeBSD write %q", line)
		}
		f.links[args[0]] = link
		return nil, nil
	}
	if name == "ip" && len(args) > 2 && args[0] == "-4" && args[1] == "route" || name == "route" && len(args) > 2 && args[0] == "-n" {
		action := args[2]
		if name == "route" {
			action = args[1]
		}
		if action == "del" || action == "delete" {
			f.defaultRoute = nil
			return nil, nil
		}
		if f.defaultRoute != nil {
			return nil, errors.New("File exists")
		}
		metric, _ := strconv.Atoi(value("metric"))
		native := value("dev")
		if name == "route" {
			native = value("-ifp")
		}
		f.defaultRoute = &ordinaryIPv4RouteObservation{Destination: "default", Gateway: dsliteInnerRemoteIPv4, Device: native, Protocol: "boot", Metric: metric}
		return nil, nil
	}
	f.t.Fatalf("unexpected native command %q", line)
	return nil, errors.New("unexpected command")
}

func newDSLiteNativeController(t *testing.T, osName platform.OS) (DSLiteTunnelController, *dsliteNativeFixture, *dsliteNativeStore) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	s := &dsliteNativeStore{SQLiteStore: store, path: path}
	f := newDSLiteNativeFixture(t, func() Store { return s })
	spec := api.DSLiteTunnelSpec{Interface: "wan", TunnelName: "ds-owned", LocalAddressSource: "static", LocalAddress: "2001:db8::10", AFTRIPv6: "2001:db8::20", DefaultRoute: true, MTU: 1454}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "wan0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds"}, Spec: spec},
	}}}
	scope := "linux:boot-a:17"
	if osName == platform.OSFreeBSD {
		scope = "freebsd:10:20:0"
	}
	c := DSLiteTunnelController{Router: router, Store: s, OS: osName, NativeScope: func(context.Context) (string, error) { return scope, nil }, NativeInterfaceIndex: f.index, CommandOutput: f.command, SetAliasByIndex: f.setAliasByIndex}
	return c, f, s
}

func TestDSLiteNativeApplyWithdrawRestartUsesOneOwner(t *testing.T) {
	for _, osName := range []platform.OS{platform.OSLinux, platform.OSFreeBSD} {
		t.Run(string(osName), func(t *testing.T) {
			c, f, store := newDSLiteNativeController(t, osName)
			if err := c.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			records, err := c.nativeTunnelReceipts()
			if err != nil || len(records) != 1 || records[0].Pending || !records[0].OwnDefault || records[0].IfIndex == 0 {
				t.Fatalf("applied receipt=%+v err=%v", records, err)
			}
			if status := store.ObjectStatus(api.NetAPIVersion, "DSLiteTunnel", "ds"); status["phase"] != "Up" {
				t.Fatalf("status=%+v", status)
			}
			before := len(f.writes)
			c.Router = &api.Router{}
			if err := c.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			records, err = c.nativeTunnelReceipts()
			if err != nil || len(records) != 0 || len(f.links) != 0 || f.defaultRoute != nil {
				t.Fatalf("withdrawal residue: records=%+v native=%+v route=%+v err=%v", records, f.links, f.defaultRoute, err)
			}
			joined := strings.Join(f.writes[before:], "\n")
			if osName == platform.OSFreeBSD && (strings.Contains(joined, "ip ") || !strings.Contains(joined, "destroy")) {
				t.Fatalf("wrong platform withdrawal: %s", joined)
			}
		})
	}
}

func TestDSLiteNativeNeverAdoptsOrDestroysForeignTunnel(t *testing.T) {
	for _, osName := range []platform.OS{platform.OSLinux, platform.OSFreeBSD} {
		t.Run(string(osName), func(t *testing.T) {
			c, f, _ := newDSLiteNativeController(t, osName)
			name := "ds-owned"
			kind := "ip6tnl"
			if osName == platform.OSFreeBSD {
				name, kind = api.DSLiteRuntimeIfName(name, "freebsd"), "gif"
			}
			foreign := dsliteTunnelObservation{Exists: true, Index: 12, Kind: kind, MTU: 1454, Local: "2001:db8::10", Remote: "2001:db8::20", Underlay: "wan0", EncapLimit: "none", InnerLocal: dsliteDefaultInnerLocalIPv4, Up: true}
			f.links[name] = foreign
			if err := c.reconcile(t.Context()); err == nil {
				t.Fatal("foreign matching tunnel adopted")
			}
			if !reflect.DeepEqual(f.links[name], foreign) || len(f.writes) != 0 {
				t.Fatalf("foreign link mutated: %+v %v", f.links[name], f.writes)
			}
			foreign.Remote = "2001:db8::ffff"
			f.links[name] = foreign
			if err := c.reconcile(t.Context()); err == nil || len(f.writes) != 0 {
				t.Fatalf("foreign mismatching tunnel destroyed: %v %v", err, f.writes)
			}
		})
	}
}

func TestDSLiteNativeFailedCleanupAndPublicationRetainRecovery(t *testing.T) {
	c, f, store := newDSLiteNativeController(t, platform.OSLinux)
	store.failPending = true
	if err := c.reconcile(t.Context()); err == nil || len(f.writes) != 0 {
		t.Fatalf("effect escaped failed pending save: %v %v", err, f.writes)
	}
	store.failPending = false
	store.failComplete = true
	if err := c.reconcile(t.Context()); err == nil {
		t.Fatal("completion failure reported success")
	}
	records, _ := c.nativeTunnelReceipts()
	if len(records) != 1 || !records[0].Pending {
		t.Fatalf("lost pending receipt: %+v", records)
	}
	store.failComplete = false
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := routerstate.OpenSQLite(store.path)
	if err != nil {
		t.Fatal(err)
	}
	store.SQLiteStore = reopened
	t.Cleanup(func() { _ = reopened.Close() })
	restarted := DSLiteTunnelController{Router: c.Router, Store: store, OS: c.OS, NativeScope: c.NativeScope, NativeInterfaceIndex: c.NativeInterfaceIndex, CommandOutput: c.CommandOutput, SetAliasByIndex: c.SetAliasByIndex}
	if err := restarted.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.failDelete = true
	restarted.Router = &api.Router{}
	if err := restarted.reconcile(t.Context()); err == nil {
		t.Fatal("failed destroy reported success")
	}
	records, _ = restarted.nativeTunnelReceipts()
	if len(records) != 1 || !records[0].Pending || len(f.links) != 1 {
		t.Fatalf("failed cleanup lost owner: %+v %+v", records, f.links)
	}
	f.failDelete = false
	if err := restarted.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	records, _ = restarted.nativeTunnelReceipts()
	if len(records) != 0 {
		t.Fatalf("cleanup retry did not finish: %+v", records)
	}
}

func TestDSLiteNativeAmbiguousCreateCannotRetagBareLink(t *testing.T) {
	c, f, store := newDSLiteNativeController(t, platform.OSLinux)
	f.loseCreateReply = true
	if err := c.reconcile(t.Context()); err == nil {
		t.Fatal("lost creation result acknowledged")
	}
	f.loseCreateReply = false
	before := len(f.writes)
	restarted := DSLiteTunnelController{Router: c.Router, Store: store, OS: c.OS, NativeScope: c.NativeScope, CommandOutput: c.CommandOutput, SetAliasByIndex: c.SetAliasByIndex}
	if err := restarted.reconcile(t.Context()); err == nil || len(f.writes) != before {
		t.Fatalf("ambiguous cookieless create was retagged or mutated after restart: %v writes=%v", err, f.writes[before:])
	}
	creates := 0
	for _, write := range f.writes {
		if strings.Contains(write, "link add") {
			creates++
		}
	}
	if creates != 1 {
		t.Fatalf("pending token recovery duplicated creation: %v", f.writes)
	}
	if receipts, err := c.nativeTunnelReceipts(); err != nil || len(receipts) != 1 || !receipts[0].Pending || f.links["ds-owned"].Token != "" {
		t.Fatalf("ambiguous creation lost its unresolved receipt: %+v err=%v", receipts, err)
	}
}

func TestDSLiteNativeReplacementBeforeCookieIsNotAdopted(t *testing.T) {
	c, f, _ := newDSLiteNativeController(t, platform.OSLinux)
	c.SetAliasByIndex = func(index int, name, proof string) error {
		link := f.links[name]
		link.Index++
		f.links[name] = link
		return f.setAliasByIndex(index, name, proof)
	}
	if err := c.reconcile(t.Context()); err == nil {
		t.Fatal("same-name replacement received the pending creation cookie")
	}
	before := len(f.writes)
	if err := c.reconcile(t.Context()); err == nil || len(f.writes) != before || f.links["ds-owned"].Token != "" {
		t.Fatalf("replacement was retagged or destroyed: %v writes=%v", err, f.writes[before:])
	}
}

func TestDSLiteNativeChangedTupleWithdrawsExactOldOwner(t *testing.T) {
	c, f, _ := newDSLiteNativeController(t, platform.OSLinux)
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	resource := c.Router.Spec.Resources[1]
	spec := resource.Spec.(api.DSLiteTunnelSpec)
	spec.TunnelName = "ds-renamed"
	spec.AFTRIPv6 = "2001:db8::30"
	resource.Spec = spec
	c.Router.Spec.Resources[1] = resource
	f.failDelete = true
	if err := c.reconcile(t.Context()); err == nil || len(f.links) != 1 || !f.links["ds-owned"].Exists {
		t.Fatalf("failed cleanup replaced old owner: %+v %v", f.links, err)
	}
	f.failDelete = false
	before := len(f.writes)
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.links) != 1 || !f.links["ds-renamed"].Exists {
		t.Fatalf("old interface leaked: %+v", f.links)
	}
	writes := strings.Join(f.writes[before:], "\n")
	if strings.Index(writes, "link delete dev ds-owned") > strings.Index(writes, "link add name ds-renamed") {
		t.Fatalf("replacement preceded withdrawal: %s", writes)
	}
}

func TestDSLiteNativeLostPremiseAndUnknownReadsPreserveForeignState(t *testing.T) {
	c, f, store := newDSLiteNativeController(t, platform.OSLinux)
	store.afterPending = func() {
		f.links["ds-owned"] = dsliteTunnelObservation{Exists: true, Index: 99, Kind: "ip6tnl", MTU: 1280}
	}
	if err := c.reconcile(t.Context()); err == nil || len(f.writes) != 0 {
		t.Fatalf("post-wait foreign identity mutated: %v %v", err, f.writes)
	}
	store.failRead = true
	if err := c.reconcile(t.Context()); err == nil || len(f.writes) != 0 {
		t.Fatalf("unknown journal authorized effects: %v %v", err, f.writes)
	}
}

func TestDSLiteNativeDryRunDoesNotAcknowledgeOrEraseLiveOwner(t *testing.T) {
	c, f, _ := newDSLiteNativeController(t, platform.OSLinux)
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, _ := c.nativeTunnelReceipts()
	writes := len(f.writes)
	c.DryRun = true
	c.Router = &api.Router{}
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, _ := c.nativeTunnelReceipts()
	if !reflect.DeepEqual(before, after) || len(f.writes) != writes {
		t.Fatalf("dry run changed live native receipt: %+v -> %+v %v", before, after, f.writes)
	}
}

func TestDSLiteNativeInactiveInputsWithdrawPriorEffect(t *testing.T) {
	for _, inactive := range []string{"disabled", "dependency", "source-missing", "aftr-missing", "when-false", "removed"} {
		t.Run(inactive, func(t *testing.T) {
			c, f, _ := newDSLiteNativeController(t, platform.OSLinux)
			if err := c.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			resource := c.Router.Spec.Resources[1]
			spec := resource.Spec.(api.DSLiteTunnelSpec)
			switch inactive {
			case "disabled":
				disabled := false
				spec.Enabled = &disabled
			case "dependency":
				spec.DependsOn = []api.ResourceDependencySpec{{Resource: "Interface/missing", Phase: "Up"}}
			case "source-missing":
				spec.LocalAddressSource, spec.LocalDelegatedAddress = "delegatedAddress", "missing"
			case "aftr-missing":
				spec.AFTRIPv6 = ""
			case "when-false":
				c.DeclaredRouter = c.Router
				c.Router = &api.Router{}
			case "removed":
				c.Router = &api.Router{}
			}
			if inactive != "when-false" && inactive != "removed" {
				resource.Spec = spec
				c.Router.Spec.Resources[1] = resource
			}
			if err := c.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			records, err := c.nativeTunnelReceipts()
			if err != nil || len(records) != 0 || len(f.links) != 0 || f.defaultRoute != nil {
				t.Fatalf("inactive effects retained: records=%+v links=%+v route=%+v err=%v", records, f.links, f.defaultRoute, err)
			}
		})
	}
}

func TestDSLiteNativeScopeAndIndexPreventForeignWithdrawal(t *testing.T) {
	for _, changed := range []string{"index", "token", "namespace"} {
		t.Run(changed, func(t *testing.T) {
			c, f, _ := newDSLiteNativeController(t, platform.OSLinux)
			if err := c.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			link := f.links["ds-owned"]
			switch changed {
			case "index":
				link.Index++
			case "token":
				link.Token = strings.Repeat("a", 48)
			case "namespace":
				c.NativeScope = func(context.Context) (string, error) { return "linux:boot-a:18", nil }
			}
			f.links["ds-owned"] = link
			writes := len(f.writes)
			c.Router = &api.Router{}
			if err := c.reconcile(t.Context()); err == nil || len(f.writes) != writes {
				t.Fatalf("changed identity was mutated: %v %v", err, f.writes[writes:])
			}
			records, _ := c.nativeTunnelReceipts()
			if len(records) != 1 {
				t.Fatalf("unknown owner receipt lost: %+v", records)
			}
		})
	}
}

func TestDSLiteNativeRebootDoesNotTreatOldReceiptAsCurrentOwnership(t *testing.T) {
	c, f, _ := newDSLiteNativeController(t, platform.OSLinux)
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, _ := c.nativeTunnelReceipts()
	f.links = map[string]dsliteTunnelObservation{}
	f.defaultRoute = nil
	c.NativeScope = func(context.Context) (string, error) { return "linux:boot-b:17", nil }
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, _ := c.nativeTunnelReceipts()
	if len(after) != 1 || after[0].Token == before[0].Token || after[0].Scope != "linux:boot-b:17" {
		t.Fatalf("reboot reused stale native authority: %+v -> %+v", before, after)
	}
}

func TestDSLiteNativeDefaultRouteNeverReplacesOrAdoptsForeignRoute(t *testing.T) {
	for _, matching := range []bool{false, true} {
		t.Run(strconv.FormatBool(matching), func(t *testing.T) {
			c, f, _ := newDSLiteNativeController(t, platform.OSLinux)
			foreign := ordinaryIPv4RouteObservation{Destination: "default", Device: "other0", Gateway: "192.0.2.1", Protocol: "bgp"}
			if matching {
				foreign.Device, foreign.Gateway, foreign.Protocol = "ds-owned", dsliteInnerRemoteIPv4, "boot"
			}
			f.defaultRoute = &foreign
			if err := c.reconcile(t.Context()); err == nil {
				t.Fatal("foreign route accepted as owned")
			}
			c.Router = &api.Router{}
			if err := c.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if f.defaultRoute == nil || *f.defaultRoute != foreign {
				t.Fatalf("foreign route changed: %+v", f.defaultRoute)
			}
			for _, write := range f.writes {
				if strings.Contains(write, "route ") {
					t.Fatalf("foreign default mutated: %v", f.writes)
				}
			}
		})
	}
}

func TestDSLiteNativeReadbackAndReadFailureNeverPublishUp(t *testing.T) {
	c, f, store := newDSLiteNativeController(t, platform.OSLinux)
	f.onLinkSet = func() { link := f.links["ds-owned"]; link.MTU = 1280; f.links["ds-owned"] = link }
	if err := c.reconcile(t.Context()); err == nil {
		t.Fatal("bad native MTU acknowledged")
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "DSLiteTunnel", "ds"); status["phase"] == "Up" {
		t.Fatalf("bad readback published Up: %+v", status)
	}
	before, _ := c.nativeTunnelReceipts()
	writes := len(f.writes)
	f.failLinkRead = true
	if err := c.reconcile(t.Context()); err == nil || len(f.writes) != writes {
		t.Fatalf("unknown observation authorized mutation: %v %v", err, f.writes[writes:])
	}
	after, _ := c.nativeTunnelReceipts()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("read failure erased receipt: %+v -> %+v", before, after)
	}
	f.failLinkRead = false
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestDSLiteNativeDriftAndSameNameTupleChangeRecreateOnlyOwnedLink(t *testing.T) {
	c, f, _ := newDSLiteNativeController(t, platform.OSLinux)
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	oldIndex := f.links["ds-owned"].Index
	link := f.links["ds-owned"]
	link.Local = "2001:db8::bad"
	f.links["ds-owned"] = link
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if current := f.links["ds-owned"]; current.Local != "2001:db8::10" || current.Index == oldIndex {
		t.Fatalf("owned drift did not reconverge: %+v", current)
	}
	resource := c.Router.Spec.Resources[1]
	spec := resource.Spec.(api.DSLiteTunnelSpec)
	spec.AFTRIPv6 = "2001:db8::30"
	resource.Spec = spec
	c.Router.Spec.Resources[1] = resource
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if current := f.links["ds-owned"]; current.Remote != "2001:db8::30" {
		t.Fatalf("same-name desired tuple ignored: %+v", current)
	}
}

func TestDSLiteNativeTruncatedFreeBSDObservationRetainsReceipt(t *testing.T) {
	c, f, _ := newDSLiteNativeController(t, platform.OSFreeBSD)
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	before := len(f.writes)
	name := api.DSLiteRuntimeIfName("ds-owned", "freebsd")
	c.CommandOutput = func(ctx context.Context, command string, args ...string) ([]byte, error) {
		if command == "ifconfig" && reflect.DeepEqual(args, []string{name}) {
			return []byte(name + ":\n"), nil
		}
		return f.command(ctx, command, args...)
	}
	c.Router = &api.Router{}
	if err := c.reconcile(t.Context()); err == nil || len(f.writes) != before {
		t.Fatalf("truncated native read allowed mutation: %v %v", err, f.writes[before:])
	}
	if receipts, err := c.nativeTunnelReceipts(); err != nil || len(receipts) != 1 {
		t.Fatalf("unknown native read lost receipt: %+v %v", receipts, err)
	}
}

func TestDSLiteNativeFreeBSDProtocolRouteReplacementIsForeign(t *testing.T) {
	for _, flag := range []string{"PROTO1", "PROTO2", "BLACKHOLE", "REJECT"} {
		t.Run(flag, func(t *testing.T) {
			c, f, _ := newDSLiteNativeController(t, platform.OSFreeBSD)
			if err := c.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			receipts, _ := c.nativeTunnelReceipts()
			before := len(f.writes)
			c.CommandOutput = func(ctx context.Context, command string, args ...string) ([]byte, error) {
				if command == "route" && strings.Join(args, " ") == "-n get -inet default" {
					return []byte(fmt.Sprintf("destination: default\ngateway: 192.0.0.1\ninterface: %s\nflags: <UP,GATEWAY,STATIC,%s>\n", receipts[0].Plan.Name, flag)), nil
				}
				return f.command(ctx, command, args...)
			}
			if err := c.withdrawTunnelDefault(t.Context(), receipts[0]); err != nil || len(f.writes) != before {
				t.Fatalf("foreign protocol route deleted: %v %v", err, f.writes[before:])
			}
		})
	}
}

func TestDSLiteNativePendingFenceRechecksDependencyBeforeCreate(t *testing.T) {
	c, f, store := newDSLiteNativeController(t, platform.OSLinux)
	resource := c.Router.Spec.Resources[1]
	spec := resource.Spec.(api.DSLiteTunnelSpec)
	spec.DependsOn = []api.ResourceDependencySpec{{Resource: "Interface/guard", Phase: "Up"}}
	resource.Spec = spec
	c.Router.Spec.Resources[1] = resource
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "guard", map[string]any{"phase": "Up"}); err != nil {
		t.Fatal(err)
	}
	store.afterPending = func() {
		if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "guard", map[string]any{"phase": "Down"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 0 {
		t.Fatalf("native create escaped changed dependency after pending save: %v", f.writes)
	}
	if receipts, err := c.nativeTunnelReceipts(); err != nil || len(receipts) != 0 {
		t.Fatalf("unstarted native acquisition retained authority: %+v %v", receipts, err)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "DSLiteTunnel", "ds"); status["phase"] != "Pending" {
		t.Fatalf("changed dependency acknowledged: %+v", status)
	}
}

func TestDSLiteNativeChangedInputWithdrawalFailurePropagates(t *testing.T) {
	c, f, store := newDSLiteNativeController(t, platform.OSLinux)
	resource := c.Router.Spec.Resources[1]
	spec := resource.Spec.(api.DSLiteTunnelSpec)
	spec.DependsOn = []api.ResourceDependencySpec{{Resource: "Interface/guard", Phase: "Up"}}
	resource.Spec = spec
	c.Router.Spec.Resources[1] = resource
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "guard", map[string]any{"phase": "Up"}); err != nil {
		t.Fatal(err)
	}
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	store.afterPending = func() {
		f.failDelete = true
		if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "guard", map[string]any{"phase": "Down"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.reconcile(t.Context()); err == nil {
		t.Fatal("input-change cleanup failure was suppressed")
	}
	if receipts, err := c.nativeTunnelReceipts(); err != nil || len(receipts) != 1 || !receipts[0].Pending {
		t.Fatalf("failed input-change cleanup lost identity: %+v %v", receipts, err)
	}
}

func TestDSLiteNativeFreeBSDInterruptedUntaggedCreateRemainsUnknown(t *testing.T) {
	c, f, _ := newDSLiteNativeController(t, platform.OSFreeBSD)
	name := api.DSLiteRuntimeIfName("ds-owned", "freebsd")
	c.CommandOutput = func(ctx context.Context, command string, args ...string) ([]byte, error) {
		if command == "ifconfig" && len(args) > 1 && args[1] == "create" {
			if _, err := f.command(ctx, command, args...); err != nil {
				return nil, err
			}
			link := f.links[name]
			link.Token = ""
			f.links[name] = link
			return nil, context.DeadlineExceeded
		}
		return f.command(ctx, command, args...)
	}
	if err := c.reconcile(t.Context()); err == nil {
		t.Fatal("untagged ambiguous create acknowledged")
	}
	before := len(f.writes)
	c.CommandOutput = f.command
	if err := c.reconcile(t.Context()); err == nil || len(f.writes) != before {
		t.Fatalf("pending receipt adopted or destroyed untagged native link: %v %v", err, f.writes[before:])
	}
	if receipts, err := c.nativeTunnelReceipts(); err != nil || len(receipts) != 1 || !receipts[0].Pending {
		t.Fatalf("unknown creation lost durable intent: %+v %v", receipts, err)
	}
}

func TestDSLiteNativeDefaultRoutePreferredSourceReplacementIsForeign(t *testing.T) {
	c, f, _ := newDSLiteNativeController(t, platform.OSLinux)
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	receipts, _ := c.nativeTunnelReceipts()
	f.defaultRoute.PreferredSource = "192.0.2.10"
	before := len(f.writes)
	if err := c.withdrawTunnelDefault(t.Context(), receipts[0]); err != nil || len(f.writes) != before {
		t.Fatalf("foreign source-bound route deleted: %v %v", err, f.writes[before:])
	}
	if err := c.reconcile(t.Context()); err == nil {
		t.Fatal("foreign preferred-source replacement acknowledged as default owner")
	}
	if f.defaultRoute == nil || f.defaultRoute.PreferredSource != "192.0.2.10" {
		t.Fatalf("foreign route changed: %+v", f.defaultRoute)
	}
}
