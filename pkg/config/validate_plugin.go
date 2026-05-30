// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func validatePluginResource(res api.Resource, _ platform.OS) (bool, error) {
	switch res.Kind {
	case "Plugin":
		if res.APIVersion != api.PluginAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.PluginAPIVersion)
		}
		spec, err := res.PluginSpec()
		if err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.Executable) == "" {
			return true, fmt.Errorf("%s spec.executable is required", res.ID())
		}
		if !filepath.IsAbs(strings.TrimSpace(spec.Executable)) {
			return true, fmt.Errorf("%s spec.executable must be an absolute path", res.ID())
		}
		if err := validateOptionalPositiveDuration(res.ID(), "spec.timeout", spec.Timeout); err != nil {
			return true, err
		}
		for i, capability := range spec.Capabilities {
			switch strings.TrimSpace(capability) {
			case "observe.cloud", "propose.dynamicConfig", "propose.providerAction":
			default:
				return true, fmt.Errorf("%s spec.capabilities[%d] must be observe.cloud, propose.dynamicConfig, or propose.providerAction", res.ID(), i)
			}
		}
		if err := validatePluginContext(res.ID(), "spec.context", spec.Context); err != nil {
			return true, err
		}
		if err := validatePluginTriggers(res.ID(), "spec.triggers", spec.Triggers); err != nil {
			return true, err
		}
		return true, nil
	case "DynamicConfigSource":
		if res.APIVersion != api.PluginAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.PluginAPIVersion)
		}
		spec, err := res.DynamicConfigSourceSpec()
		if err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.PluginRef) == "" {
			return true, fmt.Errorf("%s spec.pluginRef is required", res.ID())
		}
		if strings.TrimSpace(spec.TTL) == "" {
			return true, fmt.Errorf("%s spec.ttl is required", res.ID())
		}
		if err := validateOptionalPositiveDuration(res.ID(), "spec.ttl", spec.TTL); err != nil {
			return true, err
		}
		if spec.MergePolicy != nil {
			switch strings.TrimSpace(spec.MergePolicy.Conflict) {
			case "", "reject":
			default:
				return true, fmt.Errorf("%s spec.mergePolicy.conflict must be reject", res.ID())
			}
		}
		if err := validatePluginTriggers(res.ID(), "spec.triggers", spec.Triggers); err != nil {
			return true, err
		}
		return true, nil
	default:
		return false, nil
	}
}

// validatePluginContext checks the least-privilege context allowlist. Each ref
// must fully identify a resource (apiVersion/kind/name). The referenced resource
// is NOT required to exist at validate-time: the runner resolves it at run-time
// and a missing ref simply yields no context entry.
func validatePluginContext(resourceID, path string, ctx api.PluginContextSpec) error {
	for i, ref := range ctx.Resources {
		if strings.TrimSpace(ref.APIVersion) == "" {
			return fmt.Errorf("%s %s.resources[%d].apiVersion is required", resourceID, path, i)
		}
		if strings.TrimSpace(ref.Kind) == "" {
			return fmt.Errorf("%s %s.resources[%d].kind is required", resourceID, path, i)
		}
		if strings.TrimSpace(ref.Name) == "" {
			return fmt.Errorf("%s %s.resources[%d].name is required", resourceID, path, i)
		}
	}
	return nil
}

func validatePluginTriggers(resourceID, path string, triggers []api.PluginTrigger) error {
	for i, trigger := range triggers {
		switch strings.TrimSpace(trigger.Type) {
		case "interval":
			if strings.TrimSpace(trigger.Every) == "" {
				return fmt.Errorf("%s %s[%d].every is required for interval triggers", resourceID, path, i)
			}
			if err := validateOptionalPositiveDuration(resourceID, fmt.Sprintf("%s[%d].every", path, i), trigger.Every); err != nil {
				return err
			}
		case "event":
		default:
			return fmt.Errorf("%s %s[%d].type must be interval or event", resourceID, path, i)
		}
	}
	return nil
}

func validateOptionalPositiveDuration(resourceID, path, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("%s %s must be a valid duration: %w", resourceID, path, err)
	}
	if duration <= 0 {
		return fmt.Errorf("%s %s must be greater than 0", resourceID, path)
	}
	return nil
}
