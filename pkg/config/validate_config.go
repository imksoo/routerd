// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func validateConfigResource(res api.Resource, _ platform.OS) (bool, error) {
	switch res.Kind {
	case "DynamicOverridePolicy":
		if res.APIVersion != api.ConfigAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.ConfigAPIVersion)
		}
		spec, err := res.DynamicOverridePolicySpec()
		if err != nil {
			return true, err
		}
		for i, rule := range spec.Allow {
			if strings.TrimSpace(rule.Source) == "" {
				return true, fmt.Errorf("%s spec.allow[%d].source is required", res.ID(), i)
			}
			for j, op := range rule.Operations {
				switch strings.TrimSpace(op) {
				case "mask":
				default:
					return true, fmt.Errorf("%s spec.allow[%d].operations[%d] must be mask", res.ID(), i, j)
				}
			}
			for j, target := range rule.Targets {
				if strings.TrimSpace(target.APIVersion) == "" {
					return true, fmt.Errorf("%s spec.allow[%d].targets[%d].apiVersion is required", res.ID(), i, j)
				}
				if strings.TrimSpace(target.Kind) == "" {
					return true, fmt.Errorf("%s spec.allow[%d].targets[%d].kind is required", res.ID(), i, j)
				}
				if strings.TrimSpace(target.Name) == "" {
					return true, fmt.Errorf("%s spec.allow[%d].targets[%d].name is required", res.ID(), i, j)
				}
			}
		}
		return true, nil
	default:
		return false, nil
	}
}
