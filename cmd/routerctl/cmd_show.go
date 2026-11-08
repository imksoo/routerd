// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"

	"github.com/imksoo/routerd/pkg/api"
)

type showOptions struct {
	Target           string
	Output           string
	ConfigPath       string
	StatePath        string
	LedgerPath       string
	Diff             bool
	LedgerOnly       bool
	AdoptOnly        bool
	Events           bool
	SpecOnly         bool
	StatusOnly       bool
	Verbose          bool
	IncludeStale     bool
	ConnectionsLimit int
}

func resourceIDSet(router *api.Router) map[string]bool {
	out := map[string]bool{}
	if router == nil {
		return out
	}
	for _, res := range router.Spec.Resources {
		out[res.ID()] = true
	}
	return out
}

func flattenAny(value any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	flattenValue("", decoded, out)
	return out
}

func flattenValue(prefix string, value any, out map[string]any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			flattenValue(next, child, out)
		}
	case []any:
		out[prefix] = typed
	default:
		if prefix != "" {
			out[prefix] = typed
		}
	}
}
