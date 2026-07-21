// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func validateRouteResource(res api.Resource, targetOS platform.OS) (bool, error) {
	switch res.Kind {
	case "BGPRouter":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.BGPRouterSpec()
		if err != nil {
			return true, err
		}
		if spec.ASN == 0 {
			return true, fmt.Errorf("%s spec.asn is required", res.ID())
		}
		addr, err := netip.ParseAddr(spec.RouterID)
		if err != nil || !addr.Is4() {
			return true, fmt.Errorf("%s spec.routerID must be an IPv4 address", res.ID())
		}
		if spec.Listen.Port != 0 && (spec.Listen.Port < 1 || spec.Listen.Port > 65535) {
			return true, fmt.Errorf("%s spec.listen.port must be within 1-65535", res.ID())
		}
		if strings.TrimSpace(spec.Listen.Address) != "" {
			if _, err := netip.ParseAddr(strings.TrimSpace(spec.Listen.Address)); err != nil {
				return true, fmt.Errorf("%s spec.listen.address must be an IP address", res.ID())
			}
		}
		if err := validateBGPTimerProfile(res.ID(), "spec.timers", spec.Timers); err != nil {
			return true, err
		}
		switch strings.TrimSpace(spec.ConvergenceProfile) {
		case "", "default", "fast", "stable":
		default:
			return true, fmt.Errorf("%s spec.convergenceProfile must be default, fast, or stable", res.ID())
		}
		if err := validateBGPGracefulRestart(res.ID(), spec.GracefulRestart); err != nil {
			return true, err
		}
		if err := validateBGPWatcher(res.ID(), spec.Watcher); err != nil {
			return true, err
		}
		switch defaultString(spec.Backend, "gobgp") {
		case "gobgp":
		default:
			return true, fmt.Errorf("%s spec.backend must be gobgp", res.ID())
		}
		if err := validateBGPRouterPolicy(res.ID(), spec); err != nil {
			return true, err
		}
	case "BGPPeer":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.BGPPeerSpec()
		if err != nil {
			return true, err
		}
		kind, name, ok := strings.Cut(strings.TrimSpace(spec.RouterRef), "/")
		if !ok || kind != "BGPRouter" || strings.TrimSpace(name) == "" {
			return true, fmt.Errorf("%s spec.routerRef must reference BGPRouter/<name>", res.ID())
		}
		if spec.PeerASN == 0 {
			return true, fmt.Errorf("%s spec.peerASN is required", res.ID())
		}
		if len(spec.Peers) == 0 && len(spec.PeersFrom) == 0 {
			return true, fmt.Errorf("%s spec.peers or spec.peersFrom is required", res.ID())
		}
		if spec.EbgpMultihop < 0 || spec.EbgpMultihop > 255 {
			return true, fmt.Errorf("%s spec.ebgpMultihop must be within 0-255", res.ID())
		}
		if clusterID := strings.TrimSpace(spec.RouteReflectorClusterID); clusterID != "" {
			addr, err := netip.ParseAddr(clusterID)
			if err != nil || !addr.Is4() {
				return true, fmt.Errorf("%s spec.routeReflectorClusterID must be an IPv4 router ID", res.ID())
			}
		}
		seenPeers := map[string]bool{}
		for i, peer := range spec.Peers {
			peer = strings.TrimSpace(peer)
			if peer == "" || strings.ContainsAny(peer, " \t\n\r") {
				return true, fmt.Errorf("%s spec.peers[%d] must be a single peer address or hostname", res.ID(), i)
			}
			if seenPeers[peer] {
				return true, fmt.Errorf("%s spec.peers[%d] duplicates %q", res.ID(), i, peer)
			}
			seenPeers[peer] = true
		}
		for i, source := range spec.PeersFrom {
			if err := validateBGPPeersFrom(res.ID(), i, source); err != nil {
				return true, err
			}
		}
		if err := validateBGPTimerProfile(res.ID(), "spec.timers", spec.Timers); err != nil {
			return true, err
		}
		if err := validateBGPCommunities(res.ID(), "spec.communities", spec.Communities); err != nil {
			return true, err
		}
		if _, err := validateBGPPrefixList(res.ID(), "spec.importPolicy.allowedPrefixes", spec.ImportPolicy.AllowedPrefixes); err != nil {
			return true, err
		}
		if err := validateBGPImportPrefixLengths(res.ID(), "spec.importPolicy", spec.ImportPolicy); err != nil {
			return true, err
		}
		switch strings.TrimSpace(spec.ImportPolicy.NextHopRewrite) {
		case "", "peer-address", "unchanged":
		default:
			return true, fmt.Errorf("%s spec.importPolicy.nextHopRewrite must be peer-address or unchanged", res.ID())
		}
		if _, err := validateBGPPrefixList(res.ID(), "spec.exportPolicy.allowedPrefixes", spec.ExportPolicy.AllowedPrefixes); err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.BFD) != "" && !strings.HasPrefix(strings.TrimSpace(spec.BFD), "BFD/") {
			return true, fmt.Errorf("%s spec.bfd must reference BFD/<name>", res.ID())
		}
		if err := validateSecretValueSource(res.ID(), "spec.password", spec.Password, "spec.passwordFrom", spec.PasswordFrom); err != nil {
			return true, err
		}
	case "BGPDynamicPeer":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.BGPDynamicPeerSpec()
		if err != nil {
			return true, err
		}
		kind, name, ok := strings.Cut(strings.TrimSpace(spec.RouterRef), "/")
		if !ok || kind != "BGPRouter" || strings.TrimSpace(name) == "" {
			return true, fmt.Errorf("%s spec.routerRef must reference BGPRouter/<name>", res.ID())
		}
		if spec.PeerASN == 0 {
			return true, fmt.Errorf("%s spec.peerASN is required", res.ID())
		}
		if len(spec.Listen.SourcePrefixes) == 0 {
			return true, fmt.Errorf("%s spec.listen.sourcePrefixes is required", res.ID())
		}
		if _, err := validateBGPPrefixList(res.ID(), "spec.listen.sourcePrefixes", spec.Listen.SourcePrefixes); err != nil {
			return true, err
		}
		if spec.EbgpMultihop < 0 || spec.EbgpMultihop > 255 {
			return true, fmt.Errorf("%s spec.ebgpMultihop must be within 0-255", res.ID())
		}
		if clusterID := strings.TrimSpace(spec.RouteReflectorClusterID); clusterID != "" {
			addr, err := netip.ParseAddr(clusterID)
			if err != nil || !addr.Is4() {
				return true, fmt.Errorf("%s spec.routeReflectorClusterID must be an IPv4 router ID", res.ID())
			}
		}
		if err := validateBGPTimerProfile(res.ID(), "spec.timers", spec.Timers); err != nil {
			return true, err
		}
		if _, err := validateBGPPrefixList(res.ID(), "spec.importPolicy.allowedPrefixes", spec.ImportPolicy.AllowedPrefixes); err != nil {
			return true, err
		}
		if err := validateBGPImportPrefixLengths(res.ID(), "spec.importPolicy", spec.ImportPolicy); err != nil {
			return true, err
		}
		switch strings.TrimSpace(spec.ImportPolicy.NextHopRewrite) {
		case "", "peer-address", "unchanged":
		default:
			return true, fmt.Errorf("%s spec.importPolicy.nextHopRewrite must be peer-address or unchanged", res.ID())
		}
		if _, err := validateBGPPrefixList(res.ID(), "spec.exportPolicy.allowedPrefixes", spec.ExportPolicy.AllowedPrefixes); err != nil {
			return true, err
		}
		if err := validateSecretValueSource(res.ID(), "spec.password", spec.Password, "spec.passwordFrom", spec.PasswordFrom); err != nil {
			return true, err
		}
	case "BFD":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.BFDSpec()
		if err != nil {
			return true, err
		}
		if err := validateBFD(res.ID(), spec); err != nil {
			return true, err
		}
	case "ClusterNetworkRoute":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.ClusterNetworkRouteSpec()
		if err != nil {
			return true, err
		}
		if err := validateClusterNetworkRoute(res.ID(), spec); err != nil {
			return true, err
		}
	case "EgressRoutePolicy":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.EgressRoutePolicySpec()
		if err != nil {
			return true, err
		}
		switch defaultString(spec.Family, "ipv4") {
		case "ipv4", "ipv6":
		default:
			return true, fmt.Errorf("%s spec.family must be ipv4 or ipv6", res.ID())
		}
		switch defaultString(spec.Selection, "highest-weight-ready") {
		case "highest-weight-ready", "weighted-ecmp":
		default:
			return true, fmt.Errorf("%s spec.selection must be highest-weight-ready or weighted-ecmp", res.ID())
		}
		switch spec.Mode {
		case "", "priority", "mark", "hash":
		default:
			return true, fmt.Errorf("%s spec.mode must be priority, mark, or hash", res.ID())
		}
		for _, cidr := range spec.SourceCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || (defaultString(spec.Family, "ipv4") == "ipv4" && !prefix.Addr().Is4()) || (defaultString(spec.Family, "ipv4") == "ipv6" && !prefix.Addr().Is6()) {
				return true, fmt.Errorf("%s spec.sourceCIDRs entries must match family prefixes", res.ID())
			}
		}
		for _, cidr := range spec.DestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil {
				return true, fmt.Errorf("%s spec.destinationCIDRs entries must be valid prefixes", res.ID())
			}
			switch defaultString(spec.Family, "ipv4") {
			case "ipv4":
				if !prefix.Addr().Is4() {
					return true, fmt.Errorf("%s spec.destinationCIDRs entries must be IPv4 prefixes when family is ipv4", res.ID())
				}
			case "ipv6":
				if !prefix.Addr().Is6() {
					return true, fmt.Errorf("%s spec.destinationCIDRs entries must be IPv6 prefixes when family is ipv6", res.ID())
				}
			}
		}
		for _, cidr := range spec.ExcludeDestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || (defaultString(spec.Family, "ipv4") == "ipv4" && !prefix.Addr().Is4()) || (defaultString(spec.Family, "ipv4") == "ipv6" && !prefix.Addr().Is6()) {
				return true, fmt.Errorf("%s spec.excludeDestinationCIDRs entries must match family prefixes", res.ID())
			}
		}
		if err := validateIPAddressSetRefs(res.ID(), "spec.destinationSetRefs", spec.DestinationSetRefs); err != nil {
			return true, err
		}
		if err := validateIPAddressSetRefs(res.ID(), "spec.excludeDestinationSetRefs", spec.ExcludeDestinationSetRefs); err != nil {
			return true, err
		}
		if spec.Mode == "hash" && len(spec.HashFields) == 0 {
			return true, fmt.Errorf("%s spec.hashFields is required when mode is hash", res.ID())
		}
		for _, field := range spec.HashFields {
			switch field {
			case "sourceAddress", "destinationAddress":
			default:
				return true, fmt.Errorf("%s spec.hashFields entries must be sourceAddress or destinationAddress", res.ID())
			}
		}
		if spec.Hysteresis != "" {
			if _, err := time.ParseDuration(spec.Hysteresis); err != nil {
				return true, fmt.Errorf("%s spec.hysteresis is invalid: %w", res.ID(), err)
			}
		}
		if len(spec.Candidates) == 0 {
			return true, fmt.Errorf("%s spec.candidates is required", res.ID())
		}
		if targetOS == platform.OSFreeBSD {
			if spec.Mode == "hash" {
				return true, validateFreeBSDEgressRoutePolicyHash(res.ID(), spec)
			}
			return true, fmt.Errorf("%s FreeBSD supports only the static sourceAddress hash PF route-to shape; mode %q is not supported", res.ID(), defaultString(spec.Mode, ""))
		}
		for i, candidate := range spec.Candidates {
			if candidate.Name == "" && candidate.Source == "" && candidate.EffectiveInterface() == "" && len(candidate.Targets) == 0 {
				return true, fmt.Errorf("%s spec.candidates[%d] requires name or source", res.ID(), i)
			}
			if candidate.Table != 0 && candidate.RouteTable != 0 {
				return true, fmt.Errorf("%s spec.candidates[%d] must not set both table and routeTable", res.ID(), i)
			}
			if candidate.RouteMetric != 0 && candidate.Metric != 0 {
				return true, fmt.Errorf("%s spec.candidates[%d] must not set both routeMetric and metric", res.ID(), i)
			}
			if len(candidate.Targets) == 0 && (candidate.Mark != 0 || candidate.Priority != 0 || candidate.EffectiveTable() != 0) {
				if candidate.Mark < 1 {
					return true, fmt.Errorf("%s spec.candidates[%d].mark must be greater than 0", res.ID(), i)
				}
				if candidate.Priority < 1 || candidate.Priority > 32765 {
					return true, fmt.Errorf("%s spec.candidates[%d].priority must be within 1-32765", res.ID(), i)
				}
				if candidate.EffectiveTable() < 1 {
					return true, fmt.Errorf("%s spec.candidates[%d].table must be greater than 0", res.ID(), i)
				}
			}
			if len(candidate.Targets) > 0 && spec.Mode != "hash" && spec.Mode != "priority" {
				return true, fmt.Errorf("%s spec.candidates[%d].targets requires mode hash or priority", res.ID(), i)
			}
			if len(candidate.Targets) > 0 {
				if candidate.Interface != "" || candidate.Device != "" || candidate.DeviceFrom.Resource != "" || candidate.Gateway != "" || candidate.GatewayFrom.Resource != "" || candidate.GatewaySource != "" || candidate.Table != 0 || candidate.RouteTable != 0 || candidate.Mark != 0 || candidate.RouteMetric != 0 || candidate.Metric != 0 {
					return true, fmt.Errorf("%s spec.candidates[%d] target candidates cannot set interface, gatewaySource, gateway, table, mark, or routeMetric directly", res.ID(), i)
				}
			}
			for j, target := range candidate.Targets {
				if target.Table != 0 && target.RouteTable != 0 {
					return true, fmt.Errorf("%s spec.candidates[%d].targets[%d] must not set both table and routeTable", res.ID(), i, j)
				}
				if target.RouteMetric != 0 && target.Metric != 0 {
					return true, fmt.Errorf("%s spec.candidates[%d].targets[%d] must not set both routeMetric and metric", res.ID(), i, j)
				}
				if target.EffectiveInterface() == "" {
					return true, fmt.Errorf("%s spec.candidates[%d].targets[%d].interface is required", res.ID(), i, j)
				}
				if target.GatewayFrom.Resource != "" && target.GatewayFrom.Field == "" {
					return true, fmt.Errorf("%s spec.candidates[%d].targets[%d].gatewayFrom.field is required", res.ID(), i, j)
				}
				targetGatewaySource := defaultString(target.GatewaySource, "none")
				switch targetGatewaySource {
				case "none":
					if target.Gateway != "" || target.GatewayFrom.Resource != "" {
						return true, fmt.Errorf("%s spec.candidates[%d].targets[%d].gateway and gatewayFrom require gatewaySource static, dhcpv4, or dhcpv6", res.ID(), i, j)
					}
				case "static":
					if (target.Gateway == "") == (target.GatewayFrom.Resource == "") {
						return true, fmt.Errorf("%s spec.candidates[%d].targets[%d] must set exactly one of gateway or gatewayFrom when gatewaySource is static", res.ID(), i, j)
					}
					if target.Gateway != "" {
						addr, err := netip.ParseAddr(target.Gateway)
						if err != nil {
							return true, fmt.Errorf("%s spec.candidates[%d].targets[%d].gateway must be an IP address", res.ID(), i, j)
						}
						if defaultString(spec.Family, "ipv4") == "ipv4" && !addr.Is4() {
							return true, fmt.Errorf("%s spec.candidates[%d].targets[%d].gateway must be IPv4 when family is ipv4", res.ID(), i, j)
						}
						if defaultString(spec.Family, "ipv4") == "ipv6" && !addr.Is6() {
							return true, fmt.Errorf("%s spec.candidates[%d].targets[%d].gateway must be IPv6 when family is ipv6", res.ID(), i, j)
						}
					}
				case "dhcpv4":
					if defaultString(spec.Family, "ipv4") != "ipv4" || target.Gateway != "" {
						return true, fmt.Errorf("%s spec.candidates[%d].targets[%d].gatewaySource dhcpv4 requires IPv4 and no literal gateway", res.ID(), i, j)
					}
				case "dhcpv6":
					if defaultString(spec.Family, "ipv4") != "ipv6" || target.Gateway != "" {
						return true, fmt.Errorf("%s spec.candidates[%d].targets[%d].gatewaySource dhcpv6 requires IPv6 and no literal gateway", res.ID(), i, j)
					}
				default:
					return true, fmt.Errorf("%s spec.candidates[%d].targets[%d].gatewaySource must be static, dhcpv4, dhcpv6, or none", res.ID(), i, j)
				}
				if target.EffectiveTable() < 1 {
					return true, fmt.Errorf("%s spec.candidates[%d].targets[%d].table must be greater than 0", res.ID(), i, j)
				}
				if target.Priority < 1 || target.Priority > 32765 {
					return true, fmt.Errorf("%s spec.candidates[%d].targets[%d].priority must be within 1-32765", res.ID(), i, j)
				}
				if target.Mark < 1 {
					return true, fmt.Errorf("%s spec.candidates[%d].targets[%d].mark must be greater than 0", res.ID(), i, j)
				}
			}
			if strings.Contains(candidate.Device, "${") {
				return true, fmt.Errorf("%s spec.candidates[%d].device status expressions were removed; use deviceFrom", res.ID(), i)
			}
			if strings.Contains(candidate.Gateway, "${") {
				return true, fmt.Errorf("%s spec.candidates[%d].gateway status expressions were removed; use gatewayFrom", res.ID(), i)
			}
			if candidate.DeviceFrom.Resource != "" && candidate.DeviceFrom.Field == "" {
				return true, fmt.Errorf("%s spec.candidates[%d].deviceFrom.field is required", res.ID(), i)
			}
			if candidate.GatewayFrom.Resource != "" && candidate.GatewayFrom.Field == "" {
				return true, fmt.Errorf("%s spec.candidates[%d].gatewayFrom.field is required", res.ID(), i)
			}
			if len(candidate.ReadyWhen) > 0 {
				return true, fmt.Errorf("%s spec.candidates[%d].ready_when was removed; use dependsOn", res.ID(), i)
			}
			if candidate.Weight < 0 {
				return true, fmt.Errorf("%s spec.candidates[%d].weight must be non-negative", res.ID(), i)
			}
			source := defaultString(candidate.GatewaySource, "none")
			switch source {
			case "none":
				if candidate.Gateway != "" || candidate.GatewayFrom.Resource != "" {
					return true, fmt.Errorf("%s spec.candidates[%d].gateway and gatewayFrom are only valid when gatewaySource is static, dhcpv4, or dhcpv6", res.ID(), i)
				}
			case "static":
				if (candidate.Gateway == "") == (candidate.GatewayFrom.Resource == "") {
					return true, fmt.Errorf("%s spec.candidates[%d] must set exactly one of gateway or gatewayFrom when gatewaySource is static", res.ID(), i)
				}
				if candidate.Gateway != "" {
					addr, err := netip.ParseAddr(candidate.Gateway)
					if err != nil {
						return true, fmt.Errorf("%s spec.candidates[%d].gateway must be an IP address", res.ID(), i)
					}
					if defaultString(spec.Family, "ipv4") == "ipv4" && !addr.Is4() {
						return true, fmt.Errorf("%s spec.candidates[%d].gateway must be an IPv4 address when family is ipv4", res.ID(), i)
					}
					if defaultString(spec.Family, "ipv4") == "ipv6" && !addr.Is6() {
						return true, fmt.Errorf("%s spec.candidates[%d].gateway must be an IPv6 address when family is ipv6", res.ID(), i)
					}
				}
			case "dhcpv4":
				if defaultString(spec.Family, "ipv4") != "ipv4" {
					return true, fmt.Errorf("%s spec.candidates[%d].gatewaySource dhcpv4 requires family ipv4", res.ID(), i)
				}
				if candidate.Gateway != "" {
					return true, fmt.Errorf("%s spec.candidates[%d].gateway must not be set when gatewaySource is dhcpv4", res.ID(), i)
				}
			case "dhcpv6":
				if defaultString(spec.Family, "ipv4") != "ipv6" {
					return true, fmt.Errorf("%s spec.candidates[%d].gatewaySource dhcpv6 requires family ipv6", res.ID(), i)
				}
				if candidate.Gateway != "" {
					return true, fmt.Errorf("%s spec.candidates[%d].gateway must not be set when gatewaySource is dhcpv6", res.ID(), i)
				}
			default:
				return true, fmt.Errorf("%s spec.candidates[%d].gatewaySource must be static, dhcpv4, dhcpv6, or none", res.ID(), i)
			}
		}
		if targetOS == platform.OSFreeBSD && egressRoutePolicyRequiresLinuxPolicyRouting(spec) {
			return true, fmt.Errorf("%s uses mark/table/hash policy routing, which is not supported on FreeBSD; pf route-to parity is not implemented", res.ID())
		}
	case "EventRule":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.EventRuleSpec()
		if err != nil {
			return true, err
		}
		switch spec.Pattern.Operator {
		case "all_of", "any_of", "sequence", "window", "absence", "throttle", "debounce", "count":
		default:
			return true, fmt.Errorf("%s spec.pattern.operator must be one of all_of, any_of, sequence, window, absence, throttle, debounce, count", res.ID())
		}
		if spec.Pattern.Topic == "" && len(spec.Pattern.Topics) == 0 {
			if spec.Pattern.Trigger == "" && spec.Pattern.Expected == "" {
				return true, fmt.Errorf("%s spec.pattern.topic, spec.pattern.topics, spec.pattern.trigger, or spec.pattern.expected is required", res.ID())
			}
		}
		for field, value := range map[string]string{
			"duration": spec.Pattern.Duration,
			"window":   spec.Pattern.Window,
			"quiet":    spec.Pattern.Quiet,
			"interval": spec.Pattern.Interval,
		} {
			if value != "" {
				if _, err := time.ParseDuration(value); err != nil {
					return true, fmt.Errorf("%s spec.pattern.%s is invalid: %w", res.ID(), field, err)
				}
			}
		}
		if spec.Pattern.Threshold < 0 {
			return true, fmt.Errorf("%s spec.pattern.threshold must be non-negative", res.ID())
		}
		if spec.Pattern.Rate < 0 {
			return true, fmt.Errorf("%s spec.pattern.rate must be non-negative", res.ID())
		}
		if spec.Pattern.CorrelateBy != "" && !validEventRuleCorrelation(spec.Pattern.CorrelateBy) {
			return true, fmt.Errorf("%s spec.pattern.correlate_by must be attributes.<key>, resource.{name,kind,apiVersion}, or daemon.{instance,kind}", res.ID())
		}
		if spec.Emit.Topic == "" {
			return true, fmt.Errorf("%s spec.emit.topic is required", res.ID())
		}
	case "DerivedEvent":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DerivedEventSpec()
		if err != nil {
			return true, err
		}
		if spec.Topic == "" {
			return true, fmt.Errorf("%s spec.topic is required", res.ID())
		}
		if len(spec.Inputs) == 0 {
			return true, fmt.Errorf("%s spec.inputs is required", res.ID())
		}
		switch defaultString(spec.EmitWhen, "all_true") {
		case "all_true", "any_true":
		default:
			return true, fmt.Errorf("%s spec.emitWhen must be all_true or any_true", res.ID())
		}
		switch defaultString(spec.RetractWhen, "any_false") {
		case "any_false", "all_false":
		default:
			return true, fmt.Errorf("%s spec.retractWhen must be any_false or all_false", res.ID())
		}
		if spec.Hysteresis != "" {
			if _, err := time.ParseDuration(spec.Hysteresis); err != nil {
				return true, fmt.Errorf("%s spec.hysteresis is invalid: %w", res.ID(), err)
			}
		}
	case "IPv4DefaultRoutePolicy":
		return true, fmt.Errorf("%s is not supported; use EgressRoutePolicy with candidates directly", res.ID())
	case "IPv4PolicyRoute":
		return true, fmt.Errorf("%s is not supported; use EgressRoutePolicy with one marked candidate", res.ID())
	case "IPv4PolicyRouteSet":
		return true, fmt.Errorf("%s is not supported; put hashFields and targets under EgressRoutePolicy candidates", res.ID())
	case "IPv4Route":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv4RouteSpec()
		if err != nil {
			return true, err
		}
		if spec.Destination == "" {
			return true, fmt.Errorf("%s spec.destination is required", res.ID())
		}
		if _, err := netip.ParsePrefix(spec.Destination); err != nil {
			return true, fmt.Errorf("%s spec.destination is invalid: %w", res.ID(), err)
		}
		routeType := defaultString(spec.Type, "unicast")
		switch routeType {
		case "unicast", "blackhole":
		default:
			return true, fmt.Errorf("%s spec.type must be unicast or blackhole", res.ID())
		}
		if strings.Contains(spec.Device, "${") {
			return true, fmt.Errorf("%s spec.device status expressions were removed; use deviceFrom", res.ID())
		}
		if strings.Contains(spec.Gateway, "${") {
			return true, fmt.Errorf("%s spec.gateway status expressions were removed; use gatewayFrom", res.ID())
		}
		if strings.Contains(spec.PreferredSource, "${") {
			return true, fmt.Errorf("%s spec.preferredSource status expressions are not supported", res.ID())
		}
		if len(spec.ReadyWhen) > 0 {
			return true, fmt.Errorf("%s spec.ready_when was removed; use spec.dependsOn", res.ID())
		}
		if routeType == "blackhole" {
			if spec.Device != "" || spec.DeviceFrom.Resource != "" || spec.Gateway != "" || spec.GatewayFrom.Resource != "" || spec.PreferredSource != "" {
				return true, fmt.Errorf("%s spec.device, spec.deviceFrom, spec.gateway, spec.gatewayFrom, and spec.preferredSource are not valid when spec.type is blackhole", res.ID())
			}
		}
		if spec.Gateway != "" {
			addr, err := netip.ParseAddr(spec.Gateway)
			if err != nil || !addr.Is4() {
				return true, fmt.Errorf("%s spec.gateway must be an IPv4 address", res.ID())
			}
		}
		if spec.PreferredSource != "" {
			addr, err := netip.ParseAddr(spec.PreferredSource)
			if err != nil || !addr.Is4() {
				return true, fmt.Errorf("%s spec.preferredSource must be an IPv4 address", res.ID())
			}
		}
	default:
		return false, nil
	}
	return true, nil
}

func egressRoutePolicyRequiresLinuxPolicyRouting(spec api.EgressRoutePolicySpec) bool {
	if spec.Mode == "mark" || spec.Mode == "hash" || len(spec.HashFields) > 0 {
		return true
	}
	for _, candidate := range spec.Candidates {
		if candidate.Mark != 0 || candidate.EffectiveTable() != 0 || len(candidate.Targets) > 0 {
			return true
		}
	}
	return false
}

// validateFreeBSDEgressRoutePolicyHash accepts only the PF route-to subset.
// A multi-routehost PF pool can use round-robin sticky-address, not source-hash,
// so every Linux-only or runtime-resolved knob must fail at validation.
func validateFreeBSDEgressRoutePolicyHash(resourceID string, spec api.EgressRoutePolicySpec) error {
	family := defaultString(spec.Family, "ipv4")
	if family != "ipv4" && family != "ipv6" {
		return fmt.Errorf("%s FreeBSD route-to requires family ipv4 or ipv6", resourceID)
	}
	if len(spec.HashFields) != 1 || spec.HashFields[0] != "sourceAddress" {
		return fmt.Errorf("%s FreeBSD route-to requires spec.hashFields: [sourceAddress]", resourceID)
	}
	if len(spec.SourceCIDRs) == 0 {
		return fmt.Errorf("%s FreeBSD route-to requires spec.sourceCIDRs", resourceID)
	}
	if len(spec.DestinationSetRefs) != 0 || len(spec.ExcludeDestinationSetRefs) != 0 {
		return fmt.Errorf("%s FreeBSD route-to does not support destination address-set references", resourceID)
	}
	if len(spec.ExcludeDestinationCIDRs) > 1 {
		return fmt.Errorf("%s FreeBSD route-to supports at most one excluded destination CIDR", resourceID)
	}
	if len(spec.DestinationCIDRs) != 0 && len(spec.ExcludeDestinationCIDRs) != 0 {
		return fmt.Errorf("%s FreeBSD route-to does not support combined destinationCIDRs and excludeDestinationCIDRs", resourceID)
	}
	if spec.Selection != "" || spec.Hysteresis != "" {
		return fmt.Errorf("%s FreeBSD route-to does not support selection or hysteresis", resourceID)
	}
	if len(spec.Candidates) != 1 {
		return fmt.Errorf("%s FreeBSD route-to requires exactly one candidate", resourceID)
	}
	candidate := spec.Candidates[0]
	if !api.BoolDefault(candidate.Enabled, true) || candidate.Source != "" || candidate.EffectiveInterface() != "" || candidate.DeviceFrom.Resource != "" || candidate.Gateway != "" || candidate.GatewayFrom.Resource != "" || candidate.GatewaySource != "" || candidate.EffectiveTable() != 0 || candidate.Priority != 0 || candidate.Mark != 0 || candidate.EffectiveMetric() != 0 || candidate.Weight != 0 || candidate.HealthCheck != "" || len(candidate.DependsOn) != 0 || len(candidate.ReadyWhen) != 0 || len(candidate.When.State) != 0 || len(candidate.When.All) != 0 || len(candidate.When.Any) != 0 {
		return fmt.Errorf("%s FreeBSD route-to candidate contains unsupported direct or dynamic fields", resourceID)
	}
	if len(candidate.Targets) < 2 {
		return fmt.Errorf("%s FreeBSD route-to requires at least two static targets", resourceID)
	}
	for i, target := range candidate.Targets {
		if target.Interface != "" && target.OutboundInterface != "" {
			return fmt.Errorf("%s FreeBSD route-to target[%d] must not set both interface and outboundInterface", resourceID, i)
		}
		if target.EffectiveInterface() == "" || target.GatewaySource != "static" || target.GatewayFrom.Resource != "" || target.Gateway == "" || target.EffectiveTable() != 0 || target.Priority != 0 || target.Mark != 0 || target.EffectiveMetric() != 0 || target.HealthCheck != "" {
			return fmt.Errorf("%s FreeBSD route-to target[%d] must contain only interface and static gateway", resourceID, i)
		}
		addr, err := netip.ParseAddr(target.Gateway)
		if err != nil || (family == "ipv4" && !addr.Is4()) || (family == "ipv6" && (!addr.Is6() || addr.IsLinkLocalUnicast())) {
			return fmt.Errorf("%s FreeBSD route-to target[%d].gateway must be a non-link-local %s address", resourceID, i, family)
		}
	}
	return nil
}

func validateBGPPeersFrom(resourceID string, index int, source api.BGPPeersSourceSpec) error {
	kind, name, ok := strings.Cut(strings.TrimSpace(source.Resource), "/")
	if !ok || kind != "SAMRRSet" || strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s spec.peersFrom[%d].resource must reference SAMRRSet/<name>", resourceID, index)
	}
	return nil
}
