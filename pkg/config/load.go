package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"routerd/pkg/api"
)

func Load(path string) (*api.Router, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var router api.Router
	if err := yaml.Unmarshal(data, &router); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &router, nil
}
