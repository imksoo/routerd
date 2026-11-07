// SPDX-License-Identifier: BSD-3-Clause

package plugins_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func routeInventoryCLI(t *testing.T, bin, provider string) {
	t.Helper()
	writeExecutable(t, filepath.Join(bin, "ip"), "#!/bin/sh\nexit 1\n")
	if provider == "aws" {
		writeExecutable(t, filepath.Join(bin, "aws"), `#!/bin/sh
case "$*" in
  *describe-route-tables*)
    [ "$ROUTERD_ROUTE_RESPONSE" != failed ] || { echo route-read-failed >&2; exit 19; }
    printf '%s' "$ROUTERD_ROUTE_RESPONSE" ;;
  *describe-instances*) printf '%s' '{"Reservations":[]}' ;;
  *describe-network-interfaces*) printf '%s' '{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-router","SubnetId":"subnet-a","SourceDestCheck":false,"PrivateIpAddresses":[{"PrivateIpAddress":"10.77.60.21","Primary":true}]}]}' ;;
  *) exit 99 ;;
esac
`)
		return
	}
	writeExecutable(t, filepath.Join(bin, "az"), `#!/bin/sh
case "$*" in
  *'network route-table route list'*)
    [ "$ROUTERD_ROUTE_RESPONSE" != failed ] || { echo route-read-failed >&2; exit 19; }
    printf '%s' "$ROUTERD_ROUTE_RESPONSE" ;;
  *'network nic show'*) printf '%s' '{"id":"/nic/router","resourceGroup":"rg-demo","enableIPForwarding":true,"ipConfigurations":[{"privateIPAddress":"10.77.60.22","primary":true,"subnet":{"id":"/subnets/demo"}}]}' ;;
  *'network nic list'*) printf '%s' '[{"id":"/nic/router","ipConfigurations":[{"privateIPAddress":"10.77.60.22","primary":true,"subnet":{"id":"/subnets/demo"}}]}]' ;;
  *'vm list'*) printf '%s' '[]' ;;
  *) exit 99 ;;
esac
`)
}

func TestProviderPrivateIPInventoryRequiredAWSRouteObservation(t *testing.T) {
	requirePython(t)
	for _, sample := range []struct {
		name, response string
		wantSuccess    bool
	}{
		{"failed-read", "failed", false},
		{"missing-list", `{}`, false},
		{"unknown-list", `{"RouteTables":null}`, false},
		{"missing-table", `{"RouteTables":[]}`, false},
		{"different-table", `{"RouteTables":[{"RouteTableId":"rtb-other","Routes":[]}]}`, false},
		{"missing-routes", `{"RouteTables":[{"RouteTableId":"rtb-cloudedge"}]}`, false},
		{"empty-routes", `{"RouteTables":[{"RouteTableId":"rtb-cloudedge","Routes":[]}]}`, true},
	} {
		t.Run(sample.name, func(t *testing.T) {
			bin := fakeBinDir(t)
			routeInventoryCLI(t, bin, "aws")
			res := runInventoryPluginWithEnv(t, bin, `{"spec":{"provider":"aws","strategy":"route-table","prefix":"10.77.60.0/24","selfNicRef":"eni-router","subnetRef":"subnet-a","routeTableRef":"rtb-cloudedge"}}`, []string{"ROUTERD_ROUTE_RESPONSE=" + sample.response})
			if (res.Status.Status == "succeeded") != sample.wantSuccess || (!sample.wantSuccess && (res.Status.Status != "failed" || res.Status.Error == "")) {
				t.Fatalf("status=%s error=%s", res.Status.Status, res.Status.Error)
			}
		})
	}
}

func TestProviderPrivateIPInventoryRequiredAzureCLIRouteObservation(t *testing.T) {
	requirePython(t)
	for _, response := range []string{"failed", `null`, `{}`, `[]`} {
		t.Run(response, func(t *testing.T) {
			bin := fakeBinDir(t)
			routeInventoryCLI(t, bin, "azure")
			res := runInventoryPluginWithEnv(t, bin, `{"spec":{"provider":"azure","strategy":"route-table","prefix":"10.77.60.0/24","selfNicRef":"/nic/router","subnetRef":"/subnets/demo","routeTableRef":"/routeTables/rt-cloudedge","target":{"resourceGroup":"rg-demo","nextHopIPAddress":"10.77.60.22"}}}`, []string{"ROUTERD_PROVIDER_INVENTORY_AZURE_ARM_DISABLE=1", "ROUTERD_ROUTE_RESPONSE=" + response})
			if (res.Status.Status == "succeeded") != (response == `[]`) || (response != `[]` && (res.Status.Status != "failed" || res.Status.Error == "")) {
				t.Fatalf("status=%s error=%s", res.Status.Status, res.Status.Error)
			}
		})
	}
}

func TestProviderPrivateIPInventoryAzurePartialRouteReadRequiresCompleteFallback(t *testing.T) {
	requirePython(t)
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "failed", true: "complete-cli"}[fallback], func(t *testing.T) {
			var arm *httptest.Server
			arm = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/metadata/identity/oauth2/token":
					_, _ = w.Write([]byte(`{"access_token":"token"}`))
				case strings.HasSuffix(r.URL.Path, "/networkInterfaces"):
					_, _ = w.Write([]byte(`{"value":[{"id":"/nic/router","properties":{"enableIPForwarding":true,"ipConfigurations":[{"properties":{"privateIPAddress":"10.77.60.22","primary":true,"subnet":{"id":"/subnets/demo"}}}]}}]}`))
				case strings.HasSuffix(r.URL.Path, "/virtualMachines"):
					_, _ = w.Write([]byte(`{"value":[]}`))
				case strings.HasSuffix(r.URL.Path, "/routes"):
					_, _ = w.Write([]byte(`{"value":[{"properties":{"addressPrefix":"10.77.60.13/32","nextHopType":"VirtualAppliance","nextHopIpAddress":"10.77.60.22"}}],"nextLink":"` + arm.URL + `/route-page2"}`))
				case r.URL.Path == "/route-page2":
					// A successful HTTP response with a missing array is unknown too.
					_, _ = w.Write([]byte(`{}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer arm.Close()
			bin := fakeBinDir(t)
			routeInventoryCLI(t, bin, "azure")
			response := "failed"
			if fallback {
				response = `[{"addressPrefix":"10.77.60.14/32","nextHopType":"VirtualAppliance","nextHopIpAddress":"10.77.60.22"}]`
			}
			res := runInventoryPluginWithEnv(t, bin, `{"spec":{"provider":"azure","strategy":"route-table","prefix":"10.77.60.0/24","selfNicRef":"/nic/router","subnetRef":"/subnets/demo","routeTableRef":"/subscriptions/sub/resourceGroups/rg-demo/providers/Microsoft.Network/routeTables/rt-cloudedge","target":{"resourceGroup":"rg-demo","nextHopIPAddress":"10.77.60.22"}}}`, []string{
				"ROUTERD_PROVIDER_INVENTORY_AZURE_IMDS_BASE=" + arm.URL,
				"ROUTERD_PROVIDER_INVENTORY_AZURE_ARM_BASE=" + arm.URL,
				"ROUTERD_ROUTE_RESPONSE=" + response,
			})
			if fallback {
				if res.Status.Status != "succeeded" {
					t.Fatalf("complete fallback failed: %s", res.Status.Error)
				}
				assertSelfCaptured(t, res, "10.77.60.14/32")
				assertNotSelfCaptured(t, res, "10.77.60.13/32")
			} else if res.Status.Status != "failed" || res.Status.Error == "" {
				t.Fatalf("partial route read became success: %+v", res.Status)
			}
		})
	}
}
