// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
)

const testNICID = "/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1"

func TestParseArgsConsumesGlobalsAndSetEntries(t *testing.T) {
	req, err := parseArgs([]string{
		"network", "route-table", "route", "update",
		"--resource-group", "rg1",
		"--route-table-name", "rt1",
		"--name", "r1",
		"--set", "addressPrefix=10.0.0.9/32", "nextHopType=VirtualAppliance", "nextHopIpAddress=10.0.0.4",
		"--only-show-errors",
		"--output", "json",
	})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if got := strings.Join(req.Words, " "); got != "network route-table route update" {
		t.Fatalf("words = %q", got)
	}
	if req.Flags["output"] != "" {
		t.Fatalf("--output should be consumed, flags=%#v", req.Flags)
	}
	if req.Sets["nextHopIpAddress"] != "10.0.0.4" {
		t.Fatalf("sets = %#v", req.Sets)
	}
}

func TestDispatchNICShowOutputShape(t *testing.T) {
	fake := newFakeAzure()
	req, err := parseArgs([]string{"network", "nic", "show", "--ids", testNICID})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	var out bytes.Buffer
	if err := dispatch(context.Background(), req, fake, &out); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var body struct {
		ID                 string `json:"id"`
		Name               string `json:"name"`
		ResourceGroup      string `json:"resourceGroup"`
		EnableIPForwarding bool   `json:"enableIPForwarding"`
		IPConfigurations   []struct {
			Name             string `json:"name"`
			PrivateIPAddress string `json:"privateIPAddress"`
			Primary          bool   `json:"primary"`
		} `json:"ipConfigurations"`
	}
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if body.ID != testNICID || body.Name != "nic1" || body.ResourceGroup != "rg1" || body.EnableIPForwarding {
		t.Fatalf("body = %#v", body)
	}
	if len(body.IPConfigurations) != 1 || !body.IPConfigurations[0].Primary || body.IPConfigurations[0].PrivateIPAddress != "10.0.0.4" {
		t.Fatalf("ipConfigurations = %#v", body.IPConfigurations)
	}
}

func TestDispatchIPConfigCreateMutatesNICPUT(t *testing.T) {
	fake := newFakeAzure()
	req, err := parseArgs([]string{
		"network", "nic", "ip-config", "create",
		"--resource-group", "rg1",
		"--nic-name", "nic1",
		"--name", "ipcfg-mobility",
		"--private-ip-address", "10.0.0.9/32",
	})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if err := dispatch(context.Background(), req, fake, &bytes.Buffer{}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if fake.putNICCount != 1 {
		t.Fatalf("putNICCount = %d", fake.putNICCount)
	}
	cfg := findIPConfig(fake.nic, "ipcfg-mobility")
	if cfg == nil || cfg.Properties == nil || stringPtr(cfg.Properties.PrivateIPAddress) != "10.0.0.9" {
		t.Fatalf("created config = %#v", cfg)
	}
}

func TestDispatchIPConfigDeleteRefusesPrimary(t *testing.T) {
	fake := newFakeAzure()
	req, err := parseArgs([]string{
		"network", "nic", "ip-config", "delete",
		"--resource-group", "rg1",
		"--nic-name", "nic1",
		"--name", "primary",
	})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	err = dispatch(context.Background(), req, fake, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "primary") {
		t.Fatalf("delete primary err = %v", err)
	}
	if fake.putNICCount != 0 {
		t.Fatalf("primary delete should not PUT, count=%d", fake.putNICCount)
	}
}

func TestDispatchNICUpdateSetsForwarding(t *testing.T) {
	fake := newFakeAzure()
	req, err := parseArgs([]string{"network", "nic", "update", "--ids", testNICID, "--ip-forwarding", "true"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if err := dispatch(context.Background(), req, fake, &bytes.Buffer{}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if fake.nic.Properties == nil || !boolPtr(fake.nic.Properties.EnableIPForwarding) {
		t.Fatalf("enableIPForwarding = %#v", fake.nic.Properties)
	}
}

func TestDispatchRouteUpdateParsesSetEntries(t *testing.T) {
	fake := newFakeAzure()
	req, err := parseArgs([]string{
		"network", "route-table", "route", "update",
		"--resource-group", "rg1",
		"--route-table-name", "rt1",
		"--name", "r1",
		"--set", "addressPrefix=10.0.0.9/32", "nextHopType=VirtualAppliance", "nextHopIpAddress=10.0.0.4",
	})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if err := dispatch(context.Background(), req, fake, &bytes.Buffer{}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if fake.route.Properties == nil || stringPtr(fake.route.Properties.AddressPrefix) != "10.0.0.9/32" || stringPtr(fake.route.Properties.NextHopIPAddress) != "10.0.0.4" {
		t.Fatalf("route = %#v", fake.route)
	}
}

type fakeAzure struct {
	nic         armnetwork.Interface
	routeTable  armnetwork.RouteTable
	route       armnetwork.Route
	putNICCount int
}

func newFakeAzure() *fakeAzure {
	primary := true
	return &fakeAzure{
		nic: armnetwork.Interface{
			ID:   ptr(testNICID),
			Name: ptr("nic1"),
			Properties: &armnetwork.InterfacePropertiesFormat{
				EnableIPForwarding: ptr(false),
				IPConfigurations: []*armnetwork.InterfaceIPConfiguration{{
					Name: ptr("primary"),
					Properties: &armnetwork.InterfaceIPConfigurationPropertiesFormat{
						Primary:                   &primary,
						PrivateIPAddress:          ptr("10.0.0.4"),
						PrivateIPAllocationMethod: ptr(armnetwork.IPAllocationMethodDynamic),
						Subnet:                    &armnetwork.Subnet{ID: ptr("/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet/subnets/default")},
					},
				}},
			},
		},
		routeTable: armnetwork.RouteTable{Name: ptr("rt1")},
		route:      armnetwork.Route{Name: ptr("r1")},
	}
}

func (f *fakeAzure) GetNIC(ctx context.Context, resourceGroup, nicName string) (armnetwork.Interface, error) {
	return f.nic, nil
}

func (f *fakeAzure) ListNICs(ctx context.Context, resourceGroup string) ([]armnetwork.Interface, error) {
	return []armnetwork.Interface{f.nic}, nil
}

func (f *fakeAzure) PutNIC(ctx context.Context, resourceGroup, nicName string, nic armnetwork.Interface) (armnetwork.Interface, error) {
	f.putNICCount++
	f.nic = nic
	return f.nic, nil
}

func (f *fakeAzure) GetRouteTable(ctx context.Context, resourceGroup, routeTableName string) (armnetwork.RouteTable, error) {
	return f.routeTable, nil
}

func (f *fakeAzure) GetRoute(ctx context.Context, resourceGroup, routeTableName, routeName string) (armnetwork.Route, error) {
	return f.route, nil
}

func (f *fakeAzure) PutRoute(ctx context.Context, resourceGroup, routeTableName, routeName string, route armnetwork.Route) (armnetwork.Route, error) {
	f.route = route
	return f.route, nil
}

func (f *fakeAzure) DeleteRoute(ctx context.Context, resourceGroup, routeTableName, routeName string) error {
	f.route = armnetwork.Route{}
	return nil
}
