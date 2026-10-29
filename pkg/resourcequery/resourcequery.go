// SPDX-License-Identifier: BSD-3-Clause

package resourcequery

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
)

type Store interface {
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type StatusReader interface {
	ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error)
}

type ResourceRef struct {
	Kind string
	Name string
}

// ReadValue preserves storage failures so effect owners can retain their last
// committed result instead of mistaking an unavailable observation for absence.
func ReadValue(store StatusReader, source api.StatusValueSourceSpec) (string, error) {
	values, err := ReadValues(store, source)
	if err != nil || len(values) == 0 {
		return "", err
	}
	return values[0], nil
}

func ReadValues(store StatusReader, source api.StatusValueSourceSpec) ([]string, error) {
	if strings.TrimSpace(source.Resource) == "" {
		return nil, nil
	}
	if store == nil {
		return nil, fmt.Errorf("read %s status: store is unavailable", source.Resource)
	}
	kind, name, ok := SplitResource(source.Resource)
	if !ok {
		return nil, fmt.Errorf("invalid resource source %q", source.Resource)
	}
	status, err := store.ReadObjectStatus(APIVersionForKind(kind), kind, name)
	if err != nil {
		return nil, fmt.Errorf("read %s status: %w", source.Resource, err)
	}
	return normalizeValues(status[defaultString(source.Field, "phase")]), nil
}

func Value(store Store, source api.StatusValueSourceSpec) string {
	values := Values(store, source)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func Values(store Store, source api.StatusValueSourceSpec) []string {
	if store == nil || strings.TrimSpace(source.Resource) == "" {
		return nil
	}
	kind, name, ok := SplitResource(source.Resource)
	if !ok {
		return nil
	}
	field := defaultString(source.Field, "phase")
	value := store.ObjectStatus(APIVersionForKind(kind), kind, name)[field]
	return normalizeValues(value)
}

func ValuesFromStoreOrRouter(store Store, router *api.Router, source api.StatusValueSourceSpec) []string {
	values := ValuesFromRouter(router, source)
	if len(values) > 0 {
		return values
	}
	return Values(store, source)
}

func ReadValuesFromStoreOrRouter(store StatusReader, router *api.Router, source api.StatusValueSourceSpec) ([]string, error) {
	if values := ValuesFromRouter(router, source); len(values) > 0 {
		return values, nil
	}
	return ReadValues(store, source)
}

func ValuesFromRouter(router *api.Router, source api.StatusValueSourceSpec) []string {
	if router == nil || strings.TrimSpace(source.Resource) == "" {
		return nil
	}
	kind, name, ok := SplitResource(source.Resource)
	if !ok {
		return nil
	}
	field := strings.TrimSpace(source.Field)
	for _, resource := range router.Spec.Resources {
		if resource.Kind != kind || resource.Metadata.Name != name {
			continue
		}
		switch kind {
		case "DNSZone":
			if field != "zone" {
				return nil
			}
			spec, err := resource.DNSZoneSpec()
			if err != nil {
				return nil
			}
			return compact([]string{spec.Zone})
		case "IPv4StaticAddress":
			if field != "address" {
				return nil
			}
			spec, err := resource.IPv4StaticAddressSpec()
			if err != nil {
				return nil
			}
			return compact([]string{spec.Address})
		default:
			return nil
		}
	}
	return nil
}

func DependencyReady(store Store, dependency api.ResourceDependencySpec) bool {
	if store == nil || strings.TrimSpace(dependency.Resource) == "" {
		return dependency.Optional
	}
	kind, name, ok := SplitResource(dependency.Resource)
	if !ok {
		return dependency.Optional
	}
	status := store.ObjectStatus(APIVersionForKind(kind), kind, name)
	return dependencyReady(status, dependency)
}

func ReadDependencyReady(store StatusReader, dependency api.ResourceDependencySpec) (bool, error) {
	if strings.TrimSpace(dependency.Resource) == "" {
		return dependency.Optional, nil
	}
	kind, name, ok := SplitResource(dependency.Resource)
	if !ok {
		return false, fmt.Errorf("invalid dependency resource %q", dependency.Resource)
	}
	if store == nil {
		return false, fmt.Errorf("read dependency %s: store is unavailable", dependency.Resource)
	}
	status, err := store.ReadObjectStatus(APIVersionForKind(kind), kind, name)
	if err != nil {
		return false, fmt.Errorf("read dependency %s: %w", dependency.Resource, err)
	}
	return dependencyReady(status, dependency), nil
}

func dependencyReady(status map[string]any, dependency api.ResourceDependencySpec) bool {
	field := defaultString(dependency.Field, "phase")
	if dependency.Phase != "" {
		field = "phase"
	}
	values := normalizeValues(statusValue(status, field))
	if len(values) == 0 {
		return dependency.Optional
	}
	if dependency.NotEmpty && strings.TrimSpace(values[0]) == "" {
		return false
	}
	expected := stringutil.FirstNonEmpty(dependency.Phase, dependency.Equals)
	if expected != "" {
		for _, value := range values {
			if value == expected {
				return true
			}
		}
		return false
	}
	return strings.TrimSpace(values[0]) != ""
}

func DependenciesReady(store Store, dependencies []api.ResourceDependencySpec) bool {
	for _, dependency := range dependencies {
		if !DependencyReady(store, dependency) {
			return false
		}
	}
	return true
}

func ReadDependenciesReady(store StatusReader, dependencies []api.ResourceDependencySpec) (bool, error) {
	for _, dependency := range dependencies {
		ready, err := ReadDependencyReady(store, dependency)
		if err != nil || !ready {
			return false, err
		}
	}
	return true, nil
}

func SourceReady(store Store, source string) bool {
	kind, name, ok := SplitResource(source)
	if !ok {
		return false
	}
	return sourceReady(store.ObjectStatus(APIVersionForKind(kind), kind, name))
}

func ReadSourceReady(store StatusReader, source string) (bool, error) {
	kind, name, ok := SplitResource(source)
	if !ok {
		return false, fmt.Errorf("invalid resource source %q", source)
	}
	if store == nil {
		return false, fmt.Errorf("read source %s: store is unavailable", source)
	}
	status, err := store.ReadObjectStatus(APIVersionForKind(kind), kind, name)
	if err != nil {
		return false, fmt.Errorf("read source %s: %w", source, err)
	}
	return sourceReady(status), nil
}

func sourceReady(status map[string]any) bool {
	switch fmt.Sprint(statusValue(status, "phase")) {
	case "Applied", "Bound", "Healthy", "Installed", "Ready", "Running", "Up":
		return true
	default:
		return false
	}
}

func statusValue(status map[string]any, field string) any {
	if strings.TrimSpace(field) != "phase" {
		return status[field]
	}
	if phase := status[field]; phase != nil && strings.TrimSpace(fmt.Sprint(phase)) != "" {
		return phase
	}
	if observed, ok := status["observed"].(map[string]any); ok {
		return observed["phase"]
	}
	return nil
}

func APIVersionForKind(kind string) string {
	if version, known := api.ResourceAPIVersion(kind); known {
		return version
	}
	return api.NetAPIVersion
}

func SplitResource(ref string) (string, string, bool) {
	parts := strings.Split(strings.TrimSpace(ref), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func SourceRef(source api.StatusValueSourceSpec) (ResourceRef, bool) {
	kind, name, ok := SplitResource(source.Resource)
	return ResourceRef{Kind: kind, Name: name}, ok
}

func DependencyRef(dependency api.ResourceDependencySpec) (ResourceRef, bool) {
	kind, name, ok := SplitResource(dependency.Resource)
	return ResourceRef{Kind: kind, Name: name}, ok
}

func normalizeValues(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return splitMaybeList(typed)
	case []string:
		return compact(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		return compact(out)
	default:
		return splitMaybeList(fmt.Sprint(value))
	}
}

func splitMaybeList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var decoded []string
	if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
		return compact(decoded)
	}
	return compact(strings.Split(raw, ","))
}

func compact(values []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
