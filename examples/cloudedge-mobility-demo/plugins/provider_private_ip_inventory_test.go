// SPDX-License-Identifier: BSD-3-Clause

package plugins_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type inventoryResult struct {
	Status struct {
		Status string `json:"status"`
		Self   struct {
			NICRef            string   `json:"nicRef"`
			SubnetRef         string   `json:"subnetRef"`
			ResourceRef       string   `json:"resourceRef"`
			ResourceType      string   `json:"resourceType"`
			PrivateIPs        []string `json:"privateIPs"`
			CapturedAddresses []string `json:"capturedAddresses"`
			ForwardingEnabled *bool    `json:"forwardingEnabled"`
		} `json:"self"`
		IPs []struct {
			Address       string            `json:"address"`
			NICRef        string            `json:"nicRef"`
			SubnetRef     string            `json:"subnetRef"`
			ResourceRef   string            `json:"resourceRef"`
			ResourceType  string            `json:"resourceType"`
			Tags          map[string]string `json:"tags"`
			InstanceState string            `json:"instanceState"`
		} `json:"ips"`
		LocalIPs []struct {
			Address       string            `json:"address"`
			NICRef        string            `json:"nicRef"`
			SubnetRef     string            `json:"subnetRef"`
			ResourceRef   string            `json:"resourceRef"`
			ResourceType  string            `json:"resourceType"`
			Tags          map[string]string `json:"tags"`
			InstanceState string            `json:"instanceState"`
		} `json:"localIPs"`
		Error string `json:"error"`
	} `json:"status"`
}

func TestProviderPrivateIPInventoryPluginAWS(t *testing.T) {
	requirePython(t)
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "aws"), `#!/bin/sh
case "$*" in
  *"--network-interface-ids eni-router"*)
    printf '%s\n' '{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-router","SubnetId":"subnet-a","SourceDestCheck":false}]}'
    ;;
  *"describe-instances"*)
    printf '%s\n' '{"Reservations":[{"Instances":[{"InstanceId":"i-router","NetworkInterfaces":[{"NetworkInterfaceId":"eni-router"}]},{"InstanceId":"i-client","NetworkInterfaces":[{"NetworkInterfaceId":"eni-client"}]}]}]}'
    ;;
  *"Name=subnet-id,Values=subnet-a"*)
    printf '%s\n' '{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-router","SubnetId":"subnet-a","PrivateIpAddresses":[{"PrivateIpAddress":"10.77.60.21","Primary":true}],"TagSet":[{"Key":"role","Value":"router"}]},{"NetworkInterfaceId":"eni-client","SubnetId":"subnet-a","PrivateIpAddresses":[{"PrivateIpAddress":"10.77.60.11","Primary":false}],"TagSet":[{"Key":"role","Value":"client"}]}]}'
    ;;
  *)
    echo "unexpected aws args: $*" >&2
    exit 2
    ;;
esac
`)
	res := runInventoryPlugin(t, bin, `{"spec":{"provider":"aws","strategy":"secondary-ip","prefix":"10.77.60.0/24","selfNicRef":"eni-router","routeTableRef":"rtb-cloudedge","target":{"region":"us-east-1","routeTableRef":"rtb-cloudedge"}}}`)
	if res.Status.Status != "succeeded" {
		t.Fatalf("status = %q error=%q", res.Status.Status, res.Status.Error)
	}
	if res.Status.Self.NICRef != "eni-router" || res.Status.Self.SubnetRef != "subnet-a" {
		t.Fatalf("self = %+v, want eni-router/subnet-a", res.Status.Self)
	}
	if res.Status.Self.ResourceRef != "i-router" || res.Status.Self.ResourceType != "router-nic" {
		t.Fatalf("self resource = %+v, want i-router/router-nic", res.Status.Self)
	}
	if res.Status.Self.ForwardingEnabled == nil || !*res.Status.Self.ForwardingEnabled {
		t.Fatalf("self.forwardingEnabled = %#v, want true", res.Status.Self.ForwardingEnabled)
	}
	assertIP(t, res, "10.77.60.11", "eni-client", "subnet-a")
	assertResource(t, res, "10.77.60.11", "i-client", "instance-nic")
	assertLocalIP(t, res, "10.77.60.11")
	if len(res.Status.Self.CapturedAddresses) != 0 {
		t.Fatalf("self.capturedAddresses = %#v, want empty for secondary-ip strategy", res.Status.Self.CapturedAddresses)
	}
}

func TestProviderPrivateIPInventoryPluginAWSRouteTableCaptures(t *testing.T) {
	requirePython(t)
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "aws"), `#!/bin/sh
case "$*" in
  *"--network-interface-ids eni-router"*)
    printf '%s\n' '{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-router","SubnetId":"subnet-a","SourceDestCheck":false,"PrivateIpAddresses":[{"PrivateIpAddress":"10.77.60.21","Primary":true}]}]}'
    ;;
  *"describe-route-tables --route-table-ids rtb-cloudedge"*)
    printf '%s\n' '{"RouteTables":[{"RouteTableId":"rtb-cloudedge","Routes":[{"DestinationCidrBlock":"10.77.60.12/32","NetworkInterfaceId":"eni-router"},{"DestinationCidrBlock":"10.77.60.13/32","NetworkInterfaceId":"eni-other"},{"DestinationCidrBlock":"10.77.61.9/32","NetworkInterfaceId":"eni-router"}]}]}'
    ;;
  *"describe-instances"*)
    printf '%s\n' '{"Reservations":[{"Instances":[{"InstanceId":"i-router","State":{"Name":"running"},"NetworkInterfaces":[{"NetworkInterfaceId":"eni-router"}]},{"InstanceId":"i-client","State":{"Name":"running"},"NetworkInterfaces":[{"NetworkInterfaceId":"eni-client"}]}]}]}'
    ;;
  *"Name=subnet-id,Values=subnet-a"*)
    printf '%s\n' '{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-router","SubnetId":"subnet-a","PrivateIpAddresses":[{"PrivateIpAddress":"10.77.60.21","Primary":true}],"TagSet":[{"Key":"role","Value":"router"}]},{"NetworkInterfaceId":"eni-client","SubnetId":"subnet-a","PrivateIpAddresses":[{"PrivateIpAddress":"10.77.60.11","Primary":false}],"TagSet":[{"Key":"role","Value":"client"}]}]}'
    ;;
  *)
    echo "unexpected aws args: $*" >&2
    exit 2
    ;;
esac
`)
	res := runInventoryPlugin(t, bin, `{"spec":{"provider":"aws","strategy":"route-table","prefix":"10.77.60.0/24","selfNicRef":"eni-router","subnetRef":"subnet-a","routeTableRef":"rtb-cloudedge","target":{"region":"us-east-1","routeTableRef":"rtb-cloudedge"}}}`)
	if res.Status.Status != "succeeded" {
		t.Fatalf("status = %q error=%q", res.Status.Status, res.Status.Error)
	}
	assertSelfCaptured(t, res, "10.77.60.12/32")
	assertNotSelfCaptured(t, res, "10.77.60.13/32")
	assertNotSelfCaptured(t, res, "10.77.61.9/32")
}

func TestProviderPrivateIPInventoryPluginAWSResolvesSelfFromLocalIP(t *testing.T) {
	requirePython(t)
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "aws"), `#!/bin/sh
case "$*" in
  *"Name=addresses.private-ip-address,Values=10.77.60.21"*)
    printf '%s\n' '{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-router","SubnetId":"subnet-a","SourceDestCheck":true,"PrivateIpAddresses":[{"PrivateIpAddress":"10.77.60.21","Primary":true}]}]}'
    ;;
  *"describe-instances"*)
    printf '%s\n' '{"Reservations":[{"Instances":[{"InstanceId":"i-router","NetworkInterfaces":[{"NetworkInterfaceId":"eni-router"}]},{"InstanceId":"i-client","NetworkInterfaces":[{"NetworkInterfaceId":"eni-client"}]}]}]}'
    ;;
  *"Name=subnet-id,Values=subnet-a"*)
    printf '%s\n' '{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-router","SubnetId":"subnet-a","PrivateIpAddresses":[{"PrivateIpAddress":"10.77.60.21","Primary":true}]},{"NetworkInterfaceId":"eni-client","SubnetId":"subnet-a","PrivateIpAddresses":[{"PrivateIpAddress":"10.77.60.11","Primary":false}]}]}'
    ;;
  *)
    echo "unexpected aws args: $*" >&2
    exit 2
    ;;
esac
`)
	res := runInventoryPluginWithEnv(t, bin, `{"spec":{"provider":"aws","target":{"region":"us-east-1"}}}`, []string{"ROUTERD_PROVIDER_INVENTORY_LOCAL_IPS=10.77.60.21"})
	if res.Status.Status != "succeeded" {
		t.Fatalf("status = %q error=%q", res.Status.Status, res.Status.Error)
	}
	if res.Status.Self.NICRef != "eni-router" || res.Status.Self.SubnetRef != "subnet-a" {
		t.Fatalf("self = %+v, want resolved eni-router/subnet-a", res.Status.Self)
	}
	if res.Status.Self.ResourceRef != "i-router" || res.Status.Self.ResourceType != "router-nic" {
		t.Fatalf("self resource = %+v, want i-router/router-nic", res.Status.Self)
	}
	if res.Status.Self.ForwardingEnabled == nil || *res.Status.Self.ForwardingEnabled {
		t.Fatalf("self.forwardingEnabled = %#v, want false", res.Status.Self.ForwardingEnabled)
	}
	assertIP(t, res, "10.77.60.11", "eni-client", "subnet-a")
	assertResource(t, res, "10.77.60.11", "i-client", "instance-nic")
}

func TestProviderPrivateIPInventoryPluginAWSResolvesSelfFromIMDS(t *testing.T) {
	requirePython(t)
	imds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest/api/token":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("token"))
		case "/latest/meta-data/network/interfaces/macs/":
			_, _ = w.Write([]byte("02:00:00:00:00:05/\n"))
		case "/latest/meta-data/network/interfaces/macs/02:00:00:00:00:05/interface-id":
			_, _ = w.Write([]byte("eni-router-b"))
		case "/latest/meta-data/network/interfaces/macs/02:00:00:00:00:05/subnet-id":
			_, _ = w.Write([]byte("subnet-a"))
		case "/latest/meta-data/network/interfaces/macs/02:00:00:00:00:05/local-ipv4s":
			_, _ = w.Write([]byte("10.77.60.5\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer imds.Close()
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "aws"), `#!/bin/sh
case "$*" in
  *"--network-interface-ids eni-router-b"*)
    printf '%s\n' '{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-router-b","SubnetId":"subnet-a","SourceDestCheck":false,"PrivateIpAddresses":[{"PrivateIpAddress":"10.77.60.5","Primary":true}]}]}'
    ;;
  *"describe-instances"*)
    printf '%s\n' '{"Reservations":[{"Instances":[{"InstanceId":"i-router-b","NetworkInterfaces":[{"NetworkInterfaceId":"eni-router-b"}]},{"InstanceId":"i-client","NetworkInterfaces":[{"NetworkInterfaceId":"eni-client"}]}]}]}'
    ;;
  *"Name=subnet-id,Values=subnet-a"*)
    printf '%s\n' '{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-router-b","SubnetId":"subnet-a","PrivateIpAddresses":[{"PrivateIpAddress":"10.77.60.5","Primary":true}]},{"NetworkInterfaceId":"eni-client","SubnetId":"subnet-a","PrivateIpAddresses":[{"PrivateIpAddress":"10.77.60.11","Primary":false}]}]}'
    ;;
  *)
    echo "unexpected aws args: $*" >&2
    exit 2
    ;;
esac
`)
	res := runInventoryPluginWithEnv(t, bin, `{"spec":{"provider":"aws","target":{"region":"us-east-1"}}}`, []string{
		"ROUTERD_PROVIDER_INVENTORY_AWS_IMDS_BASE=" + imds.URL,
	})
	if res.Status.Status != "succeeded" {
		t.Fatalf("status = %q error=%q", res.Status.Status, res.Status.Error)
	}
	if res.Status.Self.NICRef != "eni-router-b" || res.Status.Self.SubnetRef != "subnet-a" {
		t.Fatalf("self = %+v, want IMDS-resolved eni-router-b/subnet-a", res.Status.Self)
	}
	if res.Status.Self.ResourceRef != "i-router-b" || res.Status.Self.ResourceType != "router-nic" {
		t.Fatalf("self resource = %+v, want i-router-b/router-nic", res.Status.Self)
	}
	if res.Status.Self.ForwardingEnabled == nil || !*res.Status.Self.ForwardingEnabled {
		t.Fatalf("self.forwardingEnabled = %#v, want true", res.Status.Self.ForwardingEnabled)
	}
	assertIP(t, res, "10.77.60.11", "eni-client", "subnet-a")
	assertResource(t, res, "10.77.60.11", "i-client", "instance-nic")
}

func TestProviderPrivateIPInventoryPluginAzure(t *testing.T) {
	requirePython(t)
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "az"), `#!/bin/sh
case "$*" in
  *"network nic show --ids /nic/router"*)
    printf '%s\n' '{"id":"/nic/router","resourceGroup":"rg-demo","enableIPForwarding":true,"ipConfigurations":[{"subnet":{"id":"/subnets/demo"}}]}'
    ;;
  *"network nic list --resource-group rg-demo"*)
    printf '%s\n' '[{"id":"/nic/router","tags":{"role":"router"},"ipConfigurations":[{"privateIPAddress":"10.77.60.22","primary":true,"subnet":{"id":"/subnets/demo"}}]},{"id":"/nic/client","tags":{"role":"client"},"ipConfigurations":[{"privateIPAddress":"10.77.60.12","primary":false,"subnet":{"id":"/subnets/demo"}}]}]'
    ;;
  *"vm list --resource-group rg-demo"*)
    printf '%s\n' '[{"id":"/vm/router","powerState":"VM running","networkProfile":{"networkInterfaces":[{"id":"/nic/router"}]}},{"id":"/vm/client","powerState":"VM running","networkProfile":{"networkInterfaces":[{"id":"/nic/client"}]}}]'
    ;;
  *)
    echo "unexpected az args: $*" >&2
    exit 2
    ;;
esac
`)
	res := runInventoryPlugin(t, bin, `{"spec":{"provider":"azure","strategy":"secondary-ip","prefix":"10.77.60.0/24","selfNicRef":"/nic/router","routeTableRef":"/subscriptions/sub/resourceGroups/rg-demo/providers/Microsoft.Network/routeTables/rt-cloudedge","target":{"resourceGroup":"rg-demo","routeTableRef":"/subscriptions/sub/resourceGroups/rg-demo/providers/Microsoft.Network/routeTables/rt-cloudedge","nextHopIPAddress":"10.77.60.22"}}}`)
	if res.Status.Status != "succeeded" {
		t.Fatalf("status = %q error=%q", res.Status.Status, res.Status.Error)
	}
	if res.Status.Self.NICRef != "/nic/router" || res.Status.Self.SubnetRef != "/subnets/demo" {
		t.Fatalf("self = %+v, want /nic/router//subnets/demo", res.Status.Self)
	}
	if res.Status.Self.ResourceRef != "/vm/router" || res.Status.Self.ResourceType != "router-nic" {
		t.Fatalf("self resource = %+v, want /vm/router/router-nic", res.Status.Self)
	}
	if res.Status.Self.ForwardingEnabled == nil || !*res.Status.Self.ForwardingEnabled {
		t.Fatalf("self.forwardingEnabled = %#v, want true", res.Status.Self.ForwardingEnabled)
	}
	assertIP(t, res, "10.77.60.12", "/nic/client", "/subnets/demo")
	assertResource(t, res, "10.77.60.12", "/vm/client", "instance-nic")
	assertLocalIP(t, res, "10.77.60.12")
	if len(res.Status.Self.CapturedAddresses) != 0 {
		t.Fatalf("self.capturedAddresses = %#v, want empty for secondary-ip strategy", res.Status.Self.CapturedAddresses)
	}
}

func TestProviderPrivateIPInventoryPluginAzureRouteTableCaptures(t *testing.T) {
	requirePython(t)
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "az"), `#!/bin/sh
case "$*" in
  *"network nic show --ids /nic/router"*)
    printf '%s\n' '{"id":"/nic/router","resourceGroup":"rg-demo","enableIPForwarding":true,"ipConfigurations":[{"privateIPAddress":"10.77.60.22","primary":true,"subnet":{"id":"/subnets/demo"}}]}'
    ;;
  *"network route-table route list --resource-group rg-demo --route-table-name rt-cloudedge"*)
    printf '%s\n' '[{"addressPrefix":"10.77.60.13/32","nextHopType":"VirtualAppliance","nextHopIpAddress":"10.77.60.22"},{"addressPrefix":"10.77.60.14/32","nextHopType":"VirtualAppliance","nextHopIpAddress":"10.77.60.99"},{"addressPrefix":"10.77.61.9/32","nextHopType":"VirtualAppliance","nextHopIpAddress":"10.77.60.22"}]'
    ;;
  *"network nic list --resource-group rg-demo"*)
    printf '%s\n' '[{"id":"/nic/router","tags":{"role":"router"},"ipConfigurations":[{"privateIPAddress":"10.77.60.22","primary":true,"subnet":{"id":"/subnets/demo"}}]},{"id":"/nic/client","tags":{"role":"client"},"ipConfigurations":[{"privateIPAddress":"10.77.60.12","primary":false,"subnet":{"id":"/subnets/demo"}}]}]'
    ;;
  *"vm list --resource-group rg-demo"*)
    printf '%s\n' '[{"id":"/vm/router","powerState":"VM running","networkProfile":{"networkInterfaces":[{"id":"/nic/router"}]}},{"id":"/vm/client","powerState":"VM running","networkProfile":{"networkInterfaces":[{"id":"/nic/client"}]}}]'
    ;;
  *)
    echo "unexpected az args: $*" >&2
    exit 2
    ;;
esac
`)
	res := runInventoryPlugin(t, bin, `{"spec":{"provider":"azure","strategy":"route-table","prefix":"10.77.60.0/24","selfNicRef":"/nic/router","routeTableRef":"/subscriptions/sub/resourceGroups/rg-demo/providers/Microsoft.Network/routeTables/rt-cloudedge","target":{"resourceGroup":"rg-demo","routeTableRef":"/subscriptions/sub/resourceGroups/rg-demo/providers/Microsoft.Network/routeTables/rt-cloudedge","nextHopIPAddress":"10.77.60.22"}}}`)
	if res.Status.Status != "succeeded" {
		t.Fatalf("status = %q error=%q", res.Status.Status, res.Status.Error)
	}
	assertSelfCaptured(t, res, "10.77.60.13/32")
	assertNotSelfCaptured(t, res, "10.77.60.14/32")
	assertNotSelfCaptured(t, res, "10.77.61.9/32")
}

func TestProviderPrivateIPInventoryPluginOCI(t *testing.T) {
	requirePython(t)
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "oci"), `#!/bin/sh
case "$*" in
  *"network vnic get --vnic-id vnic-router"*)
    printf '%s\n' '{"data":{"id":"vnic-router","subnet-id":"subnet-oci","skip-source-dest-check":true}}'
    ;;
  *"network private-ip list --subnet-id subnet-oci"*)
    printf '%s\n' '{"data":[{"ip-address":"10.77.60.23","vnic-id":"vnic-router","subnet-id":"subnet-oci","is-primary":true},{"ip-address":"10.77.60.13","vnic-id":"vnic-client","subnet-id":"subnet-oci","is-primary":false,"freeform-tags":{"role":"client"}}]}'
    ;;
  *)
    echo "unexpected oci args: $*" >&2
    exit 2
    ;;
esac
`)
	res := runInventoryPlugin(t, bin, `{"spec":{"provider":"oci","selfNicRef":"vnic-router","target":{"region":"ap-tokyo-1"}}}`)
	if res.Status.Status != "succeeded" {
		t.Fatalf("status = %q error=%q", res.Status.Status, res.Status.Error)
	}
	if res.Status.Self.NICRef != "vnic-router" || res.Status.Self.SubnetRef != "subnet-oci" {
		t.Fatalf("self = %+v, want vnic-router/subnet-oci", res.Status.Self)
	}
	if res.Status.Self.ForwardingEnabled == nil || !*res.Status.Self.ForwardingEnabled {
		t.Fatalf("self.forwardingEnabled = %#v, want true", res.Status.Self.ForwardingEnabled)
	}
	assertIP(t, res, "10.77.60.13", "vnic-client", "subnet-oci")
	assertLocalIP(t, res, "10.77.60.13")
}

func TestProviderPrivateIPInventoryPluginOCIResolvesSelfFromEnv(t *testing.T) {
	requirePython(t)
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "oci"), `#!/bin/sh
case "$*" in
  *"network vnic get --vnic-id vnic-router-b"*)
    printf '%s\n' '{"data":{"id":"vnic-router-b","subnet-id":"subnet-oci","skip-source-dest-check":false}}'
    ;;
  *"network private-ip list --subnet-id subnet-oci"*)
    printf '%s\n' '{"data":[{"ip-address":"10.77.60.5","vnic-id":"vnic-router-b","subnet-id":"subnet-oci","is-primary":true},{"ip-address":"10.77.60.13","vnic-id":"vnic-client","subnet-id":"subnet-oci","is-primary":false}]}'
    ;;
  *)
    echo "unexpected oci args: $*" >&2
    exit 2
    ;;
esac
`)
	res := runInventoryPluginWithEnv(t, bin, `{"spec":{"provider":"oci","target":{"region":"ap-tokyo-1"}}}`, []string{"ROUTERD_PROVIDER_INVENTORY_SELF_NIC_REF=vnic-router-b"})
	if res.Status.Status != "succeeded" {
		t.Fatalf("status = %q error=%q", res.Status.Status, res.Status.Error)
	}
	if res.Status.Self.NICRef != "vnic-router-b" || res.Status.Self.SubnetRef != "subnet-oci" {
		t.Fatalf("self = %+v, want env-resolved vnic-router-b/subnet-oci", res.Status.Self)
	}
	if res.Status.Self.ForwardingEnabled == nil || *res.Status.Self.ForwardingEnabled {
		t.Fatalf("self.forwardingEnabled = %#v, want false", res.Status.Self.ForwardingEnabled)
	}
	assertIP(t, res, "10.77.60.13", "vnic-client", "subnet-oci")
}

func TestProviderPrivateIPInventoryPluginAWSReportsInstanceState(t *testing.T) {
	requirePython(t)
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "aws"), `#!/bin/sh
case "$*" in
  *"--network-interface-ids eni-router"*)
    printf '%s\n' '{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-router","SubnetId":"subnet-a","SourceDestCheck":false}]}'
    ;;
  *"describe-instances"*)
    printf '%s\n' '{"Reservations":[{"Instances":[{"InstanceId":"i-router","State":{"Name":"running"},"NetworkInterfaces":[{"NetworkInterfaceId":"eni-router"}]},{"InstanceId":"i-stopped","State":{"Name":"stopped"},"NetworkInterfaces":[{"NetworkInterfaceId":"eni-stopped"}]}]}]}'
    ;;
  *"Name=subnet-id,Values=subnet-a"*)
    printf '%s\n' '{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-router","SubnetId":"subnet-a","PrivateIpAddresses":[{"PrivateIpAddress":"10.77.60.21","Primary":true}],"TagSet":[{"Key":"role","Value":"router"}]},{"NetworkInterfaceId":"eni-stopped","SubnetId":"subnet-a","PrivateIpAddresses":[{"PrivateIpAddress":"10.77.60.19","Primary":true}],"TagSet":[{"Key":"role","Value":"client"}]}]}'
    ;;
  *)
    echo "unexpected aws args: $*" >&2
    exit 2
    ;;
esac
`)
	res := runInventoryPlugin(t, bin, `{"spec":{"provider":"aws","selfNicRef":"eni-router","target":{"region":"us-east-1"}}}`)
	if res.Status.Status != "succeeded" {
		t.Fatalf("status = %q error=%q", res.Status.Status, res.Status.Error)
	}
	assertIP(t, res, "10.77.60.21", "eni-router", "subnet-a")
	assertIP(t, res, "10.77.60.19", "eni-stopped", "subnet-a")
	assertInstanceState(t, res, "10.77.60.19", "stopped")
	assertInstanceState(t, res, "10.77.60.21", "running")
}

func TestProviderPrivateIPInventoryPluginOCIReportsInstanceState(t *testing.T) {
	requirePython(t)
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "oci"), `#!/bin/sh
case "$*" in
  *"network vnic get --vnic-id vnic-router"*)
    printf '%s\n' '{"data":{"id":"vnic-router","subnet-id":"subnet-oci","compartment-id":"compartment-demo","skip-source-dest-check":true}}'
    ;;
  *"compute vnic-attachment list --compartment-id compartment-demo"*)
    printf '%s\n' '{"data":[{"vnic-id":"vnic-router","instance-id":"i-router"},{"vnic-id":"vnic-client","instance-id":"i-client"},{"vnic-id":"vnic-stopped","instance-id":"i-stopped"}]}'
    ;;
  *"compute instance list --compartment-id compartment-demo"*)
    printf '%s\n' '{"data":[{"id":"i-router","lifecycle-state":"RUNNING"},{"id":"i-client","lifecycle-state":"RUNNING"},{"id":"i-stopped","lifecycle-state":"STOPPED"}]}'
    ;;
  *"network private-ip list --subnet-id subnet-oci"*)
    printf '%s\n' '{"data":[{"ip-address":"10.77.60.23","vnic-id":"vnic-router","subnet-id":"subnet-oci","is-primary":true},{"ip-address":"10.77.60.13","vnic-id":"vnic-client","subnet-id":"subnet-oci","is-primary":false,"freeform-tags":{"role":"client"}},{"ip-address":"10.77.60.19","vnic-id":"vnic-stopped","subnet-id":"subnet-oci","is-primary":true}]}'
    ;;
  *)
    echo "unexpected oci args: $*" >&2
    exit 2
    ;;
esac
`)
	res := runInventoryPlugin(t, bin, `{"spec":{"provider":"oci","selfNicRef":"vnic-router","target":{"region":"ap-tokyo-1"}}}`)
	if res.Status.Status != "succeeded" {
		t.Fatalf("status = %q error=%q", res.Status.Status, res.Status.Error)
	}
	assertIP(t, res, "10.77.60.13", "vnic-client", "subnet-oci")
	assertIP(t, res, "10.77.60.19", "vnic-stopped", "subnet-oci")
	assertResource(t, res, "10.77.60.13", "i-client", "instance-nic")
	assertResource(t, res, "10.77.60.19", "i-stopped", "instance-nic")
	assertInstanceState(t, res, "10.77.60.19", "stopped")
	assertInstanceState(t, res, "10.77.60.13", "running")
}

func TestProviderPrivateIPInventoryPluginAzureReportsInstanceState(t *testing.T) {
	requirePython(t)
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "az"), `#!/bin/sh
case "$*" in
  *"network nic show --ids /nic/router"*)
    printf '%s\n' '{"id":"/nic/router","resourceGroup":"rg-demo","enableIPForwarding":true,"ipConfigurations":[{"subnet":{"id":"/subnets/demo"}}]}'
    ;;
  *"network nic list --resource-group rg-demo"*)
    printf '%s\n' '[{"id":"/nic/router","tags":{"role":"router"},"ipConfigurations":[{"privateIPAddress":"10.77.60.22","primary":true,"subnet":{"id":"/subnets/demo"}}]},{"id":"/nic/client","tags":{"role":"client"},"ipConfigurations":[{"privateIPAddress":"10.77.60.12","primary":false,"subnet":{"id":"/subnets/demo"}}]},{"id":"/nic/stopped","tags":{"role":"stopped"},"ipConfigurations":[{"privateIPAddress":"10.77.60.19","primary":true,"subnet":{"id":"/subnets/demo"}}]}]'
    ;;
  *"vm list --resource-group rg-demo"*)
    printf '%s\n' '[{"id":"/vm/router","powerState":"VM running","networkProfile":{"networkInterfaces":[{"id":"/nic/router"}]}},{"id":"/vm/client","powerState":"VM running","networkProfile":{"networkInterfaces":[{"id":"/nic/client"}]}},{"id":"/vm/stopped","powerState":"VM stopped","networkProfile":{"networkInterfaces":[{"id":"/nic/stopped"}]}}]'
    ;;
  *)
    echo "unexpected az args: $*" >&2
    exit 2
    ;;
esac
`)
	res := runInventoryPlugin(t, bin, `{"spec":{"provider":"azure","selfNicRef":"/nic/router","target":{"resourceGroup":"rg-demo"}}}`)
	if res.Status.Status != "succeeded" {
		t.Fatalf("status = %q error=%q", res.Status.Status, res.Status.Error)
	}
	assertIP(t, res, "10.77.60.12", "/nic/client", "/subnets/demo")
	assertIP(t, res, "10.77.60.19", "/nic/stopped", "/subnets/demo")
	assertResource(t, res, "10.77.60.12", "/vm/client", "instance-nic")
	assertResource(t, res, "10.77.60.19", "/vm/stopped", "instance-nic")
	assertInstanceState(t, res, "10.77.60.19", "stopped")
	assertInstanceState(t, res, "10.77.60.12", "running")
}

func TestProviderPrivateIPInventoryPluginAWSFailsWhenInstanceMetadataQueryFails(t *testing.T) {
	requirePython(t)
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "aws"), `#!/bin/sh
case "$*" in
  *"--network-interface-ids eni-router"*)
    printf '%s\n' '{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-router","SubnetId":"subnet-a","SourceDestCheck":false,"PrivateIpAddresses":[{"PrivateIpAddress":"10.77.60.21","Primary":true}]}]}'
    ;;
  *"describe-instances"*)
    echo "throttled" >&2
    exit 2
    ;;
  *)
    echo "unexpected aws args: $*" >&2
    exit 2
    ;;
esac
`)
	res := runInventoryPlugin(t, bin, `{"spec":{"provider":"aws","selfNicRef":"eni-router","target":{"region":"us-east-1"}}}`)
	if res.Status.Status != "failed" || !strings.Contains(res.Status.Error, "AWS instance metadata inventory failed") {
		t.Fatalf("status=%q error=%q, want failed AWS metadata error", res.Status.Status, res.Status.Error)
	}
}

func TestProviderPrivateIPInventoryPluginAzureFailsWhenVMMetadataQueryFails(t *testing.T) {
	requirePython(t)
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "az"), `#!/bin/sh
case "$*" in
  *"network nic show --ids /nic/router"*)
    printf '%s\n' '{"id":"/nic/router","resourceGroup":"rg-demo","enableIPForwarding":true,"ipConfigurations":[{"privateIPAddress":"10.77.60.22","subnet":{"id":"/subnets/demo"}}]}'
    ;;
  *"network nic list --resource-group rg-demo"*)
    printf '%s\n' '[{"id":"/nic/router","tags":{"role":"router"},"ipConfigurations":[{"privateIPAddress":"10.77.60.22","primary":true,"subnet":{"id":"/subnets/demo"}}]}]'
    ;;
  *"vm list --resource-group rg-demo"*)
    echo "temporary azure error" >&2
    exit 2
    ;;
  *)
    echo "unexpected az args: $*" >&2
    exit 2
    ;;
esac
`)
	res := runInventoryPlugin(t, bin, `{"spec":{"provider":"azure","selfNicRef":"/nic/router","target":{"resourceGroup":"rg-demo"}}}`)
	if res.Status.Status != "failed" || !strings.Contains(res.Status.Error, "Azure VM metadata inventory failed") {
		t.Fatalf("status=%q error=%q, want failed Azure metadata error", res.Status.Status, res.Status.Error)
	}
}

func runInventoryPlugin(t *testing.T, fakeBin, stdin string) inventoryResult {
	t.Helper()
	return runInventoryPluginWithEnv(t, fakeBin, stdin, nil)
}

func runInventoryPluginWithEnv(t *testing.T, fakeBin, stdin string, extraEnv []string) inventoryResult {
	t.Helper()
	cmd := exec.Command("./provider-private-ip-inventory")
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Env = append(cmd.Env, extraEnv...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("plugin failed: %v stderr=%s stdout=%s", err, errOut.String(), out.String())
	}
	var res inventoryResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("decode result: %v stdout=%s", err, out.String())
	}
	return res
}

func assertIP(t *testing.T, res inventoryResult, address, nicRef, subnetRef string) {
	t.Helper()
	for _, ip := range res.Status.IPs {
		if ip.Address == address {
			if ip.NICRef != nicRef || ip.SubnetRef != subnetRef {
				t.Fatalf("record for %s = nic %q subnet %q, want nic %q subnet %q", address, ip.NICRef, ip.SubnetRef, nicRef, subnetRef)
			}
			return
		}
	}
	t.Fatalf("missing address %s in %+v", address, res.Status.IPs)
}

func assertLocalIP(t *testing.T, res inventoryResult, address string) {
	t.Helper()
	for _, ip := range res.Status.LocalIPs {
		if ip.Address == address {
			return
		}
	}
	t.Fatalf("missing local address %s in %+v", address, res.Status.LocalIPs)
}

func assertSelfCaptured(t *testing.T, res inventoryResult, address string) {
	t.Helper()
	for _, got := range res.Status.Self.CapturedAddresses {
		if got == address {
			return
		}
	}
	t.Fatalf("missing self captured address %s in %+v", address, res.Status.Self.CapturedAddresses)
}

func assertNotSelfCaptured(t *testing.T, res inventoryResult, address string) {
	t.Helper()
	for _, got := range res.Status.Self.CapturedAddresses {
		if got == address {
			t.Fatalf("unexpected self captured address %s in %+v", address, res.Status.Self.CapturedAddresses)
		}
	}
}

func assertResource(t *testing.T, res inventoryResult, address, wantRef, wantType string) {
	t.Helper()
	for _, ip := range res.Status.IPs {
		if ip.Address == address {
			if ip.ResourceRef != wantRef || ip.ResourceType != wantType {
				t.Fatalf("resource for %s = %q/%q, want %q/%q", address, ip.ResourceRef, ip.ResourceType, wantRef, wantType)
			}
			return
		}
	}
	t.Fatalf("missing address %s in %+v", address, res.Status.IPs)
}

func assertInstanceState(t *testing.T, res inventoryResult, address, wantState string) {
	t.Helper()
	for _, ip := range res.Status.IPs {
		if ip.Address == address {
			if ip.InstanceState != wantState {
				t.Fatalf("instanceState for %s = %q, want %q", address, ip.InstanceState, wantState)
			}
			return
		}
	}
	t.Fatalf("missing address %s in %+v", address, res.Status.IPs)
}

func fakeBinDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is not supported on Windows")
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func requirePython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for the demo inventory plugin")
	}
}
