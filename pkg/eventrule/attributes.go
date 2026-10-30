// SPDX-License-Identifier: BSD-3-Clause

package eventrule

import (
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"strconv"
	"strings"
)

func correlationKey(event daemonapi.DaemonEvent, pattern api.EventRulePatternSpec) (string, bool) {
	path := strings.TrimSpace(pattern.CorrelateBy)
	if path == "" {
		return "default", true
	}
	switch {
	case strings.HasPrefix(path, "attributes."):
		key := strings.TrimPrefix(path, "attributes.")
		value := event.Attributes[key]
		if value == "" && !pattern.AllowMissingCorrelation {
			return "", false
		}
		return value, true
	case path == "resource.name":
		if event.Resource == nil {
			return "", pattern.AllowMissingCorrelation
		}
		return event.Resource.Name, true
	case path == "resource.kind":
		if event.Resource == nil {
			return "", pattern.AllowMissingCorrelation
		}
		return event.Resource.Kind, true
	case path == "resource.apiVersion":
		if event.Resource == nil {
			return "", pattern.AllowMissingCorrelation
		}
		return event.Resource.APIVersion, true
	case path == "daemon.instance":
		if event.Daemon.Instance == "" && !pattern.AllowMissingCorrelation {
			return "", false
		}
		return event.Daemon.Instance, true
	case path == "daemon.kind":
		if event.Daemon.Kind == "" && !pattern.AllowMissingCorrelation {
			return "", false
		}
		return event.Daemon.Kind, true
	default:
		return "", false
	}
}

func renderAttributes(template map[string]string, input daemonapi.DaemonEvent, correlation string, count int) map[string]string {
	out := map[string]string{}
	for key, value := range template {
		out[key] = renderValue(value, input, correlation, count)
	}
	if count > 0 {
		out["count"] = strconv.Itoa(count)
	}
	return out
}

func renderValue(value string, input daemonapi.DaemonEvent, correlation string, count int) string {
	replacements := map[string]string{
		"${event.type}":  input.Type,
		"${correlation}": correlation,
		"${count}":       strconv.Itoa(count),
	}
	if input.Resource != nil {
		replacements["${resource.name}"] = input.Resource.Name
		replacements["${resource.kind}"] = input.Resource.Kind
		replacements["${resource.apiVersion}"] = input.Resource.APIVersion
	}
	for key, attr := range input.Attributes {
		replacements["${attributes."+key+"}"] = attr
	}
	for old, replacement := range replacements {
		value = strings.ReplaceAll(value, old, replacement)
	}
	return value
}
