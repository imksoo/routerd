package resourcequery

import (
	"encoding/json"
	"fmt"
	"strings"

	"routerd/pkg/api"
)

type Store interface {
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type ResourceRef struct {
	Kind string
	Name string
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
	value := store.ObjectStatus(api.NetAPIVersion, kind, name)[field]
	return normalizeValues(value)
}

func DependencyReady(store Store, dependency api.ResourceDependencySpec) bool {
	if store == nil || strings.TrimSpace(dependency.Resource) == "" {
		return dependency.Optional
	}
	kind, name, ok := SplitResource(dependency.Resource)
	if !ok {
		return dependency.Optional
	}
	field := defaultString(dependency.Field, "phase")
	if dependency.Phase != "" {
		field = "phase"
	}
	values := normalizeValues(store.ObjectStatus(api.NetAPIVersion, kind, name)[field])
	if len(values) == 0 {
		return dependency.Optional
	}
	if dependency.NotEmpty && strings.TrimSpace(values[0]) == "" {
		return false
	}
	expected := firstNonEmpty(dependency.Phase, dependency.Equals)
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

func SourceReady(store Store, source string) bool {
	kind, name, ok := SplitResource(source)
	if !ok {
		return false
	}
	switch fmt.Sprint(store.ObjectStatus(api.NetAPIVersion, kind, name)["phase"]) {
	case "Applied", "Bound", "Healthy", "Installed", "Ready", "Running", "Up":
		return true
	default:
		return false
	}
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
