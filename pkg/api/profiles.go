package api

import (
	"path"
	"strings"
)

const (
	IPv6PDClientNetworkd = "networkd"
	IPv6PDClientDHCPv6C  = "dhcp6c"
	IPv6PDClientDHCPCD   = "dhcpcd"
)

type IPv6PDClientContext struct {
	OS      string
	NixOS   bool
	Client  string
	Profile string
}

type IPv6PDKnownNGCombination struct {
	OS      string
	NixOS   *bool
	Client  string
	Profile string
	Reason  string
	DocLink string
}

var KnownIPv6PDNGCombinations = []IPv6PDKnownNGCombination{
	{
		OS:      "freebsd",
		Client:  IPv6PDClientDHCPCD,
		Profile: "ntt-*",
		Reason:  "FreeBSD dhcpcd generated DUID-LLT unless the DUID file was forced, and still failed NTT/HGW PD acquisition in lab testing.",
		DocLink: "docs/knowledge-base/ntt-ngn-pd-acquisition.md#freebsd-dhcpcd-test-record",
	},
	{
		OS:      "linux",
		Client:  IPv6PDClientNetworkd,
		Profile: "ntt-*",
		Reason:  "systemd-networkd was observed sending Renew/Rebind with IA_PD lifetime 0/0; the HGW silently ignored it.",
		DocLink: "docs/knowledge-base/dhcpv6-pd-clients.md",
	},
}

func ValidIPv6PDClient(client string) bool {
	switch client {
	case "", IPv6PDClientNetworkd, IPv6PDClientDHCPv6C, IPv6PDClientDHCPCD:
		return true
	default:
		return false
	}
}

func ValidIPv6PDProfile(profile string) bool {
	switch profile {
	case "", IPv6PDProfileDefault, IPv6PDProfileNTTNGNDirectHikariDenwa, IPv6PDProfileNTTHGWLANPD:
		return true
	default:
		return false
	}
}

func EffectiveIPv6PDClient(osName string, nixOS bool, profile, configured string) string {
	if configured != "" {
		return configured
	}
	effectiveProfile := profile
	if effectiveProfile == "" {
		effectiveProfile = IPv6PDProfileDefault
	}
	switch strings.ToLower(osName) {
	case "freebsd":
		return IPv6PDClientDHCPv6C
	case "linux":
		if IsNTTIPv6PDProfile(effectiveProfile) {
			return IPv6PDClientDHCPCD
		}
		return IPv6PDClientNetworkd
	default:
		return IPv6PDClientNetworkd
	}
}

func MatchKnownIPv6PDNGCombinations(ctx IPv6PDClientContext) []IPv6PDKnownNGCombination {
	var matches []IPv6PDKnownNGCombination
	for _, item := range KnownIPv6PDNGCombinations {
		if item.OS != "" && !strings.EqualFold(item.OS, ctx.OS) {
			continue
		}
		if item.NixOS != nil && *item.NixOS != ctx.NixOS {
			continue
		}
		if item.Client != "" && item.Client != ctx.Client {
			continue
		}
		if item.Profile != "" && !matchIPv6PDProfilePattern(item.Profile, ctx.Profile) {
			continue
		}
		matches = append(matches, item)
	}
	return matches
}

func matchIPv6PDProfilePattern(patternValue, profile string) bool {
	if profile == "" {
		profile = IPv6PDProfileDefault
	}
	if patternValue == "ntt-*" {
		return IsNTTIPv6PDProfile(profile)
	}
	ok, err := path.Match(patternValue, profile)
	return err == nil && ok
}
