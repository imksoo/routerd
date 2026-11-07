// SPDX-License-Identifier: BSD-3-Clause

package api

// ProviderActionUndo is the planned inverse of a provider operation. The
// executor receives it only through the same policy and durable claim as a
// forward action. Parameters never contain credentials.
type ProviderActionUndo struct {
	Action     string            `yaml:"action" json:"action"`
	Parameters map[string]string `yaml:"parameters,omitempty" json:"parameters,omitempty"`
}
