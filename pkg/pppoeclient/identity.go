// SPDX-License-Identifier: BSD-3-Clause

package pppoeclient

import (
	"github.com/imksoo/routerd/pkg/api"
	"strings"
)

// SessionIdentity binds public negotiation settings without exposing or hashing
// a password. Resource/interface/device and supervisor ownership are checked
// separately by the status consumer.
type SessionIdentity struct {
	Username        string `json:"username"`
	AuthMethod      string `json:"authMethod"`
	ServiceName     string `json:"serviceName,omitempty"`
	ACName          string `json:"acName,omitempty"`
	MTU             int    `json:"mtu"`
	MRU             int    `json:"mru"`
	LCPEchoInterval int    `json:"lcpEchoInterval"`
	LCPEchoFailure  int    `json:"lcpEchoFailure"`
}

// IdentityForSpec applies the managed client's effective defaults.
func IdentityForSpec(spec api.PPPoESessionSpec) SessionIdentity {
	auth := strings.ToLower(strings.TrimSpace(spec.AuthMethod))
	if auth == "" {
		auth = "both"
	}
	return SessionIdentity{Username: spec.Username, AuthMethod: auth, ServiceName: spec.ServiceName, ACName: spec.ACName, MTU: intDefault(spec.MTU, 1454), MRU: intDefault(spec.MRU, 1454), LCPEchoInterval: intDefault(spec.LCPEchoInterval, 30), LCPEchoFailure: intDefault(spec.LCPEchoFailure, 4)}
}
