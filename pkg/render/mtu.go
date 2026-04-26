package render

import (
	"fmt"
	"sort"

	"routerd/pkg/api"
)

type pathMTUPolicy struct {
	Resource api.Resource
	Spec     api.PathMTUPolicySpec
	MTU      int
}

func pathMTUPolicies(router *api.Router) ([]pathMTUPolicy, error) {
	mtus, err := resourceMTUs(router)
	if err != nil {
		return nil, err
	}
	var policies []pathMTUPolicy
	for _, res := range router.Spec.Resources {
		if res.Kind != "PathMTUPolicy" {
			continue
		}
		spec, err := res.PathMTUPolicySpec()
		if err != nil {
			return nil, err
		}
		mtu := spec.MTU.Value
		switch defaultString(spec.MTU.Source, "minInterface") {
		case "minInterface":
			if len(spec.ToInterfaces) == 0 {
				return nil, fmt.Errorf("%s spec.toInterfaces is required when mtu.source is minInterface", res.ID())
			}
			mtu = 0
			for _, name := range spec.ToInterfaces {
				candidate := mtus[name]
				if candidate == 0 {
					return nil, fmt.Errorf("%s references interface with unknown MTU %q", res.ID(), name)
				}
				if mtu == 0 || candidate < mtu {
					mtu = candidate
				}
			}
		case "static":
			if mtu == 0 {
				return nil, fmt.Errorf("%s spec.mtu.value is required when mtu.source is static", res.ID())
			}
		default:
			return nil, fmt.Errorf("%s spec.mtu.source must be minInterface or static", res.ID())
		}
		if mtu < 1280 {
			return nil, fmt.Errorf("%s computed MTU %d is below the IPv6 minimum MTU 1280", res.ID(), mtu)
		}
		policies = append(policies, pathMTUPolicy{Resource: res, Spec: spec, MTU: mtu})
	}
	sort.Slice(policies, func(i, j int) bool {
		return policies[i].Resource.Metadata.Name < policies[j].Resource.Metadata.Name
	})
	return policies, nil
}

func resourceMTUs(router *api.Router) (map[string]int, error) {
	mtus := map[string]int{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface":
			mtus[res.Metadata.Name] = 1500
		case "PPPoEInterface":
			spec, err := res.PPPoEInterfaceSpec()
			if err != nil {
				return nil, err
			}
			mtus[res.Metadata.Name] = defaultInt(spec.MTU, 1492)
		case "DSLiteTunnel":
			spec, err := res.DSLiteTunnelSpec()
			if err != nil {
				return nil, err
			}
			mtus[res.Metadata.Name] = defaultInt(spec.MTU, 1454)
		}
	}
	return mtus, nil
}

func pathMTURAByScope(router *api.Router) (map[string]int, error) {
	policies, err := pathMTUPolicies(router)
	if err != nil {
		return nil, err
	}
	result := map[string]int{}
	for _, policy := range policies {
		if !policy.Spec.IPv6RA.Enabled {
			continue
		}
		scope := policy.Spec.IPv6RA.Scope
		if scope == "" {
			continue
		}
		if existing := result[scope]; existing == 0 || policy.MTU < existing {
			result[scope] = policy.MTU
		}
	}
	return result, nil
}

func pathMTUMSSPolicies(router *api.Router) ([]pathMTUPolicy, error) {
	policies, err := pathMTUPolicies(router)
	if err != nil {
		return nil, err
	}
	var result []pathMTUPolicy
	for _, policy := range policies {
		if policy.Spec.TCPMSSClamp.Enabled {
			result = append(result, policy)
		}
	}
	return result, nil
}

func pathMTUFamilyEnabled(families []string, family string) bool {
	if len(families) == 0 {
		return true
	}
	for _, candidate := range families {
		if candidate == family {
			return true
		}
	}
	return false
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}
