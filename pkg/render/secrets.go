// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"routerd/pkg/api"
)

func secretValue(plain string, source api.SecretValueSourceSpec) (string, error) {
	if strings.TrimSpace(plain) != "" {
		return plain, nil
	}
	if strings.TrimSpace(source.File) == "" && strings.TrimSpace(source.Env) == "" {
		return "", nil
	}
	var value string
	switch {
	case strings.TrimSpace(source.File) != "":
		data, err := os.ReadFile(strings.TrimSpace(source.File))
		if err != nil {
			return "", fmt.Errorf("read secret file %q: %w", strings.TrimSpace(source.File), err)
		}
		value = string(data)
	case strings.TrimSpace(source.Env) != "":
		env := strings.TrimSpace(source.Env)
		var ok bool
		value, ok = os.LookupEnv(env)
		if !ok {
			return "", fmt.Errorf("read secret env %q: not set", env)
		}
	}
	value = strings.TrimRight(value, "\r\n")
	if source.Base64 {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
		if err != nil {
			return "", fmt.Errorf("decode base64 secret: %w", err)
		}
		value = strings.TrimRight(string(decoded), "\r\n")
	}
	return value, nil
}
