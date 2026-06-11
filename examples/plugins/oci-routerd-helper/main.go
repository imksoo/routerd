// SPDX-License-Identifier: BSD-3-Clause

// Command oci-routerd-helper is the minimal OCI control-plane helper routerd
// ships for the CloudEdge demo. It intentionally implements only the small
// OCI-CLI-compatible command surface used by the OCI inventory and provider
// executor plugins, backed by the OCI Go SDK and instance principal auth.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/core"
)

const defaultTimeout = 25 * time.Second

type cliRequest struct {
	Region string
	Words  []string
	Flags  map[string]string
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "oci-routerd-helper: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, argv []string) error {
	req, err := parseArgs(argv)
	if err != nil {
		return err
	}
	provider, err := auth.InstancePrincipalConfigurationProvider()
	if err != nil {
		return fmt.Errorf("instance principal auth: %w", err)
	}
	vcn, err := core.NewVirtualNetworkClientWithConfigurationProvider(provider)
	if err != nil {
		return fmt.Errorf("virtual network client: %w", err)
	}
	compute, err := core.NewComputeClientWithConfigurationProvider(provider)
	if err != nil {
		return fmt.Errorf("compute client: %w", err)
	}
	if req.Region != "" {
		vcn.SetRegion(req.Region)
		compute.SetRegion(req.Region)
	}
	return dispatch(ctx, req, vcn, compute)
}

func parseArgs(argv []string) (cliRequest, error) {
	req := cliRequest{Flags: map[string]string{}}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if !strings.HasPrefix(arg, "--") {
			req.Words = append(req.Words, arg)
			continue
		}
		name := strings.TrimPrefix(arg, "--")
		switch name {
		case "all", "force", "unassign-if-already-assigned":
			req.Flags[name] = "true"
		case "output", "config-file", "profile":
			i++
		case "auth":
			if i+1 >= len(argv) {
				return req, fmt.Errorf("--auth requires a value")
			}
			i++
			if argv[i] != "instance_principal" && argv[i] != "instance-principal" {
				return req, fmt.Errorf("unsupported --auth %q (oci-routerd-helper uses instance principal auth)", argv[i])
			}
		case "region":
			if i+1 >= len(argv) {
				return req, fmt.Errorf("--region requires a value")
			}
			i++
			req.Region = argv[i]
		default:
			if i+1 >= len(argv) || strings.HasPrefix(argv[i+1], "--") {
				req.Flags[name] = "true"
				continue
			}
			i++
			req.Flags[name] = argv[i]
		}
	}
	if len(req.Words) < 3 {
		return req, fmt.Errorf("unsupported OCI command %q", strings.Join(argv, " "))
	}
	return req, nil
}

func dispatch(ctx context.Context, req cliRequest, vcn core.VirtualNetworkClient, compute core.ComputeClient) error {
	switch strings.Join(req.Words, " ") {
	case "network vnic get":
		vnicID := req.Flags["vnic-id"]
		if vnicID == "" {
			return errors.New("network vnic get requires --vnic-id")
		}
		resp, err := vcn.GetVnic(ctx, core.GetVnicRequest{VnicId: common.String(vnicID)})
		if err != nil {
			return err
		}
		return writeData(vnicJSON(resp.Vnic))
	case "network vnic update":
		vnicID := req.Flags["vnic-id"]
		if vnicID == "" {
			return errors.New("network vnic update requires --vnic-id")
		}
		skip, ok, err := boolFlag(req.Flags["skip-source-dest-check"])
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("network vnic update requires --skip-source-dest-check")
		}
		resp, err := vcn.UpdateVnic(ctx, core.UpdateVnicRequest{
			VnicId: common.String(vnicID),
			UpdateVnicDetails: core.UpdateVnicDetails{
				SkipSourceDestCheck: common.Bool(skip),
			},
		})
		if err != nil {
			return err
		}
		return writeData(vnicJSON(resp.Vnic))
	case "network private-ip list":
		items, err := listPrivateIPs(ctx, vcn, req.Flags)
		if err != nil {
			return err
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			out = append(out, privateIPJSON(item))
		}
		return writeData(out)
	case "network private-ip create":
		vnicID := req.Flags["vnic-id"]
		ip := req.Flags["ip-address"]
		if vnicID == "" || ip == "" {
			return errors.New("network private-ip create requires --vnic-id and --ip-address")
		}
		resp, err := vcn.CreatePrivateIp(ctx, core.CreatePrivateIpRequest{
			CreatePrivateIpDetails: core.CreatePrivateIpDetails{
				VnicId:    common.String(vnicID),
				IpAddress: common.String(bareIP(ip)),
			},
		})
		if err != nil {
			return err
		}
		return writeData(privateIPJSON(resp.PrivateIp))
	case "network vnic assign-private-ip":
		return assignPrivateIP(ctx, vcn, req.Flags)
	case "network private-ip delete":
		privateIPID := req.Flags["private-ip-id"]
		if privateIPID == "" {
			return errors.New("network private-ip delete requires --private-ip-id")
		}
		_, err := vcn.DeletePrivateIp(ctx, core.DeletePrivateIpRequest{PrivateIpId: common.String(privateIPID)})
		return err
	case "compute vnic-attachment list":
		return listVnicAttachments(ctx, compute, req.Flags)
	case "compute instance list":
		return listInstances(ctx, compute, req.Flags)
	default:
		return fmt.Errorf("unsupported OCI command %q", strings.Join(req.Words, " "))
	}
}

func assignPrivateIP(ctx context.Context, vcn core.VirtualNetworkClient, flags map[string]string) error {
	vnicID := flags["vnic-id"]
	ip := bareIP(flags["ip-address"])
	if vnicID == "" || ip == "" {
		return errors.New("network vnic assign-private-ip requires --vnic-id and --ip-address")
	}
	vnicResp, err := vcn.GetVnic(ctx, core.GetVnicRequest{VnicId: common.String(vnicID)})
	if err != nil {
		return err
	}
	subnetID := stringPtr(vnicResp.Vnic.SubnetId)
	if subnetID == "" {
		return fmt.Errorf("vnic %s has no subnetId", vnicID)
	}
	existing, err := listPrivateIPs(ctx, vcn, map[string]string{"subnet-id": subnetID, "ip-address": ip})
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		current := existing[0]
		if stringPtr(current.VnicId) == vnicID {
			return writeData(privateIPJSON(current))
		}
		if flags["unassign-if-already-assigned"] != "true" {
			return fmt.Errorf("private IP %s is already assigned to %s", ip, stringPtr(current.VnicId))
		}
		resp, err := vcn.UpdatePrivateIp(ctx, core.UpdatePrivateIpRequest{
			PrivateIpId: common.String(stringPtr(current.Id)),
			UpdatePrivateIpDetails: core.UpdatePrivateIpDetails{
				VnicId: common.String(vnicID),
			},
		})
		if err != nil {
			return err
		}
		return writeData(privateIPJSON(resp.PrivateIp))
	}
	resp, err := vcn.CreatePrivateIp(ctx, core.CreatePrivateIpRequest{
		CreatePrivateIpDetails: core.CreatePrivateIpDetails{
			VnicId:    common.String(vnicID),
			IpAddress: common.String(ip),
		},
	})
	if err != nil {
		return err
	}
	return writeData(privateIPJSON(resp.PrivateIp))
}

func listPrivateIPs(ctx context.Context, vcn core.VirtualNetworkClient, flags map[string]string) ([]core.PrivateIp, error) {
	var out []core.PrivateIp
	page := ""
	for {
		req := core.ListPrivateIpsRequest{}
		if flags["subnet-id"] != "" {
			req.SubnetId = common.String(flags["subnet-id"])
		}
		if flags["vnic-id"] != "" {
			req.VnicId = common.String(flags["vnic-id"])
		}
		if flags["ip-address"] != "" {
			req.IpAddress = common.String(bareIP(flags["ip-address"]))
		}
		if page != "" {
			req.Page = common.String(page)
		}
		resp, err := vcn.ListPrivateIps(ctx, req)
		if err != nil {
			return nil, err
		}
		out = append(out, resp.Items...)
		if resp.OpcNextPage == nil || *resp.OpcNextPage == "" {
			return out, nil
		}
		page = *resp.OpcNextPage
	}
}

func listVnicAttachments(ctx context.Context, compute core.ComputeClient, flags map[string]string) error {
	compartment := flags["compartment-id"]
	if compartment == "" {
		return errors.New("compute vnic-attachment list requires --compartment-id")
	}
	var out []map[string]any
	page := ""
	for {
		req := core.ListVnicAttachmentsRequest{CompartmentId: common.String(compartment)}
		if flags["vnic-id"] != "" {
			req.VnicId = common.String(flags["vnic-id"])
		}
		if page != "" {
			req.Page = common.String(page)
		}
		resp, err := compute.ListVnicAttachments(ctx, req)
		if err != nil {
			return err
		}
		for _, item := range resp.Items {
			out = append(out, map[string]any{
				"id":              stringPtr(item.Id),
				"vnic-id":         stringPtr(item.VnicId),
				"instance-id":     stringPtr(item.InstanceId),
				"compartment-id":  stringPtr(item.CompartmentId),
				"lifecycle-state": string(item.LifecycleState),
			})
		}
		if resp.OpcNextPage == nil || *resp.OpcNextPage == "" {
			return writeData(out)
		}
		page = *resp.OpcNextPage
	}
}

func listInstances(ctx context.Context, compute core.ComputeClient, flags map[string]string) error {
	compartment := flags["compartment-id"]
	if compartment == "" {
		return errors.New("compute instance list requires --compartment-id")
	}
	var out []map[string]any
	page := ""
	for {
		req := core.ListInstancesRequest{CompartmentId: common.String(compartment)}
		if page != "" {
			req.Page = common.String(page)
		}
		resp, err := compute.ListInstances(ctx, req)
		if err != nil {
			return err
		}
		for _, item := range resp.Items {
			out = append(out, map[string]any{
				"id":              stringPtr(item.Id),
				"compartment-id":  stringPtr(item.CompartmentId),
				"lifecycle-state": string(item.LifecycleState),
			})
		}
		if resp.OpcNextPage == nil || *resp.OpcNextPage == "" {
			return writeData(out)
		}
		page = *resp.OpcNextPage
	}
}

func vnicJSON(v core.Vnic) map[string]any {
	return map[string]any{
		"id":                     stringPtr(v.Id),
		"subnet-id":              stringPtr(v.SubnetId),
		"compartment-id":         stringPtr(v.CompartmentId),
		"private-ip":             stringPtr(v.PrivateIp),
		"skip-source-dest-check": boolPtr(v.SkipSourceDestCheck),
		"freeform-tags":          v.FreeformTags,
		"defined-tags":           v.DefinedTags,
	}
}

func privateIPJSON(ip core.PrivateIp) map[string]any {
	return map[string]any{
		"id":            stringPtr(ip.Id),
		"ip-address":    stringPtr(ip.IpAddress),
		"vnic-id":       stringPtr(ip.VnicId),
		"subnet-id":     stringPtr(ip.SubnetId),
		"is-primary":    boolPtr(ip.IsPrimary),
		"ip-state":      string(ip.IpState),
		"freeform-tags": ip.FreeformTags,
		"defined-tags":  ip.DefinedTags,
	}
}

func writeData(data any) error {
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(map[string]any{"data": data})
}

func boolFlag(v string) (bool, bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return false, false, nil
	case "true", "1", "yes", "on":
		return true, true, nil
	case "false", "0", "no", "off":
		return false, true, nil
	default:
		return false, true, fmt.Errorf("invalid boolean value %q", v)
	}
}

func bareIP(address string) string {
	address = strings.TrimSpace(address)
	if ip, _, err := net.ParseCIDR(address); err == nil {
		return ip.String()
	}
	return address
}

func stringPtr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func boolPtr(v *bool) bool {
	return v != nil && *v
}
