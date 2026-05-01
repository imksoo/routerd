package render

import (
	"sort"

	"routerd/pkg/api"
)

func linkAliases(router *api.Router) map[string]string {
	aliases := map[string]string{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface":
			spec, err := res.InterfaceSpec()
			if err == nil {
				aliases[res.Metadata.Name] = spec.IfName
			}
		case "Bridge":
			spec, err := res.BridgeSpec()
			if err == nil {
				aliases[res.Metadata.Name] = defaultString(spec.IfName, res.Metadata.Name)
			}
		}
	}
	return aliases
}

type bridgeConfig struct {
	Name              string
	IfName            string
	Members           []string
	STP               bool
	RSTP              bool
	ForwardDelay      int
	HelloTime         int
	MACAddress        string
	MTU               int
	MulticastSnooping bool
}

func bridgeConfigs(router *api.Router, aliases map[string]string) ([]bridgeConfig, error) {
	var bridges []bridgeConfig
	for _, res := range router.Spec.Resources {
		if res.Kind != "Bridge" {
			continue
		}
		spec, err := res.BridgeSpec()
		if err != nil {
			return nil, err
		}
		var members []string
		for _, member := range spec.Members {
			if ifname := aliases[member]; ifname != "" {
				members = append(members, ifname)
			}
		}
		bridges = append(bridges, bridgeConfig{
			Name:              res.Metadata.Name,
			IfName:            defaultString(spec.IfName, res.Metadata.Name),
			Members:           members,
			STP:               api.BoolDefault(spec.STP, true),
			RSTP:              api.BoolDefault(spec.RSTP, true),
			ForwardDelay:      defaultInt(spec.ForwardDelay, 4),
			HelloTime:         defaultInt(spec.HelloTime, 2),
			MACAddress:        spec.MACAddress,
			MTU:               spec.MTU,
			MulticastSnooping: api.BoolDefault(spec.MulticastSnooping, false),
		})
	}
	sort.Slice(bridges, func(i, j int) bool { return bridges[i].IfName < bridges[j].IfName })
	return bridges, nil
}
