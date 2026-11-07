// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// Node.Decode does not inherit Decoder.KnownFields across UnmarshalYAML.
// Check only field names after normal decoding has checked values and aliases;
// do not re-encode nodes, which would change scalar spellings and shared anchors.
func checkYAMLFields(node *yaml.Node, destination any) error {
	type visit struct {
		node   *yaml.Node
		typeOf reflect.Type
	}
	seen := make(map[visit]bool)
	var check func(*yaml.Node, reflect.Type) error
	check = func(node *yaml.Node, typ reflect.Type) error {
		if node == nil || typ == nil {
			return nil
		}
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if typ == reflect.TypeFor[yaml.Node]() {
			return nil
		}
		key := visit{node, typ}
		if seen[key] {
			return nil
		}
		seen[key] = true
		if node.Kind == yaml.AliasNode {
			return check(node.Alias, typ)
		}
		switch typ.Kind() {
		case reflect.Struct:
			if node.Kind != yaml.MappingNode {
				return nil // The normal decoder owns value/type errors.
			}
			fields := yamlStructFields(typ)
			for i := 0; i+1 < len(node.Content); i += 2 {
				name, value := node.Content[i], node.Content[i+1]
				if name.Tag == "!!merge" {
					if value.Kind == yaml.SequenceNode {
						for _, merged := range value.Content {
							if err := check(merged, typ); err != nil {
								return err
							}
						}
					} else if err := check(value, typ); err != nil {
						return err
					}
					continue
				}
				field, ok := fields[name.Value]
				if !ok {
					return fmt.Errorf("line %d: field %s not found in type %s", name.Line, name.Value, typ)
				}
				if err := check(value, field); err != nil {
					return err
				}
			}
		case reflect.Slice, reflect.Array:
			for _, value := range node.Content {
				if err := check(value, typ.Elem()); err != nil {
					return err
				}
			}
		case reflect.Map:
			for i := 1; i < len(node.Content); i += 2 {
				if err := check(node.Content[i], typ.Elem()); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return check(node, reflect.TypeOf(destination))
}

func yamlStructFields(typ reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type)
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		tag := strings.Split(field.Tag.Get("yaml"), ",")
		if tag[0] == "-" {
			continue
		}
		if len(tag) > 1 && tag[1] == "inline" {
			for name, fieldType := range yamlStructFields(field.Type) {
				fields[name] = fieldType
			}
			continue
		}
		name := tag[0]
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		fields[name] = field.Type
	}
	return fields
}
