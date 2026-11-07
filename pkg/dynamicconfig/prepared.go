// SPDX-License-Identifier: BSD-3-Clause

package dynamicconfig

import (
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

// PreparedStartup owns the immutable configuration accepted for one startup
// configuration. Effective views share its static resources read-only; callers
// that need mutable configuration must explicitly clone that configuration.
type PreparedStartup struct {
	router   api.Router
	policies []DynamicOverridePolicy
	targetOS platform.OS
}

// PrepareValidatedStartupForOS is the runtime-internal ownership boundary after
// the operator configuration has passed config.ValidateForOS. It does not admit
// external configuration: external callers must use BuildEffectiveConfigForOS,
// which retains the static validation required before runtime payloads are added.
func PrepareValidatedStartupForOS(startup api.Router, targetOS platform.OS) (*PreparedStartup, error) {
	owned := startup.Clone()
	policies, err := ExtractDynamicOverridePolicies(owned)
	if err != nil {
		return nil, err
	}
	return &PreparedStartup{router: owned, policies: policies, targetOS: targetOS}, nil
}

// Router exposes the prepared static input as a read-only resource graph.
func (p *PreparedStartup) Router() *api.Router { return &p.router }

// BuildEffective merges observations with this accepted startup. Expiry,
// conflicts and validation of the complete effective graph remain live
// decisions. Callers select when-gated resources only after that validation.
func (p *PreparedStartup) BuildEffective(parts []DynamicConfigPart, now time.Time) (api.Router, EffectiveResult, error) {
	return buildEffectiveConfigForOS(p.router, parts, p.policies, now, p.targetOS)
}
