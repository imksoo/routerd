package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/invopop/jsonschema"

	"routerd/pkg/api"
	"routerd/pkg/controlapi"
)

func main() {
	schemaName := flag.String("schema", "config", "schema to generate: config, control, or control-openapi")
	flag.Parse()
	var schema map[string]any
	switch *schemaName {
	case "config":
		schema = configSchema()
	case "control":
		schema = controlSchema()
	case "control-openapi":
		schema = controlOpenAPISchema()
	default:
		fmt.Fprintf(os.Stderr, "unknown schema %q\n", *schemaName)
		os.Exit(2)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(schema); err != nil {
		fmt.Fprintf(os.Stderr, "encode schema: %v\n", err)
		os.Exit(1)
	}
}

func configSchema() map[string]any {
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://routerd.net/schemas/routerd-config-v1alpha1.schema.json",
		"title":   "routerd config v1alpha1",
		"type":    "object",
		"required": []string{
			"apiVersion",
			"kind",
			"metadata",
			"spec",
		},
		"additionalProperties": false,
		"properties": map[string]any{
			"apiVersion": constString(api.RouterAPIVersion),
			"kind":       constString("Router"),
			"metadata":   metadataSchema(),
			"spec": map[string]any{
				"type":                 "object",
				"required":             []string{"resources"},
				"additionalProperties": false,
				"properties": map[string]any{
					"reconcile": reflectedSchema(api.ApplyPolicySpec{}),
					"resources": map[string]any{
						"type":  "array",
						"items": resourceUnionSchema(),
					},
				},
			},
		},
	}
	return schema
}

func controlSchema() map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://routerd.net/schemas/routerd-control-v1alpha1.schema.json",
		"title":   "routerd control API v1alpha1",
		"oneOf": []any{
			reflectedSchema(controlapi.Status{}),
			reflectedSchema(controlapi.ConnectionTable{}),
			reflectedSchema(controlapi.DNSQueries{}),
			reflectedSchema(controlapi.TrafficFlows{}),
			reflectedSchema(controlapi.FirewallLogs{}),
			reflectedSchema(controlapi.ApplyRequest{}),
			reflectedSchema(controlapi.ApplyResult{}),
			reflectedSchema(controlapi.DeleteRequest{}),
			reflectedSchema(controlapi.DeleteResult{}),
			reflectedSchema(controlapi.DHCPv6EventRequest{}),
			reflectedSchema(controlapi.DHCPv6EventResult{}),
			reflectedSchema(controlapi.Error{}),
		},
	}
}

func controlOpenAPISchema() map[string]any {
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "routerd control API",
			"version": "v1alpha1",
		},
		"paths": map[string]any{
			controlapi.Prefix + "/status": map[string]any{
				"get": map[string]any{
					"operationId": "getStatus",
					"responses": map[string]any{
						"200":     responseRef("Status"),
						"default": responseRef("Error"),
					},
				},
			},
			controlapi.Prefix + "/apply": map[string]any{
				"post": map[string]any{
					"operationId": "apply",
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": schemaRef("ApplyRequest"),
							},
						},
					},
					"responses": map[string]any{
						"200":     responseRef("ApplyResult"),
						"default": responseRef("Error"),
					},
				},
			},
			controlapi.Prefix + "/delete": map[string]any{
				"post": map[string]any{
					"operationId": "deleteResource",
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": schemaRef("DeleteRequest"),
							},
						},
					},
					"responses": map[string]any{
						"200":     responseRef("DeleteResult"),
						"default": responseRef("Error"),
					},
				},
			},
			controlapi.Prefix + "/connections": map[string]any{
				"get": map[string]any{
					"operationId": "getConnectionTable",
					"parameters": []any{
						map[string]any{
							"name":        "limit",
							"in":          "query",
							"required":    false,
							"description": "Maximum number of entries to return. 0 returns only summary.",
							"schema": map[string]any{
								"type":    "integer",
								"minimum": 0,
								"default": 100,
							},
						},
					},
					"responses": map[string]any{
						"200":     responseRef("ConnectionTable"),
						"default": responseRef("Error"),
					},
				},
			},
			controlapi.Prefix + "/dns-queries": logRowsPath("getDNSQueries", "DNSQueries", []any{
				queryParam("since", "Duration to look back from now.", "string", "1h"),
				queryParam("client", "Client IP address filter.", "string", ""),
				queryParam("qname", "Question name LIKE pattern.", "string", ""),
				queryParam("limit", "Maximum number of rows.", "integer", 100),
			}),
			controlapi.Prefix + "/traffic-flows": logRowsPath("getTrafficFlows", "TrafficFlows", []any{
				queryParam("since", "Duration to look back from now.", "string", "1h"),
				queryParam("client", "Client IP address filter.", "string", ""),
				queryParam("peer", "Peer IP address filter.", "string", ""),
				queryParam("limit", "Maximum number of rows.", "integer", 100),
			}),
			controlapi.Prefix + "/firewall-logs": logRowsPath("getFirewallLogs", "FirewallLogs", []any{
				queryParam("since", "Duration to look back from now.", "string", "1h"),
				queryParam("action", "Firewall action filter.", "string", ""),
				queryParam("src", "Source IP address filter.", "string", ""),
				queryParam("limit", "Maximum number of rows.", "integer", 100),
			}),
			controlapi.Prefix + "/dhcpv6-event": map[string]any{
				"post": map[string]any{
					"operationId": "recordDHCPv6Event",
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": schemaRef("DHCPv6EventRequest"),
							},
						},
					},
					"responses": map[string]any{
						"200":     responseRef("DHCPv6EventResult"),
						"default": responseRef("Error"),
					},
				},
			},
		},
		"components": map[string]any{
			"schemas": map[string]any{
				"Status":             reflectedSchema(controlapi.Status{}),
				"ConnectionTable":    reflectedSchema(controlapi.ConnectionTable{}),
				"DNSQueries":         reflectedSchema(controlapi.DNSQueries{}),
				"TrafficFlows":       reflectedSchema(controlapi.TrafficFlows{}),
				"FirewallLogs":       reflectedSchema(controlapi.FirewallLogs{}),
				"ApplyRequest":       reflectedSchema(controlapi.ApplyRequest{}),
				"ApplyResult":        reflectedSchema(controlapi.ApplyResult{}),
				"DeleteRequest":      reflectedSchema(controlapi.DeleteRequest{}),
				"DeleteResult":       reflectedSchema(controlapi.DeleteResult{}),
				"DHCPv6EventRequest": reflectedSchema(controlapi.DHCPv6EventRequest{}),
				"DHCPv6EventResult":  reflectedSchema(controlapi.DHCPv6EventResult{}),
				"Error":              reflectedSchema(controlapi.Error{}),
			},
		},
	}
}

func responseRef(name string) map[string]any {
	return map[string]any{
		"description": name,
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": schemaRef(name),
			},
		},
	}
}

func logRowsPath(operationID, responseName string, parameters []any) map[string]any {
	return map[string]any{
		"get": map[string]any{
			"operationId": operationID,
			"parameters":  parameters,
			"responses": map[string]any{
				"200":     responseRef(responseName),
				"default": responseRef("Error"),
			},
		},
	}
}

func queryParam(name, description, typ string, fallback any) map[string]any {
	schema := map[string]any{"type": typ}
	if fallback != "" {
		schema["default"] = fallback
	}
	if typ == "integer" {
		schema["minimum"] = 0
	}
	return map[string]any{
		"name":        name,
		"in":          "query",
		"required":    false,
		"description": description,
		"schema":      schema,
	}
}

func schemaRef(name string) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

func resourceUnionSchema() map[string]any {
	return map[string]any{
		"oneOf": []any{
			resourceSchema(api.SystemAPIVersion, "LogSink", api.LogSinkSpec{}),
			resourceSchema(api.SystemAPIVersion, "LogRetention", api.LogRetentionSpec{}),
			resourceSchema(api.SystemAPIVersion, "Sysctl", api.SysctlSpec{}),
			resourceSchema(api.SystemAPIVersion, "SysctlProfile", api.SysctlProfileSpec{}),
			resourceSchema(api.SystemAPIVersion, "Package", api.PackageSpec{}),
			resourceSchema(api.SystemAPIVersion, "NetworkAdoption", api.NetworkAdoptionSpec{}),
			resourceSchema(api.SystemAPIVersion, "SystemdUnit", api.SystemdUnitSpec{}),
			resourceSchema(api.SystemAPIVersion, "NTPClient", api.NTPClientSpec{}),
			resourceSchema(api.SystemAPIVersion, "WebConsole", api.WebConsoleSpec{}),
			resourceSchema(api.SystemAPIVersion, "NixOSHost", api.NixOSHostSpec{}),
			resourceSchema(api.NetAPIVersion, "Interface", api.InterfaceSpec{}),
			resourceSchema(api.NetAPIVersion, "Link", api.LinkSpec{}),
			resourceSchema(api.NetAPIVersion, "PPPoEInterface", api.PPPoEInterfaceSpec{}),
			resourceSchema(api.NetAPIVersion, "PPPoESession", api.PPPoESessionSpec{}),
			resourceSchema(api.NetAPIVersion, "WireGuardInterface", api.WireGuardInterfaceSpec{}),
			resourceSchema(api.NetAPIVersion, "WireGuardPeer", api.WireGuardPeerSpec{}),
			resourceSchema(api.NetAPIVersion, "IPsecConnection", api.IPsecConnectionSpec{}),
			resourceSchema(api.NetAPIVersion, "VRF", api.VRFSpec{}),
			resourceSchema(api.NetAPIVersion, "VXLANTunnel", api.VXLANTunnelSpec{}),
			resourceSchema(api.NetAPIVersion, "IPv4StaticAddress", api.IPv4StaticAddressSpec{}),
			resourceSchema(api.NetAPIVersion, "DHCPv4Address", api.DHCPv4AddressSpec{}),
			resourceSchema(api.NetAPIVersion, "DHCPv4Lease", api.DHCPv4LeaseSpec{}),
			resourceSchema(api.NetAPIVersion, "DHCPv4Server", api.DHCPv4ServerSpec{}),
			resourceSchema(api.NetAPIVersion, "DHCPv4Scope", api.DHCPv4ScopeSpec{}),
			resourceSchema(api.NetAPIVersion, "DHCPv4Reservation", api.DHCPv4ReservationSpec{}),
			resourceSchema(api.NetAPIVersion, "DHCPv6Address", api.DHCPv6AddressSpec{}),
			resourceSchema(api.NetAPIVersion, "IPv6RAAddress", api.IPv6RAAddressSpec{}),
			resourceSchema(api.NetAPIVersion, "DHCPv6PrefixDelegation", api.DHCPv6PrefixDelegationSpec{}),
			resourceSchema(api.NetAPIVersion, "IPv6DelegatedAddress", api.IPv6DelegatedAddressSpec{}),
			resourceSchema(api.NetAPIVersion, "DHCPv6Information", api.DHCPv6InformationSpec{}),
			resourceSchema(api.NetAPIVersion, "IPv6RouterAdvertisement", api.IPv6RouterAdvertisementSpec{}),
			resourceSchema(api.NetAPIVersion, "DHCPv6Server", api.DHCPv6ServerSpec{}),
			resourceSchema(api.NetAPIVersion, "DHCPv6Scope", api.DHCPv6ScopeSpec{}),
			resourceSchema(api.NetAPIVersion, "DHCPv4Relay", api.DHCPv4RelaySpec{}),
			resourceSchema(api.NetAPIVersion, "SelfAddressPolicy", api.SelfAddressPolicySpec{}),
			resourceSchema(api.NetAPIVersion, "DNSZone", api.DNSZoneSpec{}),
			resourceSchema(api.NetAPIVersion, "DNSResolver", api.DNSResolverSpec{}),
			resourceSchema(api.NetAPIVersion, "DSLiteTunnel", api.DSLiteTunnelSpec{}),
			resourceSchema(api.NetAPIVersion, "IPv4Route", api.IPv4RouteSpec{}),
			resourceSchema(api.NetAPIVersion, "StatePolicy", api.StatePolicySpec{}),
			resourceSchema(api.NetAPIVersion, "HealthCheck", api.HealthCheckSpec{}),
			resourceSchema(api.NetAPIVersion, "EgressRoutePolicy", api.EgressRoutePolicySpec{}),
			resourceSchema(api.NetAPIVersion, "EventRule", api.EventRuleSpec{}),
			resourceSchema(api.NetAPIVersion, "DerivedEvent", api.DerivedEventSpec{}),
			resourceSchema(api.NetAPIVersion, "IPv4DefaultRoutePolicy", api.IPv4DefaultRoutePolicySpec{}),
			resourceSchema(api.NetAPIVersion, "IPv4SourceNAT", api.IPv4SourceNATSpec{}),
			resourceSchema(api.NetAPIVersion, "NAT44Rule", api.NAT44RuleSpec{}),
			resourceSchema(api.NetAPIVersion, "IPv4PolicyRoute", api.IPv4PolicyRouteSpec{}),
			resourceSchema(api.NetAPIVersion, "IPv4PolicyRouteSet", api.IPv4PolicyRouteSetSpec{}),
			resourceSchema(api.NetAPIVersion, "IPv4ReversePathFilter", api.IPv4ReversePathFilterSpec{}),
			resourceSchema(api.NetAPIVersion, "PathMTUPolicy", api.PathMTUPolicySpec{}),
			resourceSchema(api.NetAPIVersion, "TrafficFlowLog", api.TrafficFlowLogSpec{}),
			resourceSchema(api.FirewallAPIVersion, "FirewallZone", api.FirewallZoneSpec{}),
			resourceSchema(api.FirewallAPIVersion, "FirewallPolicy", api.FirewallPolicySpec{}),
			resourceSchema(api.FirewallAPIVersion, "FirewallRule", api.FirewallRuleSpec{}),
			resourceSchema(api.FirewallAPIVersion, "FirewallLog", api.FirewallLogSpec{}),
			resourceSchema(api.NetAPIVersion, "Hostname", api.HostnameSpec{}),
		},
	}
}

func resourceSchema(apiVersion, kind string, spec any) map[string]any {
	return map[string]any{
		"type": "object",
		"required": []string{
			"apiVersion",
			"kind",
			"metadata",
			"spec",
		},
		"additionalProperties": false,
		"properties": map[string]any{
			"apiVersion": constString(apiVersion),
			"kind":       constString(kind),
			"metadata":   metadataSchema(),
			"spec":       reflectedSchema(spec),
			"status": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
		},
	}
}

func reflectedSchema(value any) map[string]any {
	reflector := jsonschema.Reflector{
		Anonymous:                 true,
		DoNotReference:            true,
		ExpandedStruct:            true,
		AllowAdditionalProperties: false,
	}
	data, err := json.Marshal(reflector.Reflect(value))
	if err != nil {
		panic(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		panic(err)
	}
	delete(schema, "$schema")
	delete(schema, "$id")
	return schema
}

func metadataSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"required":             []string{"name"},
		"additionalProperties": false,
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "minLength": 1},
		},
	}
}

func constString(value string) map[string]any {
	return map[string]any{
		"type":  "string",
		"const": value,
	}
}
