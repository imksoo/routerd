// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func TestParseArgsKeepsAWSCommandAndConsumesGlobals(t *testing.T) {
	req, err := parseArgs([]string{
		"ec2", "describe-network-interfaces",
		"--network-interface-ids", "eni-1",
		"--region", "ap-northeast-1",
		"--output", "json",
	})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if got := strings.Join(req.Words, " "); got != "ec2 describe-network-interfaces" {
		t.Fatalf("words = %q", got)
	}
	if req.Region != "ap-northeast-1" {
		t.Fatalf("region = %q", req.Region)
	}
	if req.Flags["network-interface-ids"] != "eni-1" {
		t.Fatalf("flags = %#v", req.Flags)
	}
	if _, ok := req.Flags["output"]; ok {
		t.Fatalf("--output should be consumed as a global, flags=%#v", req.Flags)
	}
}

func TestDispatchDescribeNetworkInterfacesOutputShape(t *testing.T) {
	fake := &fakeEC2{
		networkInterfaces: []types.NetworkInterface{{
			NetworkInterfaceId: aws.String("eni-1"),
			SourceDestCheck:    aws.Bool(true),
			PrivateIpAddresses: []types.NetworkInterfacePrivateIpAddress{
				{PrivateIpAddress: aws.String("10.99.0.4"), Primary: aws.Bool(true)},
				{PrivateIpAddress: aws.String("10.77.60.10"), Primary: aws.Bool(false)},
			},
		}},
	}
	req, err := parseArgs([]string{"ec2", "describe-network-interfaces", "--network-interface-ids", "eni-1", "--region", "ap-northeast-1"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	var out bytes.Buffer
	if err := dispatch(context.Background(), req, fake, &out); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var body struct {
		NetworkInterfaces []struct {
			NetworkInterfaceID string `json:"NetworkInterfaceId"`
			SourceDestCheck    bool   `json:"SourceDestCheck"`
			PrivateIPAddresses []struct {
				PrivateIPAddress string `json:"PrivateIpAddress"`
				Primary          bool   `json:"Primary"`
			} `json:"PrivateIpAddresses"`
		} `json:"NetworkInterfaces"`
	}
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if len(body.NetworkInterfaces) != 1 || body.NetworkInterfaces[0].NetworkInterfaceID != "eni-1" || !body.NetworkInterfaces[0].SourceDestCheck {
		t.Fatalf("body = %#v", body)
	}
	if len(body.NetworkInterfaces[0].PrivateIPAddresses) != 2 || body.NetworkInterfaces[0].PrivateIPAddresses[1].PrivateIPAddress != "10.77.60.10" {
		t.Fatalf("private IPs = %#v", body.NetworkInterfaces[0].PrivateIPAddresses)
	}
}

func TestDispatchAssignPrivateIPUsesBareIPAndAllowReassignment(t *testing.T) {
	fake := &fakeEC2{}
	req, err := parseArgs([]string{
		"ec2", "assign-private-ip-addresses",
		"--network-interface-id", "eni-1",
		"--private-ip-addresses", "10.77.60.10/32",
		"--allow-reassignment",
		"--region", "ap-northeast-1",
	})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if err := dispatch(context.Background(), req, fake, &bytes.Buffer{}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if fake.assign == nil || aws.ToString(fake.assign.NetworkInterfaceId) != "eni-1" {
		t.Fatalf("assign input = %#v", fake.assign)
	}
	if len(fake.assign.PrivateIpAddresses) != 1 || fake.assign.PrivateIpAddresses[0] != "10.77.60.10" {
		t.Fatalf("assign private IPs = %#v", fake.assign.PrivateIpAddresses)
	}
	if !aws.ToBool(fake.assign.AllowReassignment) {
		t.Fatalf("assign AllowReassignment = %#v", fake.assign.AllowReassignment)
	}
}

func TestDispatchModifyNetworkInterfaceAttributeParsesJSONValue(t *testing.T) {
	fake := &fakeEC2{}
	req, err := parseArgs([]string{
		"ec2", "modify-network-interface-attribute",
		"--network-interface-id", "eni-1",
		"--source-dest-check", `{"Value":false}`,
		"--region", "ap-northeast-1",
	})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if err := dispatch(context.Background(), req, fake, &bytes.Buffer{}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if fake.modify == nil || aws.ToString(fake.modify.NetworkInterfaceId) != "eni-1" {
		t.Fatalf("modify input = %#v", fake.modify)
	}
	if fake.modify.SourceDestCheck == nil || aws.ToBool(fake.modify.SourceDestCheck.Value) {
		t.Fatalf("sourceDestCheck = %#v", fake.modify.SourceDestCheck)
	}
}

func TestDispatchRouteTableCommands(t *testing.T) {
	fake := &fakeEC2{routeTables: []types.RouteTable{{
		RouteTableId: aws.String("rtb-1"),
		Routes: []types.Route{{
			DestinationCidrBlock: aws.String("10.77.60.10/32"),
			NetworkInterfaceId:   aws.String("eni-1"),
		}},
	}}}
	req, err := parseArgs([]string{"ec2", "describe-route-tables", "--route-table-ids", "rtb-1", "--region", "ap-northeast-1"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	var out bytes.Buffer
	if err := dispatch(context.Background(), req, fake, &out); err != nil {
		t.Fatalf("describe dispatch: %v", err)
	}
	if !strings.Contains(out.String(), `"RouteTableId":"rtb-1"`) || !strings.Contains(out.String(), `"NetworkInterfaceId":"eni-1"`) {
		t.Fatalf("route table output = %s", out.String())
	}

	req, err = parseArgs([]string{"ec2", "replace-route", "--route-table-id", "rtb-1", "--destination-cidr-block", "10.77.60.10/32", "--network-interface-id", "eni-2"})
	if err != nil {
		t.Fatalf("parseArgs replace: %v", err)
	}
	if err := dispatch(context.Background(), req, fake, &bytes.Buffer{}); err != nil {
		t.Fatalf("replace dispatch: %v", err)
	}
	if fake.replace == nil || aws.ToString(fake.replace.NetworkInterfaceId) != "eni-2" {
		t.Fatalf("replace input = %#v", fake.replace)
	}
}

type fakeEC2 struct {
	networkInterfaces []types.NetworkInterface
	routeTables       []types.RouteTable
	assign            *ec2.AssignPrivateIpAddressesInput
	unassign          *ec2.UnassignPrivateIpAddressesInput
	modify            *ec2.ModifyNetworkInterfaceAttributeInput
	create            *ec2.CreateRouteInput
	replace           *ec2.ReplaceRouteInput
	delete            *ec2.DeleteRouteInput
}

func (f *fakeEC2) DescribeNetworkInterfaces(context.Context, *ec2.DescribeNetworkInterfacesInput, ...func(*ec2.Options)) (*ec2.DescribeNetworkInterfacesOutput, error) {
	return &ec2.DescribeNetworkInterfacesOutput{NetworkInterfaces: f.networkInterfaces}, nil
}

func (f *fakeEC2) AssignPrivateIpAddresses(_ context.Context, in *ec2.AssignPrivateIpAddressesInput, _ ...func(*ec2.Options)) (*ec2.AssignPrivateIpAddressesOutput, error) {
	f.assign = in
	return &ec2.AssignPrivateIpAddressesOutput{}, nil
}

func (f *fakeEC2) UnassignPrivateIpAddresses(_ context.Context, in *ec2.UnassignPrivateIpAddressesInput, _ ...func(*ec2.Options)) (*ec2.UnassignPrivateIpAddressesOutput, error) {
	f.unassign = in
	return &ec2.UnassignPrivateIpAddressesOutput{}, nil
}

func (f *fakeEC2) ModifyNetworkInterfaceAttribute(_ context.Context, in *ec2.ModifyNetworkInterfaceAttributeInput, _ ...func(*ec2.Options)) (*ec2.ModifyNetworkInterfaceAttributeOutput, error) {
	f.modify = in
	return &ec2.ModifyNetworkInterfaceAttributeOutput{}, nil
}

func (f *fakeEC2) DescribeRouteTables(context.Context, *ec2.DescribeRouteTablesInput, ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
	return &ec2.DescribeRouteTablesOutput{RouteTables: f.routeTables}, nil
}

func (f *fakeEC2) CreateRoute(_ context.Context, in *ec2.CreateRouteInput, _ ...func(*ec2.Options)) (*ec2.CreateRouteOutput, error) {
	f.create = in
	return &ec2.CreateRouteOutput{}, nil
}

func (f *fakeEC2) ReplaceRoute(_ context.Context, in *ec2.ReplaceRouteInput, _ ...func(*ec2.Options)) (*ec2.ReplaceRouteOutput, error) {
	f.replace = in
	return &ec2.ReplaceRouteOutput{}, nil
}

func (f *fakeEC2) DeleteRoute(_ context.Context, in *ec2.DeleteRouteInput, _ ...func(*ec2.Options)) (*ec2.DeleteRouteOutput, error) {
	f.delete = in
	return &ec2.DeleteRouteOutput{}, nil
}
