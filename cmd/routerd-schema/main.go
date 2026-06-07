// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync"

	"github.com/invopop/jsonschema"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
)

var (
	apiCommentMap  map[string]string
	apiCommentOnce sync.Once
	apiCommentErr  error
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
		"$defs": map[string]any{
			"ResourceWhen": resourceWhenSchema(),
		},
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
			reflectedSchema(controlapi.GetResult{}),
			reflectedSchema(controlapi.DescribeResult{}),
			reflectedSchema(controlapi.ProbeResult{}),
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
		"$id":     "https://routerd.net/schemas/routerd-control-openapi-v1alpha1.json",
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
			controlapi.Prefix + "/get": logRowsPath("getSubject", "GetResult", []any{
				queryParam("subject", "Inspection subject or resource target.", "string", "resources"),
				queryParam("limit", "Maximum number of runtime rows.", "integer", 100),
				queryParam("events-limit", "Recent events to attach per resource.", "integer", 10),
				queryParam("since-id", "Only events with an ID greater than this value.", "integer", 0),
				queryParam("topic", "Event topic filter.", "string", ""),
				queryParam("resource", "Event resource filter as kind/name.", "string", ""),
				queryParam("kind", "Event kind filter.", "string", ""),
				queryParam("name", "Event name filter.", "string", ""),
			}),
			controlapi.Prefix + "/describe": logRowsPath("describeResource", "DescribeResult", []any{
				queryParam("target", "Resource target as kind/name.", "string", ""),
				queryParam("events-limit", "Recent events to attach.", "integer", 10),
			}),
			controlapi.Prefix + "/probe": logRowsPath("probeSubject", "ProbeResult", []any{
				queryParam("subject", "Probe subject.", "string", ""),
				queryParam("target", "Optional probe target.", "string", ""),
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
				"GetResult":          reflectedSchema(controlapi.GetResult{}),
				"DescribeResult":     reflectedSchema(controlapi.DescribeResult{}),
				"ProbeResult":        reflectedSchema(controlapi.ProbeResult{}),
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
	resources := api.ConfigResourceKinds()
	schemas := make([]any, 0, len(resources))
	for _, resource := range resources {
		schemas = append(schemas, resourceSchema(resource.APIVersion, resource.Kind, resource.Spec))
	}
	return map[string]any{
		"oneOf": schemas,
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
		CommentMap:                apiComments(),
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
	patchResourceWhenSchemas(schema)
	return schema
}

func apiComments() map[string]string {
	apiCommentOnce.Do(func() {
		reflector := jsonschema.Reflector{}
		apiCommentErr = reflector.AddGoComments("github.com/imksoo/routerd", "./pkg/api")
		apiCommentMap = reflector.CommentMap
	})
	if apiCommentErr != nil {
		panic(apiCommentErr)
	}
	return apiCommentMap
}

func resourceWhenSchema() map[string]any {
	stateMatch := reflectedSchema(api.StateMatchSpec{})
	return map[string]any{
		"oneOf": []any{
			map[string]any{
				"type":                 "object",
				"required":             []string{"state"},
				"additionalProperties": false,
				"properties": map[string]any{
					"state": map[string]any{
						"type":                 "object",
						"minProperties":        1,
						"additionalProperties": stateMatch,
					},
				},
			},
			map[string]any{
				"type":                 "object",
				"required":             []string{"all"},
				"additionalProperties": false,
				"properties": map[string]any{
					"all": map[string]any{
						"type":     "array",
						"minItems": 1,
						"items":    map[string]any{"$ref": "#/$defs/ResourceWhen"},
					},
				},
			},
			map[string]any{
				"type":                 "object",
				"required":             []string{"any"},
				"additionalProperties": false,
				"properties": map[string]any{
					"any": map[string]any{
						"type":     "array",
						"minItems": 1,
						"items":    map[string]any{"$ref": "#/$defs/ResourceWhen"},
					},
				},
			},
		},
	}
}

func patchResourceWhenSchemas(value any) {
	switch node := value.(type) {
	case map[string]any:
		if node["title"] == "ResourceWhenSpec" {
			for key := range node {
				delete(node, key)
			}
			node["$ref"] = "#/$defs/ResourceWhen"
			return
		}
		for _, child := range node {
			patchResourceWhenSchemas(child)
		}
	case []any:
		for _, child := range node {
			patchResourceWhenSchemas(child)
		}
	}
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
