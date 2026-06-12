// SPDX-License-Identifier: BSD-3-Clause

// Command aws-routerd-helper is the minimal AWS EC2 control-plane helper
// routerd ships for CloudEdge SAM. It intentionally implements only the small
// AWS-CLI-compatible command surface used by aws-provider-executor, backed by
// AWS SDK for Go v2 and the instance role credential chain.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

const defaultTimeout = 25 * time.Second

type cliRequest struct {
	Region string
	Words  []string
	Flags  map[string]string
}

type ec2API interface {
	DescribeNetworkInterfaces(context.Context, *ec2.DescribeNetworkInterfacesInput, ...func(*ec2.Options)) (*ec2.DescribeNetworkInterfacesOutput, error)
	DescribeInstances(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	AssignPrivateIpAddresses(context.Context, *ec2.AssignPrivateIpAddressesInput, ...func(*ec2.Options)) (*ec2.AssignPrivateIpAddressesOutput, error)
	UnassignPrivateIpAddresses(context.Context, *ec2.UnassignPrivateIpAddressesInput, ...func(*ec2.Options)) (*ec2.UnassignPrivateIpAddressesOutput, error)
	ModifyNetworkInterfaceAttribute(context.Context, *ec2.ModifyNetworkInterfaceAttributeInput, ...func(*ec2.Options)) (*ec2.ModifyNetworkInterfaceAttributeOutput, error)
	DescribeRouteTables(context.Context, *ec2.DescribeRouteTablesInput, ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error)
	CreateRoute(context.Context, *ec2.CreateRouteInput, ...func(*ec2.Options)) (*ec2.CreateRouteOutput, error)
	ReplaceRoute(context.Context, *ec2.ReplaceRouteInput, ...func(*ec2.Options)) (*ec2.ReplaceRouteOutput, error)
	DeleteRoute(context.Context, *ec2.DeleteRouteInput, ...func(*ec2.Options)) (*ec2.DeleteRouteOutput, error)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "aws-routerd-helper: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, argv []string, out io.Writer) error {
	req, err := parseArgs(argv)
	if err != nil {
		return err
	}
	opts := []func(*config.LoadOptions) error{}
	if req.Region != "" {
		opts = append(opts, config.WithRegion(req.Region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return fmt.Errorf("aws config: %w", err)
	}
	return dispatch(ctx, req, ec2.NewFromConfig(cfg), out)
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
		case "allow-reassignment", "no-source-dest-check":
			req.Flags[name] = "true"
		case "output", "profile":
			i++
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
	if len(req.Words) < 2 {
		return req, fmt.Errorf("unsupported AWS command %q", strings.Join(argv, " "))
	}
	return req, nil
}

func dispatch(ctx context.Context, req cliRequest, client ec2API, out io.Writer) error {
	switch strings.Join(req.Words, " ") {
	case "ec2 describe-network-interfaces":
		eni := req.Flags["network-interface-ids"]
		resp, err := client.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
			NetworkInterfaceIds: splitComma(eni),
			Filters:             ec2Filters(req.Flags["filters"]),
		})
		if err != nil {
			return err
		}
		items := make([]map[string]any, 0, len(resp.NetworkInterfaces))
		for _, item := range resp.NetworkInterfaces {
			items = append(items, networkInterfaceJSON(item))
		}
		return writeJSON(out, map[string]any{"NetworkInterfaces": items})
	case "ec2 describe-instances":
		resp, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			Filters: ec2Filters(req.Flags["filters"]),
		})
		if err != nil {
			return err
		}
		items := make([]map[string]any, 0, len(resp.Reservations))
		for _, item := range resp.Reservations {
			items = append(items, reservationJSON(item))
		}
		return writeJSON(out, map[string]any{"Reservations": items})
	case "ec2 assign-private-ip-addresses":
		eni := req.Flags["network-interface-id"]
		ip := bareIP(req.Flags["private-ip-addresses"])
		if eni == "" || ip == "" {
			return errors.New("ec2 assign-private-ip-addresses requires --network-interface-id and --private-ip-addresses")
		}
		_, err := client.AssignPrivateIpAddresses(ctx, &ec2.AssignPrivateIpAddressesInput{
			NetworkInterfaceId: aws.String(eni),
			PrivateIpAddresses: []string{ip},
			AllowReassignment:  aws.Bool(req.Flags["allow-reassignment"] == "true"),
		})
		return err
	case "ec2 unassign-private-ip-addresses":
		eni := req.Flags["network-interface-id"]
		ip := bareIP(req.Flags["private-ip-addresses"])
		if eni == "" || ip == "" {
			return errors.New("ec2 unassign-private-ip-addresses requires --network-interface-id and --private-ip-addresses")
		}
		_, err := client.UnassignPrivateIpAddresses(ctx, &ec2.UnassignPrivateIpAddressesInput{
			NetworkInterfaceId: aws.String(eni),
			PrivateIpAddresses: []string{ip},
		})
		return err
	case "ec2 modify-network-interface-attribute":
		eni := req.Flags["network-interface-id"]
		if eni == "" {
			return errors.New("ec2 modify-network-interface-attribute requires --network-interface-id")
		}
		sourceDestCheck, err := sourceDestCheckValue(req.Flags)
		if err != nil {
			return err
		}
		_, err = client.ModifyNetworkInterfaceAttribute(ctx, &ec2.ModifyNetworkInterfaceAttributeInput{
			NetworkInterfaceId: aws.String(eni),
			SourceDestCheck:    &types.AttributeBooleanValue{Value: aws.Bool(sourceDestCheck)},
		})
		return err
	case "ec2 describe-route-tables":
		routeTableID := req.Flags["route-table-ids"]
		if routeTableID == "" {
			return errors.New("ec2 describe-route-tables requires --route-table-ids")
		}
		resp, err := client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
			RouteTableIds: []string{routeTableID},
		})
		if err != nil {
			return err
		}
		items := make([]map[string]any, 0, len(resp.RouteTables))
		for _, item := range resp.RouteTables {
			items = append(items, routeTableJSON(item))
		}
		return writeJSON(out, map[string]any{"RouteTables": items})
	case "ec2 create-route":
		return createRoute(ctx, client, req.Flags)
	case "ec2 replace-route":
		return replaceRoute(ctx, client, req.Flags)
	case "ec2 delete-route":
		return deleteRoute(ctx, client, req.Flags)
	default:
		return fmt.Errorf("unsupported AWS command %q", strings.Join(req.Words, " "))
	}
}

func createRoute(ctx context.Context, client ec2API, flags map[string]string) error {
	routeTableID, destination, eni, err := requireRouteFlags(flags)
	if err != nil {
		return fmt.Errorf("ec2 create-route: %w", err)
	}
	_, err = client.CreateRoute(ctx, &ec2.CreateRouteInput{
		RouteTableId:         aws.String(routeTableID),
		DestinationCidrBlock: aws.String(destination),
		NetworkInterfaceId:   aws.String(eni),
	})
	return err
}

func replaceRoute(ctx context.Context, client ec2API, flags map[string]string) error {
	routeTableID, destination, eni, err := requireRouteFlags(flags)
	if err != nil {
		return fmt.Errorf("ec2 replace-route: %w", err)
	}
	_, err = client.ReplaceRoute(ctx, &ec2.ReplaceRouteInput{
		RouteTableId:         aws.String(routeTableID),
		DestinationCidrBlock: aws.String(destination),
		NetworkInterfaceId:   aws.String(eni),
	})
	return err
}

func deleteRoute(ctx context.Context, client ec2API, flags map[string]string) error {
	routeTableID := flags["route-table-id"]
	destination := flags["destination-cidr-block"]
	if routeTableID == "" || destination == "" {
		return errors.New("ec2 delete-route requires --route-table-id and --destination-cidr-block")
	}
	_, err := client.DeleteRoute(ctx, &ec2.DeleteRouteInput{
		RouteTableId:         aws.String(routeTableID),
		DestinationCidrBlock: aws.String(destination),
	})
	return err
}

func requireRouteFlags(flags map[string]string) (routeTableID, destination, eni string, err error) {
	routeTableID = flags["route-table-id"]
	destination = flags["destination-cidr-block"]
	eni = flags["network-interface-id"]
	if routeTableID == "" || destination == "" || eni == "" {
		return "", "", "", errors.New("requires --route-table-id, --destination-cidr-block, and --network-interface-id")
	}
	return routeTableID, destination, eni, nil
}

func splitComma(raw string) []string {
	var out []string
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func ec2Filters(raw string) []types.Filter {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	nameRaw, valuesRaw, ok := strings.Cut(raw, ",Values=")
	if !ok {
		return nil
	}
	name, ok := strings.CutPrefix(strings.TrimSpace(nameRaw), "Name=")
	if !ok {
		return nil
	}
	values := splitComma(valuesRaw)
	name = strings.TrimSpace(name)
	if name == "" || len(values) == 0 {
		return nil
	}
	return []types.Filter{{Name: aws.String(name), Values: values}}
}

func sourceDestCheckValue(flags map[string]string) (bool, error) {
	if flags["no-source-dest-check"] == "true" {
		return false, nil
	}
	raw := strings.TrimSpace(flags["source-dest-check"])
	if raw == "" {
		return false, errors.New("ec2 modify-network-interface-attribute requires --source-dest-check or --no-source-dest-check")
	}
	switch strings.ToLower(raw) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	}
	var body struct {
		Value bool `json:"Value"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return false, fmt.Errorf("invalid --source-dest-check value %q", raw)
	}
	return body.Value, nil
}

func networkInterfaceJSON(ni types.NetworkInterface) map[string]any {
	privateIPs := make([]map[string]any, 0, len(ni.PrivateIpAddresses))
	for _, ip := range ni.PrivateIpAddresses {
		privateIPs = append(privateIPs, map[string]any{
			"PrivateIpAddress": aws.ToString(ip.PrivateIpAddress),
			"Primary":          aws.ToBool(ip.Primary),
		})
	}
	tags := make([]map[string]any, 0, len(ni.TagSet))
	for _, tag := range ni.TagSet {
		tags = append(tags, map[string]any{
			"Key":   aws.ToString(tag.Key),
			"Value": aws.ToString(tag.Value),
		})
	}
	return map[string]any{
		"NetworkInterfaceId": aws.ToString(ni.NetworkInterfaceId),
		"SubnetId":           aws.ToString(ni.SubnetId),
		"SourceDestCheck":    aws.ToBool(ni.SourceDestCheck),
		"PrivateIpAddresses": privateIPs,
		"TagSet":             tags,
	}
}

func reservationJSON(res types.Reservation) map[string]any {
	instances := make([]map[string]any, 0, len(res.Instances))
	for _, instance := range res.Instances {
		interfaces := make([]map[string]any, 0, len(instance.NetworkInterfaces))
		for _, ni := range instance.NetworkInterfaces {
			interfaces = append(interfaces, map[string]any{
				"NetworkInterfaceId": aws.ToString(ni.NetworkInterfaceId),
			})
		}
		instances = append(instances, map[string]any{
			"InstanceId":        aws.ToString(instance.InstanceId),
			"State":             map[string]any{"Name": string(instance.State.Name)},
			"NetworkInterfaces": interfaces,
		})
	}
	return map[string]any{"Instances": instances}
}

func routeTableJSON(rt types.RouteTable) map[string]any {
	routes := make([]map[string]any, 0, len(rt.Routes))
	for _, route := range rt.Routes {
		routes = append(routes, map[string]any{
			"DestinationCidrBlock": aws.ToString(route.DestinationCidrBlock),
			"NetworkInterfaceId":   aws.ToString(route.NetworkInterfaceId),
		})
	}
	return map[string]any{
		"RouteTableId": aws.ToString(rt.RouteTableId),
		"Routes":       routes,
	}
}

func writeJSON(out io.Writer, data any) error {
	enc := json.NewEncoder(out)
	return enc.Encode(data)
}

func bareIP(address string) string {
	address = strings.TrimSpace(address)
	if ip, _, err := net.ParseCIDR(address); err == nil {
		return ip.String()
	}
	return address
}
