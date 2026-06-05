// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	routerotel "github.com/imksoo/routerd/pkg/otel"
	"github.com/imksoo/routerd/pkg/platform"
	routerversion "github.com/imksoo/routerd/pkg/version"
)

var version = routerversion.String()

const (
	routerdDnsmasqService       = "routerd-dnsmasq.service"
	defaultKeepalivedConfigPath = "/etc/keepalived/keepalived.conf"
	freebsdSysrcStateKey        = "freebsd.applyFreeBSDConfig.lastSysrcKeys"
)

var (
	platformDefaults, platformFeatures = platform.Current()

	defaultConfigPath           = platformDefaults.ConfigFile()
	defaultPluginDir            = platformDefaults.PluginDir
	defaultNetplanPath          = platformDefaults.NetplanFile
	defaultDnsmasqConfigPath    = platformDefaults.DnsmasqConfigFile
	defaultDnsmasqServicePath   = platformDefaults.DnsmasqServiceFile
	defaultFreeBSDDHClientPath  = platformDefaults.FreeBSDDHClientConfigFile
	defaultFreeBSDMPD5Path      = platformDefaults.FreeBSDMPD5ConfigFile
	defaultFreeBSDPFPath        = platformDefaults.FreeBSDPFConfigFile
	defaultNftablesPath         = platformDefaults.NftablesFile
	defaultRouteNftablesPath    = platformDefaults.DefaultRouteNftablesFile
	defaultTimesyncdPath        = platformDefaults.TimesyncdDropinFile
	defaultLedgerPath           = platformDefaults.DBFile()
	defaultStatePath            = platformDefaults.DBFile()
	runtimeKeepalivedConfigPath = defaultKeepalivedConfigPath
	pppoeCHAPSecretsPath        = platformDefaults.PPPoEChapSecretsFile
	pppoePAPSecretsPath         = platformDefaults.PPPoEPapSecretsFile
	pdClientLeaseDir            = filepath.Join(platformDefaults.StateDir, "dhcpv6-client")
	legacyFreeBSDStateDir       = "/var/lib/routerd"
)

var errNoIPv6PrefixAvailable = errors.New("no IPv6 prefix available")

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	telemetry, err := routerotel.Setup(context.Background(), "routerd")
	if err != nil {
		return err
	}
	defer telemetry.ShutdownGracefully()
	if len(args) == 0 {
		usage(stderr)
		return errors.New("missing command")
	}

	switch args[0] {
	case "version", "--version":
		fmt.Fprintf(stdout, "routerd %s\n", version)
		return nil
	case "validate":
		return validateCommand(args[1:], stdout)
	case "check":
		return checkCommand(args[1:], stdout)
	case "observe":
		return configCommand(args[1:], stdout, "observe")
	case "plan":
		return configCommand(args[1:], stdout, "plan")
	case "adopt":
		return adoptCommand(args[1:], stdout)
	case "render":
		return renderCommand(args[1:], stdout)
	case "apply":
		return applyCommand(args[1:], stdout, stderr)
	case "rollback":
		return rollbackCommand(args[1:], stdout, stderr)
	case "delete":
		return deleteCommand(args[1:], stdout)
	case "serve":
		return serveCommand(args[1:], stdout, stderr)
	case "run":
		return configCommand(args[1:], stdout, "run")
	case "status":
		return statusCommand(args[1:], stdout)
	case "plugin":
		return pluginCommand(args[1:], stdout)
	case "help", "-h", "--help":
		usage(stdout)
		return nil
	default:
		usage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerd <command> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  validate --config <path>")
	fmt.Fprintln(w, "  observe --config <path>")
	fmt.Fprintln(w, "  plan --config <path>")
	fmt.Fprintln(w, "  adopt --config <path> --candidates")
	fmt.Fprintln(w, "  adopt --config <path> --apply")
	fmt.Fprintln(w, "  render nixos --config <path> [--out <path>]")
	fmt.Fprintln(w, "  render freebsd --config <path> [--out-dir <path>]")
	fmt.Fprintln(w, "  render alpine --config <path> [--out-dir <path>]")
	fmt.Fprintln(w, "  apply --config <path> --once [--dry-run] [--allow-mgmt-lockout] [--override-client <client>] [--override-profile <profile>]")
	fmt.Fprintln(w, "  rollback [--list] [--to <generation>] [--dry-run]")
	fmt.Fprintln(w, "  delete <kind>/<name> [--dry-run] [--force] [--api-version <version>]")
	fmt.Fprintln(w, "  delete -f <path> [--dry-run]")
	fmt.Fprintln(w, "  serve --config <path> [--socket <path>] [--status-socket <path>]")
	fmt.Fprintln(w, "  run --config <path>")
	fmt.Fprintln(w, "  status [--status-file <path>]")
	fmt.Fprintln(w, "  plugin list --plugin-dir <path>")
	fmt.Fprintln(w, "  plugin inspect <plugin-name> --plugin-dir <path>")
}
