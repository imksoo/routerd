// SPDX-License-Identifier: BSD-3-Clause

// Command azure-routerd-helper is the minimal Azure Network control-plane helper
// routerd ships for CloudEdge SAM. It intentionally implements only the small
// Azure-CLI-compatible command surface used by azure-provider-executor, backed
// by Azure SDK managed identity credentials.
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

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
)

const (
	defaultTimeout  = 120 * time.Second
	helperVersion   = "azure-routerd-helper/v1"
	managementScope = "https://management.azure.com/.default"
)

type cliRequest struct {
	Words []string
	Flags map[string]string
	Sets  map[string]string
}

type azureAPI interface {
	GetNIC(ctx context.Context, resourceGroup, nicName string) (armnetwork.Interface, error)
	ListNICs(ctx context.Context, resourceGroup string) ([]armnetwork.Interface, error)
	PutNIC(ctx context.Context, resourceGroup, nicName string, nic armnetwork.Interface) (armnetwork.Interface, error)
	GetRouteTable(ctx context.Context, resourceGroup, routeTableName string) (armnetwork.RouteTable, error)
	GetRoute(ctx context.Context, resourceGroup, routeTableName, routeName string) (armnetwork.Route, error)
	PutRoute(ctx context.Context, resourceGroup, routeTableName, routeName string, route armnetwork.Route) (armnetwork.Route, error)
	DeleteRoute(ctx context.Context, resourceGroup, routeTableName, routeName string) error
}

type sdkAzureAPI struct {
	interfaces  *armnetwork.InterfacesClient
	routeTables *armnetwork.RouteTablesClient
	routes      *armnetwork.RoutesClient
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "azure-routerd-helper: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, argv []string, out io.Writer) error {
	req, err := parseArgs(argv)
	if err != nil {
		return err
	}
	switch strings.Join(req.Words, " ") {
	case "version":
		return writeJSON(out, map[string]any{"version": helperVersion})
	case "preflight":
		return runPreflight(ctx, out)
	}
	subscriptionID, err := subscriptionFromRequest(req)
	if err != nil {
		return err
	}
	cred, err := azidentity.NewManagedIdentityCredential(nil)
	if err != nil {
		return fmt.Errorf("managed identity credential: %w", err)
	}
	api, err := newSDKAzureAPI(subscriptionID, cred)
	if err != nil {
		return err
	}
	return dispatch(ctx, req, api, out)
}

func runPreflight(ctx context.Context, out io.Writer) error {
	cred, err := azidentity.NewManagedIdentityCredential(nil)
	if err != nil {
		return fmt.Errorf("managed identity credential: %w", err)
	}
	_, err = cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{managementScope}})
	if err != nil {
		return fmt.Errorf("managed identity token: %w", err)
	}
	return writeJSON(out, map[string]any{
		"version":           helperVersion,
		"managedIdentity":   "ok",
		"managementScope":   managementScope,
		"subscriptionProbe": "not-requested",
	})
}

func newSDKAzureAPI(subscriptionID string, cred azcore.TokenCredential) (*sdkAzureAPI, error) {
	interfaces, err := armnetwork.NewInterfacesClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("interfaces client: %w", err)
	}
	routeTables, err := armnetwork.NewRouteTablesClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("route tables client: %w", err)
	}
	routes, err := armnetwork.NewRoutesClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("routes client: %w", err)
	}
	return &sdkAzureAPI{interfaces: interfaces, routeTables: routeTables, routes: routes}, nil
}

func (a *sdkAzureAPI) GetNIC(ctx context.Context, resourceGroup, nicName string) (armnetwork.Interface, error) {
	resp, err := a.interfaces.Get(ctx, resourceGroup, nicName, nil)
	if err != nil {
		return armnetwork.Interface{}, err
	}
	return resp.Interface, nil
}

func (a *sdkAzureAPI) ListNICs(ctx context.Context, resourceGroup string) ([]armnetwork.Interface, error) {
	var out []armnetwork.Interface
	pager := a.interfaces.NewListPager(resourceGroup, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, nic := range page.Value {
			if nic != nil {
				out = append(out, *nic)
			}
		}
	}
	return out, nil
}

func (a *sdkAzureAPI) PutNIC(ctx context.Context, resourceGroup, nicName string, nic armnetwork.Interface) (armnetwork.Interface, error) {
	poller, err := a.interfaces.BeginCreateOrUpdate(ctx, resourceGroup, nicName, nic, nil)
	if err != nil {
		return armnetwork.Interface{}, err
	}
	resp, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		return armnetwork.Interface{}, err
	}
	return resp.Interface, nil
}

func (a *sdkAzureAPI) GetRouteTable(ctx context.Context, resourceGroup, routeTableName string) (armnetwork.RouteTable, error) {
	resp, err := a.routeTables.Get(ctx, resourceGroup, routeTableName, nil)
	if err != nil {
		return armnetwork.RouteTable{}, err
	}
	return resp.RouteTable, nil
}

func (a *sdkAzureAPI) GetRoute(ctx context.Context, resourceGroup, routeTableName, routeName string) (armnetwork.Route, error) {
	resp, err := a.routes.Get(ctx, resourceGroup, routeTableName, routeName, nil)
	if err != nil {
		return armnetwork.Route{}, err
	}
	return resp.Route, nil
}

func (a *sdkAzureAPI) PutRoute(ctx context.Context, resourceGroup, routeTableName, routeName string, route armnetwork.Route) (armnetwork.Route, error) {
	poller, err := a.routes.BeginCreateOrUpdate(ctx, resourceGroup, routeTableName, routeName, route, nil)
	if err != nil {
		return armnetwork.Route{}, err
	}
	resp, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		return armnetwork.Route{}, err
	}
	return resp.Route, nil
}

func (a *sdkAzureAPI) DeleteRoute(ctx context.Context, resourceGroup, routeTableName, routeName string) error {
	poller, err := a.routes.BeginDelete(ctx, resourceGroup, routeTableName, routeName, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

func parseArgs(argv []string) (cliRequest, error) {
	req := cliRequest{Flags: map[string]string{}, Sets: map[string]string{}}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if !strings.HasPrefix(arg, "--") {
			req.Words = append(req.Words, arg)
			continue
		}
		name := strings.TrimPrefix(arg, "--")
		switch name {
		case "only-show-errors", "yes":
			req.Flags[name] = "true"
		case "output":
			i++
		case "set":
			for i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "--") {
				i++
				key, value, ok := strings.Cut(argv[i], "=")
				if !ok {
					return req, fmt.Errorf("--set entry %q must be key=value", argv[i])
				}
				req.Sets[key] = value
			}
		default:
			if i+1 >= len(argv) || strings.HasPrefix(argv[i+1], "--") {
				req.Flags[name] = "true"
				continue
			}
			i++
			req.Flags[name] = argv[i]
		}
	}
	if len(req.Words) == 0 {
		return req, fmt.Errorf("unsupported Azure command %q", strings.Join(argv, " "))
	}
	return req, nil
}

func dispatch(ctx context.Context, req cliRequest, api azureAPI, out io.Writer) error {
	switch strings.Join(req.Words, " ") {
	case "network nic show":
		ref, err := nicRefFromID(req.Flags["ids"])
		if err != nil {
			return err
		}
		nic, err := api.GetNIC(ctx, ref.ResourceGroup, ref.Name)
		if err != nil {
			return err
		}
		return writeJSON(out, nicJSON(nic, ref.ResourceGroup))
	case "network nic list":
		resourceGroup := req.Flags["resource-group"]
		if resourceGroup == "" {
			return errors.New("network nic list requires --resource-group")
		}
		nics, err := api.ListNICs(ctx, resourceGroup)
		if err != nil {
			return err
		}
		body := make([]map[string]any, 0, len(nics))
		for _, nic := range nics {
			body = append(body, nicJSON(nic, resourceGroup))
		}
		return writeJSON(out, body)
	case "network nic ip-config list":
		resourceGroup, nicName, err := requireNICNameFlags(req.Flags)
		if err != nil {
			return err
		}
		nic, err := api.GetNIC(ctx, resourceGroup, nicName)
		if err != nil {
			return err
		}
		return writeJSON(out, ipConfigsJSON(nic))
	case "network nic ip-config create":
		return createIPConfig(ctx, req.Flags, api, out)
	case "network nic ip-config delete":
		return deleteIPConfig(ctx, req.Flags, api, out)
	case "network nic update":
		return updateNIC(ctx, req.Flags, api, out)
	case "network route-table show":
		resourceGroup, routeTableName, err := requireRouteTableFlags(req.Flags)
		if err != nil {
			return err
		}
		rt, err := api.GetRouteTable(ctx, resourceGroup, routeTableName)
		if err != nil {
			return err
		}
		return writeJSON(out, routeTableJSON(rt))
	case "network route-table route show":
		resourceGroup, routeTableName, routeName, err := requireRouteFlags(req.Flags)
		if err != nil {
			return err
		}
		route, err := api.GetRoute(ctx, resourceGroup, routeTableName, routeName)
		if err != nil {
			return err
		}
		return writeJSON(out, routeJSON(route))
	case "network route-table route create":
		return putRoute(ctx, req.Flags, req.Flags, api, out)
	case "network route-table route update":
		return putRoute(ctx, req.Flags, req.Sets, api, out)
	case "network route-table route delete":
		resourceGroup, routeTableName, routeName, err := requireRouteFlags(req.Flags)
		if err != nil {
			return err
		}
		return api.DeleteRoute(ctx, resourceGroup, routeTableName, routeName)
	default:
		return fmt.Errorf("unsupported Azure command %q", strings.Join(req.Words, " "))
	}
}

type resourceRef struct {
	SubscriptionID string
	ResourceGroup  string
	Name           string
}

func subscriptionFromRequest(req cliRequest) (string, error) {
	if id := req.Flags["ids"]; id != "" {
		ref, err := parseARMID(id)
		if err != nil {
			return "", err
		}
		return ref.SubscriptionID, nil
	}
	if sub := req.Flags["subscription"]; sub != "" {
		return sub, nil
	}
	return "", errors.New("subscription is required via --ids <ARM ID> or --subscription")
}

func nicRefFromID(id string) (resourceRef, error) {
	if strings.TrimSpace(id) == "" {
		return resourceRef{}, errors.New("network nic show/update requires --ids")
	}
	return parseARMID(id)
}

func parseARMID(id string) (resourceRef, error) {
	parsed, err := arm.ParseResourceID(id)
	if err != nil {
		return resourceRef{}, err
	}
	if parsed.SubscriptionID == "" || parsed.ResourceGroupName == "" || parsed.Name == "" {
		return resourceRef{}, fmt.Errorf("ARM ID %q must include subscription, resource group, and resource name", id)
	}
	return resourceRef{SubscriptionID: parsed.SubscriptionID, ResourceGroup: parsed.ResourceGroupName, Name: parsed.Name}, nil
}

func requireNICNameFlags(flags map[string]string) (resourceGroup, nicName string, err error) {
	resourceGroup = flags["resource-group"]
	nicName = flags["nic-name"]
	if resourceGroup == "" || nicName == "" {
		return "", "", errors.New("network nic ip-config requires --resource-group and --nic-name")
	}
	return resourceGroup, nicName, nil
}

func requireRouteTableFlags(flags map[string]string) (resourceGroup, routeTableName string, err error) {
	resourceGroup = flags["resource-group"]
	routeTableName = firstNonEmpty(flags["route-table-name"], flags["name"])
	if resourceGroup == "" || routeTableName == "" {
		return "", "", errors.New("network route-table requires --resource-group and --name/--route-table-name")
	}
	return resourceGroup, routeTableName, nil
}

func requireRouteFlags(flags map[string]string) (resourceGroup, routeTableName, routeName string, err error) {
	resourceGroup = flags["resource-group"]
	routeTableName = flags["route-table-name"]
	routeName = flags["name"]
	if resourceGroup == "" || routeTableName == "" || routeName == "" {
		return "", "", "", errors.New("network route-table route requires --resource-group, --route-table-name, and --name")
	}
	return resourceGroup, routeTableName, routeName, nil
}

func createIPConfig(ctx context.Context, flags map[string]string, api azureAPI, out io.Writer) error {
	resourceGroup, nicName, err := requireNICNameFlags(flags)
	if err != nil {
		return err
	}
	name := flags["name"]
	address := bareIP(flags["private-ip-address"])
	if name == "" || address == "" {
		return errors.New("network nic ip-config create requires --name and --private-ip-address")
	}
	nic, err := api.GetNIC(ctx, resourceGroup, nicName)
	if err != nil {
		return err
	}
	if findIPConfig(nic, name) != nil {
		return fmt.Errorf("ip-config %s already exists", name)
	}
	template := primaryIPConfig(nic)
	if template == nil || template.Properties == nil || template.Properties.Subnet == nil {
		return fmt.Errorf("NIC %s has no primary ipConfiguration subnet to copy", nicName)
	}
	cfg := &armnetwork.InterfaceIPConfiguration{
		Name: &name,
		Properties: &armnetwork.InterfaceIPConfigurationPropertiesFormat{
			PrivateIPAddress:          &address,
			PrivateIPAllocationMethod: ptr(armnetwork.IPAllocationMethodStatic),
			Subnet:                    template.Properties.Subnet,
		},
	}
	ensureNICProperties(&nic)
	nic.Properties.IPConfigurations = append(nic.Properties.IPConfigurations, cfg)
	updated, err := api.PutNIC(ctx, resourceGroup, nicName, nic)
	if err != nil {
		return err
	}
	return writeJSON(out, ipConfigJSON(findIPConfig(updated, name)))
}

func deleteIPConfig(ctx context.Context, flags map[string]string, api azureAPI, out io.Writer) error {
	resourceGroup, nicName, err := requireNICNameFlags(flags)
	if err != nil {
		return err
	}
	name := flags["name"]
	if name == "" {
		return errors.New("network nic ip-config delete requires --name")
	}
	nic, err := api.GetNIC(ctx, resourceGroup, nicName)
	if err != nil {
		return err
	}
	ensureNICProperties(&nic)
	next := nic.Properties.IPConfigurations[:0]
	found := false
	for _, cfg := range nic.Properties.IPConfigurations {
		if cfg == nil || !strings.EqualFold(stringPtr(cfg.Name), name) {
			next = append(next, cfg)
			continue
		}
		if cfg.Properties != nil && boolPtr(cfg.Properties.Primary) {
			return fmt.Errorf("refusing to delete primary ip-config %s", name)
		}
		found = true
	}
	if !found {
		return fmt.Errorf("ip-config %s not found", name)
	}
	nic.Properties.IPConfigurations = next
	updated, err := api.PutNIC(ctx, resourceGroup, nicName, nic)
	if err != nil {
		return err
	}
	return writeJSON(out, nicJSON(updated, resourceGroup))
}

func updateNIC(ctx context.Context, flags map[string]string, api azureAPI, out io.Writer) error {
	ref, err := nicRefFromID(flags["ids"])
	if err != nil {
		return err
	}
	forwarding, err := boolFlag(flags["ip-forwarding"])
	if err != nil {
		return err
	}
	nic, err := api.GetNIC(ctx, ref.ResourceGroup, ref.Name)
	if err != nil {
		return err
	}
	ensureNICProperties(&nic)
	nic.Properties.EnableIPForwarding = &forwarding
	updated, err := api.PutNIC(ctx, ref.ResourceGroup, ref.Name, nic)
	if err != nil {
		return err
	}
	return writeJSON(out, nicJSON(updated, ref.ResourceGroup))
}

func putRoute(ctx context.Context, flags, values map[string]string, api azureAPI, out io.Writer) error {
	resourceGroup, routeTableName, routeName, err := requireRouteFlags(flags)
	if err != nil {
		return err
	}
	address := firstNonEmpty(values["address-prefix"], values["addressPrefix"])
	nextHopType := firstNonEmpty(values["next-hop-type"], values["nextHopType"])
	nextHopIP := firstNonEmpty(values["next-hop-ip-address"], values["nextHopIpAddress"])
	if address == "" || nextHopType == "" {
		return errors.New("route create/update requires addressPrefix and nextHopType")
	}
	route := armnetwork.Route{
		Name: &routeName,
		Properties: &armnetwork.RoutePropertiesFormat{
			AddressPrefix: &address,
			NextHopType:   ptr(armnetwork.RouteNextHopType(nextHopType)),
		},
	}
	if nextHopIP != "" {
		route.Properties.NextHopIPAddress = &nextHopIP
	}
	updated, err := api.PutRoute(ctx, resourceGroup, routeTableName, routeName, route)
	if err != nil {
		return err
	}
	return writeJSON(out, routeJSON(updated))
}

func ensureNICProperties(nic *armnetwork.Interface) {
	if nic.Properties == nil {
		nic.Properties = &armnetwork.InterfacePropertiesFormat{}
	}
}

func primaryIPConfig(nic armnetwork.Interface) *armnetwork.InterfaceIPConfiguration {
	if nic.Properties == nil {
		return nil
	}
	for _, cfg := range nic.Properties.IPConfigurations {
		if cfg != nil && cfg.Properties != nil && boolPtr(cfg.Properties.Primary) {
			return cfg
		}
	}
	if len(nic.Properties.IPConfigurations) > 0 {
		return nic.Properties.IPConfigurations[0]
	}
	return nil
}

func findIPConfig(nic armnetwork.Interface, name string) *armnetwork.InterfaceIPConfiguration {
	if nic.Properties == nil {
		return nil
	}
	for _, cfg := range nic.Properties.IPConfigurations {
		if cfg != nil && strings.EqualFold(stringPtr(cfg.Name), name) {
			return cfg
		}
	}
	return nil
}

func nicJSON(nic armnetwork.Interface, defaultResourceGroup string) map[string]any {
	resourceGroup := defaultResourceGroup
	if id := stringPtr(nic.ID); id != "" {
		if ref, err := parseARMID(id); err == nil && ref.ResourceGroup != "" {
			resourceGroup = ref.ResourceGroup
		}
	}
	return map[string]any{
		"id":                 stringPtr(nic.ID),
		"name":               stringPtr(nic.Name),
		"resourceGroup":      resourceGroup,
		"enableIPForwarding": nic.Properties != nil && boolPtr(nic.Properties.EnableIPForwarding),
		"ipConfigurations":   ipConfigsJSON(nic),
	}
}

func ipConfigsJSON(nic armnetwork.Interface) []map[string]any {
	if nic.Properties == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(nic.Properties.IPConfigurations))
	for _, cfg := range nic.Properties.IPConfigurations {
		if cfg == nil {
			continue
		}
		out = append(out, ipConfigJSON(cfg))
	}
	return out
}

func ipConfigJSON(cfg *armnetwork.InterfaceIPConfiguration) map[string]any {
	if cfg == nil {
		return map[string]any{}
	}
	address := ""
	primary := false
	if cfg.Properties != nil {
		address = stringPtr(cfg.Properties.PrivateIPAddress)
		primary = boolPtr(cfg.Properties.Primary)
	}
	return map[string]any{
		"id":               stringPtr(cfg.ID),
		"name":             stringPtr(cfg.Name),
		"privateIPAddress": address,
		"privateIpAddress": address,
		"primary":          primary,
	}
}

func routeTableJSON(rt armnetwork.RouteTable) map[string]any {
	var routes []map[string]any
	if rt.Properties != nil {
		for _, route := range rt.Properties.Routes {
			if route != nil {
				routes = append(routes, routeJSON(*route))
			}
		}
	}
	return map[string]any{
		"id":     stringPtr(rt.ID),
		"name":   stringPtr(rt.Name),
		"routes": routes,
	}
}

func routeJSON(route armnetwork.Route) map[string]any {
	addressPrefix := ""
	nextHopType := ""
	nextHopIP := ""
	if route.Properties != nil {
		addressPrefix = stringPtr(route.Properties.AddressPrefix)
		if route.Properties.NextHopType != nil {
			nextHopType = string(*route.Properties.NextHopType)
		}
		nextHopIP = stringPtr(route.Properties.NextHopIPAddress)
	}
	return map[string]any{
		"id":               stringPtr(route.ID),
		"name":             stringPtr(route.Name),
		"addressPrefix":    addressPrefix,
		"nextHopType":      nextHopType,
		"nextHopIpAddress": nextHopIP,
		"properties": map[string]any{
			"addressPrefix":    addressPrefix,
			"nextHopType":      nextHopType,
			"nextHopIpAddress": nextHopIP,
		},
	}
}

func writeJSON(out io.Writer, data any) error {
	enc := json.NewEncoder(out)
	return enc.Encode(data)
}

func boolFlag(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value %q", v)
	}
}

func bareIP(address string) string {
	address = strings.TrimSpace(address)
	if ip, _, err := net.ParseCIDR(address); err == nil {
		return ip.String()
	}
	return address
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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

func ptr[T any](v T) *T {
	return &v
}
