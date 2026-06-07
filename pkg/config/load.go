// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
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
	if err := yaml.Unmarshal(data, &router); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", source, err)
	}
	return &router, nil
}
