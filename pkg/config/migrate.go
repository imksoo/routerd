// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"bytes"
	"fmt"
	"io"
	"net/netip"
	"strings"

	"gopkg.in/yaml.v3"
)

// Migration is a checked legacy-to-current configuration change.
// It deliberately excludes values, so it is safe to report to an operator.
type Migration struct {
	Resource string
	Field    string
	Reason   string
}

// MigrateLegacyYAML converts only legacy fields whose replacement is explicit
// in the input. The caller must still admit and validate the returned YAML.
func MigrateLegacyYAML(data []byte, source string) ([]byte, []Migration, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, nil, fmt.Errorf("parse legacy config %s: %w", source, err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, nil, fmt.Errorf("parse legacy config %s: %w", source, err)
		}
		return nil, nil, fmt.Errorf("parse legacy config %s: multiple YAML documents are unsupported", source)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("parse legacy config %s: expected one mapping document", source)
	}
	resources := yamlMappingValue(yamlMappingValue(document.Content[0], "spec"), "resources")
	if resources == nil || resources.Kind != yaml.SequenceNode {
		return nil, nil, fmt.Errorf("parse legacy config %s: spec.resources is required", source)
	}

	zoneRoles := map[string]string{}
	for _, resource := range resources.Content {
		if yamlMappingScalar(resource, "kind") != "FirewallZone" {
			continue
		}
		spec := yamlMappingValue(resource, "spec")
		role := yamlMappingScalar(spec, "role")
		interfaces := yamlMappingValue(spec, "interfaces")
		if role == "" || interfaces == nil || interfaces.Kind != yaml.SequenceNode {
			continue
		}
		for _, reference := range interfaces.Content {
			if reference.Kind != yaml.ScalarNode || !strings.HasPrefix(reference.Value, "Interface/") {
				continue
			}
			name := strings.TrimPrefix(reference.Value, "Interface/")
			if prior, exists := zoneRoles[name]; exists && prior != role {
				return nil, nil, fmt.Errorf("legacy config %s: Interface/%s has conflicting FirewallZone roles %q and %q", source, name, prior, role)
			}
			zoneRoles[name] = role
		}
	}

	changes := []Migration{}
	for _, resource := range resources.Content {
		if yamlMappingScalar(resource, "kind") != "Interface" {
			continue
		}
		name := yamlMappingScalar(yamlMappingValue(resource, "metadata"), "name")
		spec := yamlMappingValue(resource, "spec")
		role := yamlMappingScalar(spec, "role")
		if role == "" {
			continue
		}
		if zoneRoles[name] != role {
			return nil, nil, fmt.Errorf("legacy config %s: Interface/%s spec.role=%q has no matching FirewallZone role", source, name, role)
		}
		yamlRemoveMappingKey(spec, "role")
		changes = append(changes, Migration{Resource: "Interface/" + name, Field: "spec.role", Reason: "FirewallZone preserves the role"})
	}
	for _, resource := range resources.Content {
		if yamlMappingScalar(resource, "kind") != "FirewallRule" {
			continue
		}
		name := yamlMappingScalar(yamlMappingValue(resource, "metadata"), "name")
		spec := yamlMappingValue(resource, "spec")
		legacyCIDRs := yamlMappingValue(spec, "sourceCIDRs")
		if legacyCIDRs == nil {
			continue
		}
		if yamlMappingValue(spec, "srcCIDRs") != nil {
			return nil, nil, fmt.Errorf("legacy config %s: FirewallRule/%s specifies both sourceCIDRs and srcCIDRs", source, name)
		}
		if legacyCIDRs.Kind != yaml.SequenceNode {
			return nil, nil, fmt.Errorf("legacy config %s: FirewallRule/%s sourceCIDRs must be a sequence", source, name)
		}
		for index, cidr := range legacyCIDRs.Content {
			if cidr.Kind != yaml.ScalarNode {
				return nil, nil, fmt.Errorf("legacy config %s: FirewallRule/%s sourceCIDRs[%d] must be a CIDR", source, name, index)
			}
			if _, err := netip.ParsePrefix(cidr.Value); err != nil {
				return nil, nil, fmt.Errorf("legacy config %s: FirewallRule/%s sourceCIDRs[%d] is invalid: %w", source, name, index, err)
			}
		}
		yamlRenameMappingKey(spec, "sourceCIDRs", "srcCIDRs")
		changes = append(changes, Migration{Resource: "FirewallRule/" + name, Field: "spec.sourceCIDRs", Reason: "srcCIDRs preserves every source CIDR"})
	}
	telemetryLogs := yamlTelemetryLogExports(resources)
	for index := 0; index < len(resources.Content); {
		resource := resources.Content[index]
		if yamlMappingScalar(resource, "kind") != "LogSink" || yamlMappingScalar(yamlMappingValue(resource, "spec"), "type") != "otlp" {
			index++
			continue
		}
		name := yamlMappingScalar(yamlMappingValue(resource, "metadata"), "name")
		telemetryRef := yamlMappingScalar(yamlMappingValue(yamlMappingValue(resource, "spec"), "otlp"), "telemetryRef")
		if !telemetryLogs[telemetryRef] {
			return nil, nil, fmt.Errorf("legacy config %s: LogSink/%s must reference a Telemetry resource that exports logs", source, name)
		}
		reference := "LogSink/" + name
		migratedLogs := yamlRemoveFirewallLogSinkReferences(resources, reference)
		if yamlCountScalar(document.Content[0], reference) != 0 {
			return nil, nil, fmt.Errorf("legacy config %s: LogSink/%s is referenced outside FirewallEventLog.sinks and cannot be migrated safely", source, name)
		}
		resources.Content = append(resources.Content[:index], resources.Content[index+1:]...)
		changes = append(changes, Migration{Resource: "LogSink/" + name, Field: "spec.type", Reason: telemetryRef + " exports logs for " + strings.Join(migratedLogs, ", ")})
	}

	output, err := yaml.Marshal(&document)
	if err != nil {
		return nil, nil, fmt.Errorf("encode migrated config %s: %w", source, err)
	}
	return output, changes, nil
}

func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func yamlMappingScalar(node *yaml.Node, key string) string {
	value := yamlMappingValue(node, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return value.Value
}

func yamlRemoveMappingKey(node *yaml.Node, key string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
	}
}

func yamlRenameMappingKey(node *yaml.Node, oldKey, newKey string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == oldKey {
			node.Content[i].Value = newKey
			return
		}
	}
}

func yamlTelemetryLogExports(resources *yaml.Node) map[string]bool {
	exports := map[string]bool{}
	for _, resource := range resources.Content {
		if yamlMappingScalar(resource, "kind") != "Telemetry" {
			continue
		}
		name := yamlMappingScalar(yamlMappingValue(resource, "metadata"), "name")
		signals := yamlMappingValue(yamlMappingValue(resource, "spec"), "signals")
		if signals == nil {
			exports["Telemetry/"+name] = true
			continue
		}
		for _, signal := range signals.Content {
			if signal.Kind == yaml.ScalarNode && signal.Value == "logs" {
				exports["Telemetry/"+name] = true
			}
		}
	}
	return exports
}

func yamlRemoveFirewallLogSinkReferences(resources *yaml.Node, reference string) []string {
	var names []string
	for _, resource := range resources.Content {
		if yamlMappingScalar(resource, "kind") != "FirewallEventLog" {
			continue
		}
		sinks := yamlMappingValue(yamlMappingValue(resource, "spec"), "sinks")
		if sinks == nil || sinks.Kind != yaml.SequenceNode {
			continue
		}
		out := sinks.Content[:0]
		removed := false
		for _, sink := range sinks.Content {
			if sink.Kind == yaml.ScalarNode && sink.Value == reference {
				removed = true
				continue
			}
			out = append(out, sink)
		}
		sinks.Content = out
		if removed {
			names = append(names, "FirewallEventLog/"+yamlMappingScalar(yamlMappingValue(resource, "metadata"), "name"))
		}
	}
	return names
}

func yamlCountScalar(node *yaml.Node, value string) int {
	if node == nil {
		return 0
	}
	count := 0
	if node.Kind == yaml.ScalarNode && node.Value == value {
		count++
	}
	for _, child := range node.Content {
		count += yamlCountScalar(child, value)
	}
	return count
}
