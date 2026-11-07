// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/imksoo/routerd/pkg/api"
)

func Load(path string) (*api.Router, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	return LoadBytes(data, path)
}

func LoadBytes(data []byte, source string) (*api.Router, error) {
	var router api.Router
	if err := decodeSingleYAMLDocument(data, &router); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", source, err)
	}
	return &router, nil
}

// All config/hash/mutation paths consume exactly one document. Silently
// discarding a second document would accept only part of operator intent.
func decodeSingleYAMLDocument(data []byte, destination any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("multiple YAML documents are unsupported")
	}
	return nil
}
