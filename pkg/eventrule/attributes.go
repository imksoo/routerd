// SPDX-License-Identifier: BSD-3-Clause

package eventrule

import (
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
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
	var out strings.Builder
	for {
		prefix, after, found := strings.Cut(value, "${")
		out.WriteString(prefix)
		if !found {
			break
		}
		key, rest, closed := strings.Cut(after, "}")
		if !closed {
			out.WriteString("${")
			out.WriteString(after)
			break
		}
		var replacement string
		known := true
		switch key {
		case "event.type":
			replacement = input.Type
		case "correlation":
			replacement = correlation
		case "count":
			replacement = strconv.Itoa(count)
		case "resource.name", "resource.kind", "resource.apiVersion":
			known = input.Resource != nil
			if known {
				switch key {
				case "resource.name":
					replacement = input.Resource.Name
				case "resource.kind":
					replacement = input.Resource.Kind
				default:
					replacement = input.Resource.APIVersion
				}
			}
		default:
			if attr, ok := strings.CutPrefix(key, "attributes."); ok {
				replacement, known = input.Attributes[attr]
			} else {
				known = false
			}
		}
		if known {
			// Replacements are input data; never parse their placeholders.
			out.WriteString(replacement)
		} else {
			out.WriteString("${")
			out.WriteString(key)
			out.WriteString("}")
		}
		value = rest
	}
	return out.String()
}
