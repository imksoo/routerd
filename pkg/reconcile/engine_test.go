package reconcile

import (
	"fmt"
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/config"
)

func TestPlanBlocksManagedInterfaceWhenOSNetworkingExists(t *testing.T) {
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
	if result.Phase != "Blocked" {
		t.Fatalf("phase = %s, want Blocked", result.Phase)
	}

	lan := findResult(result, "net.routerd.net/v1alpha1/Interface/lan")
	if lan == nil {
		t.Fatal("missing lan result")
	}
	if lan.Phase != "RequiresAdoption" {
		t.Fatalf("lan phase = %s, want RequiresAdoption", lan.Phase)
	}

	static := findResult(result, "net.routerd.net/v1alpha1/IPv4StaticAddress/lan-ipv4")
	if static == nil {
		t.Fatal("missing lan static result")
	}
	if static.Phase != "RequiresAdoption" {
		t.Fatalf("static phase = %s, want RequiresAdoption", static.Phase)
	}
	if got := strings.Join(static.Plan, "\n"); !strings.Contains(got, "requires adoption") {
		t.Fatalf("static plan = %q, want adoption block", got)
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

func TestPlanUsesExternalDHCPv4DefaultRoute(t *testing.T) {
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

	route := findResult(result, "net.routerd.net/v1alpha1/IPv4DefaultRoute/default-v4")
	if route == nil {
		t.Fatal("missing default route result")
	}
	if route.Observed["currentGateway"] != "192.168.1.1" {
		t.Fatalf("currentGateway = %q, want 192.168.1.1", route.Observed["currentGateway"])
	}
	if route.Observed["currentIfname"] != "ens18" {
		t.Fatalf("currentIfname = %q, want ens18", route.Observed["currentIfname"])
	}
	if got := strings.Join(route.Plan, "\n"); !strings.Contains(got, "DHCPv4 default route") {
		t.Fatalf("route plan = %q, want DHCPv4 default route", got)
	}
}

func TestPlanStaticDefaultRouteDrift(t *testing.T) {
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
	route := findResult(result, "net.routerd.net/v1alpha1/IPv4DefaultRoute/default-v4")
	if route == nil {
		t.Fatal("missing default route result")
	}
	if route.Phase != "Drifted" {
		t.Fatalf("route phase = %s, want Drifted", route.Phase)
	}
	if got := strings.Join(route.Plan, "\n"); !strings.Contains(got, "via 192.168.1.254 dev ens18") {
		t.Fatalf("route plan = %q, want static gateway ensure", got)
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
				Spec: map[string]any{
					"ifname":  "ens18",
					"managed": true,
					"owner":   "routerd",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DefaultRoute"},
				Metadata: api.ObjectMeta{Name: "default-v4"},
				Spec: map[string]any{
					"interface":     "wan",
					"gatewaySource": "static",
					"gateway":       "192.168.1.254",
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
	staticSpec := map[string]any{
		"interface": "lan",
		"address":   "192.168.160.3/24",
	}
	if allowOverlap {
		staticSpec["allowOverlap"] = true
		staticSpec["allowOverlapReason"] = "overlapping customer network for NAT lab"
	}
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec: map[string]any{
					"ifname":  "ens18",
					"managed": false,
					"owner":   "external",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec: map[string]any{
					"ifname":  "ens19",
					"managed": true,
					"owner":   "routerd",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "lan-ipv4"},
				Spec:     staticSpec,
			},
		}},
	}
}
