// SPDX-License-Identifier: BSD-3-Clause

package plugins_test

import (
	"bytes"
	"encoding/json"
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
			NICRef     string   `json:"nicRef"`
			SubnetRef  string   `json:"subnetRef"`
			PrivateIPs []string `json:"privateIPs"`
		} `json:"self"`
		IPs []struct {
			Address   string            `json:"address"`
			NICRef    string            `json:"nicRef"`
			SubnetRef string            `json:"subnetRef"`
			Tags      map[string]string `json:"tags"`
		} `json:"ips"`
		Error string `json:"error"`
	} `json:"status"`
}

func TestProviderPrivateIPInventoryPluginAWS(t *testing.T) {
	requirePython(t)
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "aws"), `#!/bin/sh
case "$*" in
  *"--network-interface-ids eni-router"*)
    printf '%s\n' '{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-router","SubnetId":"subnet-a"}]}'
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
	res := runInventoryPlugin(t, bin, `{"spec":{"provider":"aws","selfNicRef":"eni-router","target":{"region":"us-east-1"}}}`)
	if res.Status.Status != "succeeded" {
		t.Fatalf("status = %q error=%q", res.Status.Status, res.Status.Error)
	}
	if res.Status.Self.NICRef != "eni-router" || res.Status.Self.SubnetRef != "subnet-a" {
		t.Fatalf("self = %+v, want eni-router/subnet-a", res.Status.Self)
	}
	assertIP(t, res, "10.77.60.11", "eni-client", "subnet-a")
}

func TestProviderPrivateIPInventoryPluginAWSResolvesSelfFromLocalIP(t *testing.T) {
	requirePython(t)
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "aws"), `#!/bin/sh
case "$*" in
  *"Name=addresses.private-ip-address,Values=10.77.60.21"*)
    printf '%s\n' '{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-router","SubnetId":"subnet-a","PrivateIpAddresses":[{"PrivateIpAddress":"10.77.60.21","Primary":true}]}]}'
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
	assertIP(t, res, "10.77.60.11", "eni-client", "subnet-a")
}

func TestProviderPrivateIPInventoryPluginAzure(t *testing.T) {
	requirePython(t)
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "az"), `#!/bin/sh
case "$*" in
  *"network nic show --ids /nic/router"*)
    printf '%s\n' '{"id":"/nic/router","resourceGroup":"rg-demo","ipConfigurations":[{"subnet":{"id":"/subnets/demo"}}]}'
    ;;
  *"network nic list --resource-group rg-demo"*)
    printf '%s\n' '[{"id":"/nic/router","tags":{"role":"router"},"ipConfigurations":[{"privateIPAddress":"10.77.60.22","primary":true,"subnet":{"id":"/subnets/demo"}}]},{"id":"/nic/client","tags":{"role":"client"},"ipConfigurations":[{"privateIPAddress":"10.77.60.12","primary":false,"subnet":{"id":"/subnets/demo"}}]}]'
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
	if res.Status.Self.NICRef != "/nic/router" || res.Status.Self.SubnetRef != "/subnets/demo" {
		t.Fatalf("self = %+v, want /nic/router//subnets/demo", res.Status.Self)
	}
	assertIP(t, res, "10.77.60.12", "/nic/client", "/subnets/demo")
}

func TestProviderPrivateIPInventoryPluginOCI(t *testing.T) {
	requirePython(t)
	bin := fakeBinDir(t)
	writeExecutable(t, filepath.Join(bin, "oci"), `#!/bin/sh
case "$*" in
  *"network vnic get --vnic-id vnic-router"*)
    printf '%s\n' '{"data":{"id":"vnic-router","subnet-id":"subnet-oci"}}'
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
	assertIP(t, res, "10.77.60.13", "vnic-client", "subnet-oci")
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
