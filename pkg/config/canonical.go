// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"

	"github.com/imksoo/routerd/pkg/api"
)

type MutationTarget struct {
	APIVersion string
	Kind       string
	Name       string
}

func CanonicalYAML(data []byte) ([]byte, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&node); err != nil {
		_ = encoder.Close()
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func CanonicalYAMLFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	out, err := CanonicalYAML(data)
	if err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return out, nil
}

func CanonicalRouterYAML(data []byte, source string) ([]byte, *api.Router, error) {
	out, err := CanonicalYAML(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parse config %s: %w", source, err)
	}
	router, err := LoadBytes(out, source)
	if err != nil {
		return nil, nil, err
	}
	if err := Validate(router); err != nil {
		return nil, nil, fmt.Errorf("validate config %s: %w", source, err)
	}
	return out, router, nil
}

func UpsertCandidateYAML(currentYAML, candidateYAML []byte, replace bool) ([]byte, *api.Router, error) {
	if replace {
		return CanonicalRouterYAML(candidateYAML, "candidate")
	}
	currentDoc, currentRoot, err := parseYAMLDocument(currentYAML, "canonical")
	if err != nil {
		return nil, nil, err
	}
	_, candidateRoot, err := parseYAMLDocument(candidateYAML, "candidate")
	if err != nil {
		return nil, nil, err
	}
	candidateResources, ok, err := resourcesNode(candidateRoot)
	if err != nil {
		return nil, nil, err
	}
	if !ok || len(candidateResources.Content) == 0 {
		return nil, nil, errors.New("candidate spec.resources must contain at least one resource for partial apply")
	}
	currentResources, err := ensureResourcesNode(currentRoot)
	if err != nil {
		return nil, nil, err
	}
	index := resourceNodeIndex(currentResources)
	for _, resourceNode := range candidateResources.Content {
		key, err := resourceNodeKey(resourceNode)
		if err != nil {
			return nil, nil, err
		}
		if existing, ok := index[key.String()]; ok {
			currentResources.Content[existing] = resourceNode
			continue
		}
		currentResources.Content = append(currentResources.Content, resourceNode)
		index[key.String()] = len(currentResources.Content) - 1
	}
	return canonicalRouterFromNode(currentDoc, "mutated canonical")
}

func DeleteResourceYAML(currentYAML []byte, target MutationTarget) ([]byte, *api.Router, bool, error) {
	currentDoc, currentRoot, err := parseYAMLDocument(currentYAML, "canonical")
	if err != nil {
		return nil, nil, false, err
	}
	resources, ok, err := resourcesNode(currentRoot)
	if err != nil {
		return nil, nil, false, err
	}
	if !ok {
		out, router, err := canonicalRouterFromNode(currentDoc, "mutated canonical")
		return out, router, false, err
	}
	targetKey := resourceKey(target)
	outResources := resources.Content[:0]
	removed := false
	for _, resourceNode := range resources.Content {
		key, err := resourceNodeKey(resourceNode)
		if err != nil {
			return nil, nil, false, err
		}
		if key == targetKey {
			removed = true
			continue
		}
		outResources = append(outResources, resourceNode)
	}
	resources.Content = outResources
	out, router, err := canonicalRouterFromNode(currentDoc, "mutated canonical")
	return out, router, removed, err
}

func AtomicWriteFile(path string, data []byte) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("atomic write path is empty")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	mode := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	cleanup = false
	if err := syncDir(dir); err != nil {
		return err
	}
	return nil
}

type resourceNodeID struct {
	APIVersion string
	Kind       string
	Name       string
}

func (id resourceNodeID) String() string {
	return id.APIVersion + "/" + id.Kind + "/" + id.Name
}

func resourceKey(target MutationTarget) resourceNodeID {
	return resourceNodeID{APIVersion: strings.TrimSpace(target.APIVersion), Kind: strings.TrimSpace(target.Kind), Name: strings.TrimSpace(target.Name)}
}

func parseYAMLDocument(data []byte, source string) (*yaml.Node, *yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse config %s: %w", source, err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("parse config %s: expected a YAML mapping document", source)
	}
	return &doc, doc.Content[0], nil
}

func canonicalRouterFromNode(doc *yaml.Node, source string) ([]byte, *api.Router, error) {
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		_ = encoder.Close()
		return nil, nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, nil, err
	}
	router, err := LoadBytes(out.Bytes(), source)
	if err != nil {
		return nil, nil, err
	}
	if err := Validate(router); err != nil {
		return nil, nil, fmt.Errorf("validate config %s: %w", source, err)
	}
	return out.Bytes(), router, nil
}

func resourcesNode(root *yaml.Node) (*yaml.Node, bool, error) {
	spec, ok := mappingValue(root, "spec")
	if !ok {
		return nil, false, nil
	}
	if spec.Kind != yaml.MappingNode {
		return nil, false, errors.New("spec must be a mapping")
	}
	resources, ok := mappingValue(spec, "resources")
	if !ok {
		return nil, false, nil
	}
	if resources.Kind != yaml.SequenceNode {
		return nil, false, errors.New("spec.resources must be a sequence")
	}
	return resources, true, nil
}

func ensureResourcesNode(root *yaml.Node) (*yaml.Node, error) {
	spec, ok := mappingValue(root, "spec")
	if !ok {
		spec = &yaml.Node{Kind: yaml.MappingNode}
		root.Content = append(root.Content, scalarNode("spec"), spec)
	}
	if spec.Kind != yaml.MappingNode {
		return nil, errors.New("spec must be a mapping")
	}
	resources, ok := mappingValue(spec, "resources")
	if !ok {
		resources = &yaml.Node{Kind: yaml.SequenceNode}
		spec.Content = append(spec.Content, scalarNode("resources"), resources)
	}
	if resources.Kind != yaml.SequenceNode {
		return nil, errors.New("spec.resources must be a sequence")
	}
	return resources, nil
}

func mappingValue(node *yaml.Node, key string) (*yaml.Node, bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1], true
		}
	}
	return nil, false
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func resourceNodeIndex(resources *yaml.Node) map[string]int {
	out := map[string]int{}
	for i, node := range resources.Content {
		key, err := resourceNodeKey(node)
		if err == nil {
			out[key.String()] = i
		}
	}
	return out
}

func resourceNodeKey(node *yaml.Node) (resourceNodeID, error) {
	var resource struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Metadata   struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
	}
	if err := node.Decode(&resource); err != nil {
		return resourceNodeID{}, err
	}
	id := resourceNodeID{
		APIVersion: strings.TrimSpace(resource.APIVersion),
		Kind:       strings.TrimSpace(resource.Kind),
		Name:       strings.TrimSpace(resource.Metadata.Name),
	}
	if id.APIVersion == "" || id.Kind == "" || id.Name == "" {
		return resourceNodeID{}, errors.New("resource requires apiVersion, kind, and metadata.name")
	}
	return id, nil
}

func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := f.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return err
	}
	return nil
}
