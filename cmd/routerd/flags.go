// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"flag"
	"fmt"

	"routerd/pkg/api"
)

type applyFlagValues struct {
	ConfigPath         *string
	StatusFile         *string
	NetplanPath        *string
	DnsmasqConfigPath  *string
	DnsmasqServicePath *string
	NftablesPath       *string
	LedgerPath         *string
	StatePath          *string
	OverrideClient     *string
	OverrideProfile    *string
	Once               *bool
	DryRun             *bool
	SkipServiceManager *bool
	AllowMgmtLockout   *bool
}

func registerApplyFlags(fs *flag.FlagSet, includeConfig, includeOnce bool) applyFlagValues {
	var flags applyFlagValues
	if includeConfig {
		flags.ConfigPath = fs.String("config", defaultConfigPath, "config path")
	}
	flags.StatusFile = fs.String("status-file", defaultStatusFile(), "status file")
	flags.NetplanPath = fs.String("netplan-file", defaultNetplanPath, "routerd-managed netplan file")
	flags.DnsmasqConfigPath = fs.String("dnsmasq-file", defaultDnsmasqConfigPath, "routerd-managed dnsmasq config file")
	flags.DnsmasqServicePath = fs.String("dnsmasq-service-file", defaultDnsmasqServicePath, "routerd-managed dnsmasq systemd unit file")
	flags.NftablesPath = fs.String("nftables-file", defaultNftablesPath, "routerd-managed nftables ruleset file")
	flags.LedgerPath = fs.String("ledger-file", defaultLedgerPath, "routerd ownership ledger file")
	flags.StatePath = fs.String("state-file", defaultStatePath, "routerd state database file")
	flags.OverrideClient = fs.String("override-client", "", "override DHCPv6PrefixDelegation client for this apply: routerd-dhcpv6-client, networkd, dhcp6c, or dhcpcd")
	flags.OverrideProfile = fs.String("override-profile", "", "override DHCPv6PrefixDelegation profile for this apply")
	if includeOnce {
		flags.Once = fs.Bool("once", false, "run one apply loop")
	}
	flags.DryRun = fs.Bool("dry-run", false, "plan without applying changes")
	flags.SkipServiceManager = fs.Bool("skip-service-manager", false, "skip applying service-manager units during this apply")
	flags.AllowMgmtLockout = fs.Bool("allow-mgmt-lockout", false, "allow apply to continue despite ManagementAccess lockout findings")
	return flags
}

func (flags applyFlagValues) validateOverrides() error {
	if !api.ValidIPv6PDClient(*flags.OverrideClient) {
		return fmt.Errorf("invalid --override-client %q", *flags.OverrideClient)
	}
	if !api.ValidIPv6PDProfile(*flags.OverrideProfile) {
		return fmt.Errorf("invalid --override-profile %q", *flags.OverrideProfile)
	}
	return nil
}

func (flags applyFlagValues) applyOptions(configPath string) applyOptions {
	return applyOptions{
		ConfigPath:          configPath,
		StatusFile:          *flags.StatusFile,
		NetplanPath:         *flags.NetplanPath,
		DnsmasqConfigPath:   *flags.DnsmasqConfigPath,
		DnsmasqServicePath:  runtimeDnsmasqServicePath(*flags.DnsmasqServicePath),
		NftablesPath:        *flags.NftablesPath,
		LedgerPath:          *flags.LedgerPath,
		StatePath:           *flags.StatePath,
		OverrideClient:      *flags.OverrideClient,
		OverrideProfile:     *flags.OverrideProfile,
		DryRun:              *flags.DryRun,
		SkipServiceManager:  *flags.SkipServiceManager,
		AllowMgmtLockout:    *flags.AllowMgmtLockout,
		AnnounceDryRunToCLI: true,
	}
}
