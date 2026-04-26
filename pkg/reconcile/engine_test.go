package reconcile

import (
	"fmt"
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/config"
	"routerd/pkg/resource"
)

func TestPlanUsesNetplanForManagedInterfaceWhenAvailable(t *testing.T) {
	router, err := config.Load("../../examples/router-lab.yaml")
	if err != nil {
		t.Fatalf("load example: %v", err)
	}

	engine := &Engine{
		Command: fakeCommand(map[string]string{
			"hostname":                         "old-router\n",
			"sysctl -n net.ipv4.ip_forward":    "0\n",
			"ip -4 route show default":         "default via 192.168.1.1 dev ens18 proto dhcp src 192.168.1.10 metric 100\n",
			"ip -brief link show dev ens18":    "ens18 UP 52:54:00:00:00:18 <BROADCAST,MULTICAST,UP,LOWER_UP>\n",
			"ip -brief link show dev ens19":    "ens19 DOWN 52:54:00:00:00:19 <BROADCAST,MULTICAST>\n",
			"ip -brief -4 addr show dev ens19": "",
		}),
		OSNetworking: &osNetworking{CloudInit: true, Netplan: true, Networkd: true},
	}

	result, err := engine.Plan(router)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if result.Phase != "Healthy" {
		t.Fatalf("phase = %s, want Healthy", result.Phase)
	}

	lan := findResult(result, "net.routerd.net/v1alpha1/Interface/lan")
	if lan == nil {
		t.Fatal("missing lan result")
	}
	if lan.Phase != "Healthy" {
		t.Fatalf("lan phase = %s, want Healthy", lan.Phase)
	}

	static := findResult(result, "net.routerd.net/v1alpha1/IPv4StaticAddress/lan-ipv4")
	if static == nil {
		t.Fatal("missing lan static result")
	}
	if static.Phase != "Drifted" {
		t.Fatalf("static phase = %s, want Drifted", static.Phase)
	}
	if got := strings.Join(static.Plan, "\n"); !strings.Contains(got, "ensure IPv4 address") {
		t.Fatalf("static plan = %q, want address ensure", got)
	}
}

func TestPlanBlocksManagedInterfaceWhenCloudInitOwnsNetworking(t *testing.T) {
	router, err := config.Load("../../examples/router-lab.yaml")
	if err != nil {
		t.Fatalf("load example: %v", err)
	}

	engine := &Engine{
		Command: fakeCommand(map[string]string{
			"hostname":                         "old-router\n",
			"sysctl -n net.ipv4.ip_forward":    "0\n",
			"ip -4 route show default":         "default via 192.168.1.1 dev ens18 proto dhcp src 192.168.1.10 metric 100\n",
			"ip -brief link show dev ens18":    "ens18 UP 52:54:00:00:00:18 <BROADCAST,MULTICAST,UP,LOWER_UP>\n",
			"ip -brief link show dev ens19":    "ens19 DOWN 52:54:00:00:00:19 <BROADCAST,MULTICAST>\n",
			"ip -brief -4 addr show dev ens19": "",
		}),
		OSNetworking: &osNetworking{CloudInit: true, Netplan: false, Networkd: true},
	}

	result, err := engine.Plan(router)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	lan := findResult(result, "net.routerd.net/v1alpha1/Interface/lan")
	if lan == nil {
		t.Fatal("missing lan result")
	}
	if lan.Phase != "RequiresAdoption" {
		t.Fatalf("lan phase = %s, want RequiresAdoption", lan.Phase)
	}
}

func TestPlanKeepsExternalWANObserveOnly(t *testing.T) {
	router, err := config.Load("../../examples/router-lab.yaml")
	if err != nil {
		t.Fatalf("load example: %v", err)
	}

	engine := &Engine{
		Command: fakeCommand(map[string]string{
			"hostname":                         "router.example\n",
			"sysctl -n net.ipv4.ip_forward":    "1\n",
			"ip -4 route show default":         "default via 192.168.1.1 dev ens18 proto dhcp src 192.168.1.10 metric 100\n",
			"ip -brief link show dev ens18":    "ens18 UP 52:54:00:00:00:18 <BROADCAST,MULTICAST,UP,LOWER_UP>\n",
			"ip -brief link show dev ens19":    "ens19 DOWN 52:54:00:00:00:19 <BROADCAST,MULTICAST>\n",
			"ip -brief -4 addr show dev ens19": "",
		}),
		OSNetworking: &osNetworking{CloudInit: true, Netplan: true, Networkd: true},
	}

	result, err := engine.Plan(router)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	wanDHCP := findResult(result, "net.routerd.net/v1alpha1/IPv4DHCPAddress/wan-dhcp4")
	if wanDHCP == nil {
		t.Fatal("missing wan dhcp result")
	}
	if got := strings.Join(wanDHCP.Plan, "\n"); !strings.Contains(got, "observe only") {
		t.Fatalf("wan dhcp plan = %q, want observe only", got)
	}
}

func TestPlanIPv4DefaultRoutePolicy(t *testing.T) {
	router, err := config.Load("../../examples/router-lab.yaml")
	if err != nil {
		t.Fatalf("load example: %v", err)
	}

	engine := &Engine{
		Command: fakeCommand(map[string]string{
			"hostname":                         "router.example\n",
			"sysctl -n net.ipv4.ip_forward":    "1\n",
			"ip -4 route show default":         "default via 192.168.1.1 dev ens18 proto dhcp src 192.168.1.10 metric 100\n",
			"ip -brief link show dev ens18":    "ens18 UP 52:54:00:00:00:18 <BROADCAST,MULTICAST,UP,LOWER_UP>\n",
			"ip -brief link show dev ens19":    "ens19 DOWN 52:54:00:00:00:19 <BROADCAST,MULTICAST>\n",
			"ip -brief -4 addr show dev ens18": "192.168.1.10/24\n",
			"ip -brief -4 addr show dev ens19": "",
		}),
		OSNetworking: &osNetworking{CloudInit: true, Netplan: true, Networkd: true},
	}

	result, err := engine.Plan(router)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	route := findResult(result, "net.routerd.net/v1alpha1/IPv4DefaultRoutePolicy/default-v4")
	if route == nil {
		t.Fatal("missing default route result")
	}
	if route.Observed["currentGateway"] != "192.168.1.1" {
		t.Fatalf("currentGateway = %q, want 192.168.1.1", route.Observed["currentGateway"])
	}
	if route.Observed["currentIfname"] != "ens18" {
		t.Fatalf("currentIfname = %q, want ens18", route.Observed["currentIfname"])
	}
	if got := strings.Join(route.Plan, "\n"); !strings.Contains(got, "first healthy IPv4 default route candidate") {
		t.Fatalf("route plan = %q, want policy selection", got)
	}
}

func TestPlanIPv4SourceNAT(t *testing.T) {
	router, err := config.Load("../../examples/router-lab.yaml")
	if err != nil {
		t.Fatalf("load example: %v", err)
	}

	engine := &Engine{
		Command: fakeCommand(map[string]string{
			"hostname":                         "router.example\n",
			"sysctl -n net.ipv4.ip_forward":    "1\n",
			"ip -4 route show default":         "default via 192.168.1.1 dev ens18 proto dhcp src 192.168.1.10 metric 100\n",
			"ip -brief link show dev ens18":    "ens18 UP 52:54:00:00:00:18 <BROADCAST,MULTICAST,UP,LOWER_UP>\n",
			"ip -brief link show dev ens19":    "ens19 DOWN 52:54:00:00:00:19 <BROADCAST,MULTICAST>\n",
			"ip -brief -4 addr show dev ens18": "192.168.1.10/24\n",
			"ip -brief -4 addr show dev ens19": "",
		}),
		OSNetworking: &osNetworking{CloudInit: true, Netplan: true, Networkd: true},
	}

	result, err := engine.Plan(router)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	nat := findResult(result, "net.routerd.net/v1alpha1/IPv4SourceNAT/lan-to-wan")
	if nat == nil {
		t.Fatal("missing source NAT result")
	}
	if nat.Observed["translationType"] != "interfaceAddress" {
		t.Fatalf("translationType = %q, want interfaceAddress", nat.Observed["translationType"])
	}
	if got := strings.Join(nat.Plan, "\n"); !strings.Contains(got, "source NAT") {
		t.Fatalf("NAT plan = %q, want source NAT", got)
	}
	if nat.Observed["portMapping"] != "range" {
		t.Fatalf("portMapping = %q, want range", nat.Observed["portMapping"])
	}
	if nat.Observed["portRange"] != "1024-65535" {
		t.Fatalf("portRange = %q, want 1024-65535", nat.Observed["portRange"])
	}
}

func TestPlanStaticDefaultRoutePolicy(t *testing.T) {
	router := staticDefaultRouteRouter()
	engine := &Engine{
		Command: fakeCommand(map[string]string{
			"ip -4 route show default":         "default via 192.168.1.1 dev ens18 proto static metric 100\n",
			"ip -brief link show dev ens18":    "ens18 UP 52:54:00:00:00:18 <BROADCAST,MULTICAST,UP,LOWER_UP>\n",
			"ip -brief -4 addr show dev ens18": "192.168.1.10/24\n",
		}),
		OSNetworking: &osNetworking{},
	}

	result, err := engine.Plan(router)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	route := findResult(result, "net.routerd.net/v1alpha1/IPv4DefaultRoutePolicy/default-v4")
	if route == nil {
		t.Fatal("missing default route result")
	}
	if got := strings.Join(route.Plan, "\n"); !strings.Contains(got, "gatewaySource=static") {
		t.Fatalf("route plan = %q, want static candidate", got)
	}
}

func TestPlanAllowsDHCPScopeWhenNetplanCanManageInterface(t *testing.T) {
	router, err := config.Load("../../examples/router-lab.yaml")
	if err != nil {
		t.Fatalf("load example: %v", err)
	}

	engine := &Engine{
		Command: fakeCommand(map[string]string{
			"hostname":                         "router.example\n",
			"sysctl -n net.ipv4.ip_forward":    "1\n",
			"ip -4 route show default":         "default via 192.168.1.1 dev ens18 proto dhcp src 192.168.1.10 metric 100\n",
			"ip -brief link show dev ens18":    "ens18 UP 52:54:00:00:00:18 <BROADCAST,MULTICAST,UP,LOWER_UP>\n",
			"ip -brief link show dev ens19":    "ens19 DOWN 52:54:00:00:00:19 <BROADCAST,MULTICAST>\n",
			"ip -brief -4 addr show dev ens18": "192.168.1.10/24\n",
			"ip -brief -4 addr show dev ens19": "",
		}),
		OSNetworking: &osNetworking{CloudInit: true, Netplan: true, Networkd: true},
	}

	result, err := engine.Plan(router)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	scope := findResult(result, "net.routerd.net/v1alpha1/IPv4DHCPScope/lan-dhcp4")
	if scope == nil {
		t.Fatal("missing DHCP scope result")
	}
	if scope.Phase != "Healthy" {
		t.Fatalf("DHCP scope phase = %s, want Healthy", scope.Phase)
	}
	if got := strings.Join(scope.Plan, "\n"); !strings.Contains(got, "ensure IPv4 DHCP scope") {
		t.Fatalf("DHCP scope plan = %q, want scope ensure", got)
	}
}

func TestPlanSysctlDrift(t *testing.T) {
	router, err := config.Load("../../examples/router-lab.yaml")
	if err != nil {
		t.Fatalf("load example: %v", err)
	}

	engine := &Engine{
		Command: fakeCommand(map[string]string{
			"hostname":                         "router.example\n",
			"sysctl -n net.ipv4.ip_forward":    "0\n",
			"ip -4 route show default":         "default via 192.168.1.1 dev ens18 proto dhcp src 192.168.1.10 metric 100\n",
			"ip -brief link show dev ens18":    "ens18 UP 52:54:00:00:00:18 <BROADCAST,MULTICAST,UP,LOWER_UP>\n",
			"ip -brief link show dev ens19":    "ens19 DOWN 52:54:00:00:00:19 <BROADCAST,MULTICAST>\n",
			"ip -brief -4 addr show dev ens18": "192.168.1.2/24\n",
			"ip -brief -4 addr show dev ens19": "",
		}),
		OSNetworking: &osNetworking{},
	}

	result, err := engine.Plan(router)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	sysctl := findResult(result, "system.routerd.net/v1alpha1/Sysctl/ipv4-forwarding")
	if sysctl == nil {
		t.Fatal("missing sysctl result")
	}
	if sysctl.Phase != "Drifted" {
		t.Fatalf("sysctl phase = %s, want Drifted", sysctl.Phase)
	}
	if got := strings.Join(sysctl.Plan, "\n"); !strings.Contains(got, "net.ipv4.ip_forward=1") {
		t.Fatalf("sysctl plan = %q, want forwarding ensure", got)
	}
}

func TestPlanBlocksOverlappingObservedWANAndLANStatic(t *testing.T) {
	router := overlapRouter(false)
	engine := &Engine{
		Command: fakeCommand(map[string]string{
			"hostname":                         "router.example\n",
			"ip -brief link show dev ens18":    "ens18 UP 52:54:00:00:00:18 <BROADCAST,MULTICAST,UP,LOWER_UP>\n",
			"ip -brief link show dev ens19":    "ens19 UP 52:54:00:00:00:19 <BROADCAST,MULTICAST,UP,LOWER_UP>\n",
			"ip -brief -4 addr show dev ens18": "ens18 UP 192.168.160.20/24\n",
			"ip -brief -4 addr show dev ens19": "",
		}),
		OSNetworking: &osNetworking{},
	}

	result, err := engine.Plan(router)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if result.Phase != "Blocked" {
		t.Fatalf("phase = %s, want Blocked", result.Phase)
	}

	static := findResult(result, "net.routerd.net/v1alpha1/IPv4StaticAddress/lan-ipv4")
	if static == nil {
		t.Fatal("missing lan static result")
	}
	if static.Phase != "Blocked" {
		t.Fatalf("static phase = %s, want Blocked", static.Phase)
	}
	if got := strings.Join(static.Plan, "\n"); !strings.Contains(got, "overlaps") {
		t.Fatalf("static plan = %q, want overlap block", got)
	}
}

func staticDefaultRouteRouter() *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: true, Owner: "routerd"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DefaultRoutePolicy"},
				Metadata: api.ObjectMeta{Name: "default-v4"},
				Spec: api.IPv4DefaultRoutePolicySpec{
					Candidates: []api.IPv4DefaultRoutePolicyCandidate{
						{Interface: "wan", GatewaySource: "static", Gateway: "192.168.1.254", Priority: 10, Table: 100, Mark: 256},
					},
				},
			},
		}},
	}
}

func TestPlanAllowsDocumentedOverlapWithWarning(t *testing.T) {
	router := overlapRouter(true)
	engine := &Engine{
		Command: fakeCommand(map[string]string{
			"hostname":                         "router.example\n",
			"ip -brief link show dev ens18":    "ens18 UP 52:54:00:00:00:18 <BROADCAST,MULTICAST,UP,LOWER_UP>\n",
			"ip -brief link show dev ens19":    "ens19 UP 52:54:00:00:00:19 <BROADCAST,MULTICAST,UP,LOWER_UP>\n",
			"ip -brief -4 addr show dev ens18": "ens18 UP 192.168.160.20/24\n",
			"ip -brief -4 addr show dev ens19": "",
		}),
		OSNetworking: &osNetworking{},
	}

	result, err := engine.Plan(router)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	static := findResult(result, "net.routerd.net/v1alpha1/IPv4StaticAddress/lan-ipv4")
	if static == nil {
		t.Fatal("missing lan static result")
	}
	if static.Phase == "Blocked" {
		t.Fatalf("static phase = %s, want non-blocked", static.Phase)
	}
	if len(static.Warnings) == 0 {
		t.Fatal("expected overlap warning")
	}
}

func TestAdoptionCandidates(t *testing.T) {
	router := staticDefaultRouteRouter()
	engine := &Engine{
		Command: fakeCommand(map[string]string{
			"ip -4 rule show":                  "0:\tfrom all lookup local\n10:\tfrom all fwmark 0x100 lookup 100\n32766:\tfrom all lookup main\n",
			"ip -4 route show table all":       "default via 192.168.1.254 dev ens18 table 100 metric 50\n",
			"nft list tables":                  "table ip routerd_default_route\n",
			"ip -4 route show default":         "default via 192.168.1.254 dev ens18 proto static metric 100\n",
			"ip -brief link show dev ens18":    "ens18 UP 52:54:00:00:00:18 <BROADCAST,MULTICAST,UP,LOWER_UP>\n",
			"ip -brief -4 addr show dev ens18": "192.168.1.10/24\n",
		}),
		OSNetworking: &osNetworking{},
	}
	candidates, err := engine.AdoptionCandidates(router, nil)
	if err != nil {
		t.Fatalf("adoption candidates: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected adoption candidates")
	}
	var foundRule bool
	for _, candidate := range candidates {
		if candidate.Kind == "linux.ipv4.fwmarkRule" && candidate.Name == "priority=10,mark=0x100,table=100" {
			foundRule = true
		}
	}
	if !foundRule {
		t.Fatalf("missing fwmark adoption candidate: %+v", candidates)
	}
	ledger := &resource.Ledger{Version: 1}
	ledger.Remember([]resource.Artifact{
		{
			Kind:  "linux.ipv4.fwmarkRule",
			Name:  "priority=10,mark=0x100,table=100",
			Owner: "net.routerd.net/v1alpha1/IPv4DefaultRoutePolicy/default-v4",
			Attributes: map[string]string{
				"priority": "10",
				"mark":     "0x100",
				"table":    "100",
			},
		},
	})
	candidates, err = engine.AdoptionCandidates(router, ledger)
	if err != nil {
		t.Fatalf("adoption candidates with ledger: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.Kind == "linux.ipv4.fwmarkRule" && candidate.Name == "priority=10,mark=0x100,table=100" {
			t.Fatalf("ledger-owned rule still returned as adoption candidate: %+v", candidates)
		}
	}
}

func TestAdoptionCandidatesReportAttributeDrift(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Hostname"},
				Metadata: api.ObjectMeta{Name: "system-hostname"},
				Spec:     api.HostnameSpec{Hostname: "router03.lain.local", Managed: true},
			},
		}},
	}
	engine := &Engine{
		Command: fakeCommand(map[string]string{
			"hostname": "router03\n",
		}),
		OSNetworking: &osNetworking{},
	}
	candidates, err := engine.AdoptionCandidates(router, nil)
	if err != nil {
		t.Fatalf("adoption candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %+v, want one", candidates)
	}
	got := candidates[0]
	if got.Desired["hostname"] != "router03.lain.local" || got.Observed["hostname"] != "router03" {
		t.Fatalf("candidate attributes = %+v", got)
	}
	if !strings.Contains(got.Reason, "observed attributes differ") {
		t.Fatalf("reason = %q", got.Reason)
	}
}

func TestLedgerOwnedOrphansOnlyReportsCleanupEligibleArtifacts(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
	}
	ledger := &resource.Ledger{Version: 1}
	ledger.Remember([]resource.Artifact{
		{Kind: "linux.ipip6.tunnel", Name: "ds-old", Owner: "net.routerd.net/v1alpha1/DSLiteTunnel/old"},
		{Kind: "nft.table", Name: "routerd_old", Owner: "net.routerd.net/v1alpha1/IPv4SourceNAT/old", Attributes: map[string]string{"family": "ip", "name": "routerd_old"}},
		{Kind: "systemd.service", Name: "routerd-old.service", Owner: "net.routerd.net/v1alpha1/PPPoEInterface/old"},
		{Kind: "linux.link", Name: "ens19", Owner: "net.routerd.net/v1alpha1/Interface/lan"},
		{Kind: "file", Name: "/etc/ppp/chap-secrets", Owner: "net.routerd.net/v1alpha1/PPPoEInterface/old"},
	})
	engine := &Engine{
		Command: fakeCommand(map[string]string{
			"ip -4 rule show":            "",
			"ip -4 route show table all": "",
			"nft list tables":            "table ip routerd_old\n",
			"systemctl list-unit-files routerd-*.service --no-legend --no-pager": "routerd-old.service enabled enabled\n",
			"systemctl cat routerd-dnsmasq.service":                              "",
			"systemctl cat routerd-old.service":                                  "[Unit]\n",
			"test -f /etc/ppp/chap-secrets":                                      "",
			"test -f /etc/ppp/pap-secrets":                                       "",
			"test -f /usr/local/etc/routerd/dnsmasq.conf":                        "",
			"test -f /usr/local/etc/routerd/nftables.nft":                        "",
			"test -f /usr/local/etc/routerd/default-route.nft":                   "",
			"hostname":                                  "router\n",
			"ip -brief link show":                       "ens19 UP aa:bb <BROADCAST>\n",
			"ip -brief -4 addr show":                    "",
			"ip -brief -6 addr show":                    "",
			"ip -d link show type ip6tnl":               "8: ds-old@NONE: <POINTOPOINT,NOARP,UP> mtu 1454\n",
			"sysctl -n net.ipv4.ip_forward":             "1\n",
			"sysctl -n net.ipv6.conf.all.forwarding":    "1\n",
			"sysctl -n net.ipv4.conf.all.rp_filter":     "0\n",
			"sysctl -n net.ipv4.conf.default.rp_filter": "0\n",
			"ls /proc/sys/net/ipv4/conf":                "",
		}),
		OSNetworking: &osNetworking{},
	}
	orphans, artifacts, err := engine.LedgerOwnedOrphans(router, ledger)
	if err != nil {
		t.Fatalf("ledger owned orphans: %v", err)
	}
	if len(orphans) != 3 || len(artifacts) != 3 {
		t.Fatalf("orphans = %+v artifacts = %+v, want three cleanup eligible", orphans, artifacts)
	}
	kinds := map[string]bool{}
	for _, orphan := range orphans {
		kinds[orphan.Kind+"/"+orphan.Name] = true
	}
	for _, want := range []string{
		"linux.ipip6.tunnel/ds-old",
		"nft.table/routerd_old",
		"systemd.service/routerd-old.service",
	} {
		if !kinds[want] {
			t.Fatalf("missing ledger orphan %s in %+v", want, orphans)
		}
	}
}

func fakeCommand(outputs map[string]string) func(string, ...string) ([]byte, error) {
	return func(name string, args ...string) ([]byte, error) {
		key := strings.Join(append([]string{name}, args...), " ")
		if out, ok := outputs[key]; ok {
			return []byte(out), nil
		}
		if strings.HasPrefix(key, "ip -brief -4 addr show dev ") {
			return nil, fmt.Errorf("no address output for %s", key)
		}
		return nil, fmt.Errorf("unexpected command %s", key)
	}
}

func findResult(result *Result, id string) *ResourceResult {
	for i := range result.Resources {
		if result.Resources[i].ID == id {
			return &result.Resources[i]
		}
	}
	return nil
}

func overlapRouter(allowOverlap bool) *api.Router {
	staticSpec := api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.160.3/24"}
	if allowOverlap {
		staticSpec.AllowOverlap = true
		staticSpec.AllowOverlapReason = "overlapping customer network for NAT lab"
	}
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true, Owner: "routerd"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "lan-ipv4"},
				Spec:     staticSpec,
			},
		}},
	}
}
