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
	case "serve":
		return serveCommand(args[1:], stdout, stderr)
	case "apply":
		return applyCommand(args[1:], stdout, stderr)
	case "rollback":
		return rollbackCommand(args[1:], stdout, stderr)
	case "validate":
		return validateCommand(args[1:], stdout)
	case "check":
		return checkCommand(args[1:], stdout)
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
	fmt.Fprintln(w, "  serve --config <path> [options]   run the control loop and serve control sockets")
	fmt.Fprintln(w, "  serve --config <path> --once      converge once and exit (no control sockets)")
	fmt.Fprintln(w, "  apply --config <path> --once      apply config once")
	fmt.Fprintln(w, "  rollback --list|--to <generation> list or re-apply saved config generations")
	fmt.Fprintln(w, "  validate --config <path>          validate config")
	fmt.Fprintln(w, "  check --config <path>             run preflight check")
	fmt.Fprintln(w, "  version                          print the routerd version")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "serve options:")
	fmt.Fprintln(w, "  --config <path>                  config path")
	fmt.Fprintln(w, "  --socket <path>                  control Unix domain socket path")
	fmt.Fprintln(w, "  --status-socket <path>           read-only status Unix domain socket path")
	fmt.Fprintln(w, "  --state-file <path>              routerd state database file")
	fmt.Fprintln(w, "  --controllers <list>             comma-separated controllers to run (default all; use bgp for isolated BGP labs)")
	fmt.Fprintln(w, "  --apply-interval <dur>           periodic apply interval; 0 disables scheduled apply")
	fmt.Fprintln(w, "  --graceful-stop-timeout <dur>    wait up to this long for mobility make-before-break handoff on SIGTERM/SIGINT (default 20s; 0 disables)")
	fmt.Fprintln(w, "  --once                           converge once and exit without serving control sockets")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Run 'routerctl --help' for config authoring, apply, status, and doctor verbs.")
}
