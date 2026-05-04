package chain

import (
	"os"
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestPathMTUPolicyControllerRendersMSSClamp(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.DSLiteTunnelSpec{TunnelName: "ds-lite-a", MTU: 1454}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PathMTUPolicy"}, Metadata: api.ObjectMeta{Name: "lan-dslite"}, Spec: api.PathMTUPolicySpec{
			FromInterface: "lan",
			ToInterfaces:  []string{"ds-lite-a"},
			MTU:           api.PathMTUPolicyMTUSpec{Source: "static", Value: 1454},
			TCPMSSClamp:   api.PathMTUPolicyTCPMSSSpec{Enabled: true, Families: []string{"ipv4"}},
		}},
	}}}
	store := mapStore{}
	controller := PathMTUPolicyController{Router: router, Store: store, DryRun: true, Path: dir + "/mss.nft"}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(controller.Path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{`table inet routerd_mss`, `iifname "ens19" oifname "ds-lite-a" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size set 1414`} {
		if !strings.Contains(got, want) {
			t.Fatalf("mss rules missing %q:\n%s", want, got)
		}
	}
	status := store.ObjectStatus(api.NetAPIVersion, "PathMTUPolicy", "lan-dslite")
	if status["phase"] != "Applied" {
		t.Fatalf("status = %#v", status)
	}
}
