// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
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
			SubnetId:           aws.String("subnet-a"),
			SourceDestCheck:    aws.Bool(true),
			PrivateIpAddresses: []types.NetworkInterfacePrivateIpAddress{
				{PrivateIpAddress: aws.String("10.99.0.4"), Primary: aws.Bool(true)},
				{PrivateIpAddress: aws.String("10.77.60.10"), Primary: aws.Bool(false)},
			},
			TagSet: []types.Tag{{Key: aws.String("role"), Value: aws.String("router")}},
		}},
	}
	req, err := parseArgs([]string{"ec2", "describe-network-interfaces", "--filters", "Name=subnet-id,Values=subnet-a", "--region", "ap-northeast-1"})
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
			SubnetID           string `json:"SubnetId"`
			SourceDestCheck    bool   `json:"SourceDestCheck"`
			PrivateIPAddresses []struct {
				PrivateIPAddress string `json:"PrivateIpAddress"`
				Primary          bool   `json:"Primary"`
			} `json:"PrivateIpAddresses"`
			TagSet []struct {
				Key   string `json:"Key"`
				Value string `json:"Value"`
			} `json:"TagSet"`
		} `json:"NetworkInterfaces"`
	}
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if len(body.NetworkInterfaces) != 1 || body.NetworkInterfaces[0].NetworkInterfaceID != "eni-1" || !body.NetworkInterfaces[0].SourceDestCheck {
		t.Fatalf("body = %#v", body)
	}
	if fake.describeNI == nil || len(fake.describeNI.Filters) != 1 || aws.ToString(fake.describeNI.Filters[0].Name) != "subnet-id" || fake.describeNI.Filters[0].Values[0] != "subnet-a" {
		t.Fatalf("describe input = %#v, want subnet-id filter", fake.describeNI)
	}
	if body.NetworkInterfaces[0].SubnetID != "subnet-a" || len(body.NetworkInterfaces[0].TagSet) != 1 || body.NetworkInterfaces[0].TagSet[0].Key != "role" {
		t.Fatalf("body missing subnet/tag shape: %#v", body.NetworkInterfaces[0])
	}
	if len(body.NetworkInterfaces[0].PrivateIPAddresses) != 2 || body.NetworkInterfaces[0].PrivateIPAddresses[1].PrivateIPAddress != "10.77.60.10" {
		t.Fatalf("private IPs = %#v", body.NetworkInterfaces[0].PrivateIPAddresses)
	}
}

func TestDispatchDescribeInstancesOutputShape(t *testing.T) {
	fake := &fakeEC2{
		reservations: []types.Reservation{{
			Instances: []types.Instance{{
				InstanceId: aws.String("i-router"),
				State:      &types.InstanceState{Name: types.InstanceStateNameRunning},
				NetworkInterfaces: []types.InstanceNetworkInterface{{
					NetworkInterfaceId: aws.String("eni-router"),
				}},
			}},
		}},
	}
	req, err := parseArgs([]string{"ec2", "describe-instances", "--filters", "Name=network-interface.subnet-id,Values=subnet-a", "--region", "ap-northeast-1"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	var out bytes.Buffer
	if err := dispatch(context.Background(), req, fake, &out); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if fake.describeInstances == nil || len(fake.describeInstances.Filters) != 1 || aws.ToString(fake.describeInstances.Filters[0].Name) != "network-interface.subnet-id" {
		t.Fatalf("describe instances input = %#v, want subnet filter", fake.describeInstances)
	}
	var body struct {
		Reservations []struct {
			Instances []struct {
				InstanceID string `json:"InstanceId"`
				State      struct {
					Name string `json:"Name"`
				} `json:"State"`
				NetworkInterfaces []struct {
					NetworkInterfaceID string `json:"NetworkInterfaceId"`
				} `json:"NetworkInterfaces"`
			} `json:"Instances"`
		} `json:"Reservations"`
	}
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if len(body.Reservations) != 1 || len(body.Reservations[0].Instances) != 1 {
		t.Fatalf("body = %#v", body)
	}
	got := body.Reservations[0].Instances[0]
	if got.InstanceID != "i-router" || got.State.Name != "running" || got.NetworkInterfaces[0].NetworkInterfaceID != "eni-router" {
		t.Fatalf("instance = %#v", got)
	}
}

func TestEC2FiltersPreservesMultipleValues(t *testing.T) {
	filters, err := ec2Filters("Name=addresses.private-ip-address,Values=10.77.60.21,10.77.60.22")
	if err != nil {
		t.Fatalf("ec2Filters: %v", err)
	}
	if len(filters) != 1 {
		t.Fatalf("filters = %#v, want one filter", filters)
	}
	if aws.ToString(filters[0].Name) != "addresses.private-ip-address" {
		t.Fatalf("name = %q", aws.ToString(filters[0].Name))
	}
	if !reflect.DeepEqual(filters[0].Values, []string{"10.77.60.21", "10.77.60.22"}) {
		t.Fatalf("values = %#v", filters[0].Values)
	}
}

func TestEC2FiltersRejectsMalformedValues(t *testing.T) {
	for _, raw := range []string{
		"Name=subnet-id",
		"Values=subnet-a",
		"Name=,Values=subnet-a",
		"Name=subnet-id,Values=",
	} {
		if filters, err := ec2Filters(raw); err == nil {
			t.Fatalf("ec2Filters(%q) = %#v, nil error", raw, filters)
		}
	}
}

func TestDescribeNetworkInterfacesRequiresBoundedTarget(t *testing.T) {
	req, err := parseArgs([]string{"ec2", "describe-network-interfaces", "--region", "ap-northeast-1"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if err := dispatch(context.Background(), req, &fakeEC2{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "requires --network-interface-ids or --filters") {
		t.Fatalf("dispatch error = %v, want bounded-target error", err)
	}
}

func TestDescribeNetworkInterfacesRejectsMalformedFilter(t *testing.T) {
	req, err := parseArgs([]string{"ec2", "describe-network-interfaces", "--filters", "Name=subnet-id", "--region", "ap-northeast-1"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if err := dispatch(context.Background(), req, &fakeEC2{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "invalid --filters value") {
		t.Fatalf("dispatch error = %v, want invalid filter error", err)
	}
}

func TestReservationJSONHandlesNilState(t *testing.T) {
	body := reservationJSON(types.Reservation{Instances: []types.Instance{{InstanceId: aws.String("i-router")}}})
	instances, ok := body["Instances"].([]map[string]any)
	if !ok || len(instances) != 1 {
		t.Fatalf("body = %#v", body)
	}
	state, ok := instances[0]["State"].(map[string]any)
	if !ok || state["Name"] != "" {
		t.Fatalf("state = %#v, want empty state name", instances[0]["State"])
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
	reservations      []types.Reservation
	routeTables       []types.RouteTable
	describeNI        *ec2.DescribeNetworkInterfacesInput
	describeInstances *ec2.DescribeInstancesInput
	assign            *ec2.AssignPrivateIpAddressesInput
	unassign          *ec2.UnassignPrivateIpAddressesInput
	modify            *ec2.ModifyNetworkInterfaceAttributeInput
	create            *ec2.CreateRouteInput
	replace           *ec2.ReplaceRouteInput
	delete            *ec2.DeleteRouteInput
}

func (f *fakeEC2) DescribeNetworkInterfaces(_ context.Context, in *ec2.DescribeNetworkInterfacesInput, _ ...func(*ec2.Options)) (*ec2.DescribeNetworkInterfacesOutput, error) {
	f.describeNI = in
	return &ec2.DescribeNetworkInterfacesOutput{NetworkInterfaces: f.networkInterfaces}, nil
}

func (f *fakeEC2) DescribeInstances(_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	f.describeInstances = in
	return &ec2.DescribeInstancesOutput{Reservations: f.reservations}, nil
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
