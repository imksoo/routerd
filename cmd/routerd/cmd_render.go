// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
)

func renderCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("render requires a target: freebsd")
	}
	switch args[0] {
	case "freebsd":
		return renderFreeBSDCommand(args[1:], stdout)
	default:
		return fmt.Errorf("unknown render target %q", args[0])
	}
}

func renderFreeBSDCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("render freebsd", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	outDir := fs.String("out-dir", "", "output directory for FreeBSD generated files; writes rc.conf fragment to stdout when empty")
	if err := fs.Parse(args); err != nil {
		return err
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if err := config.ValidateForOS(router, platform.OSFreeBSD); err != nil {
		return err
	}
	data, err := render.FreeBSDWithPPPoEPasswords(router, pppoePassword)
	if err != nil {
		return err
	}
	if *outDir == "" {
		_, err := stdout.Write(data.RCConf)
		return err
	}
	if err := os.MkdirAll(*outDir, 0755); err != nil {
		return err
	}
	files := map[string][]byte{
		"rc.conf.d-routerd": data.RCConf,
	}
	dnsmasqConfig, dnsmasqWarnings, err := render.DnsmasqConfig(router, render.DnsmasqRuntime{
		RuntimeDir: "/var/run/routerd",
		LeaseFile:  (platform.Defaults{StateDir: "/var/db/routerd"}).DnsmasqLeaseFile(),
	})
	if err != nil {
		return err
	}
	for _, warning := range dnsmasqWarnings {
		fmt.Fprintf(stdout, "warning: %s\n", warning)
	}
	if len(dnsmasqConfig) > 0 {
		files["dnsmasq.conf"] = dnsmasqConfig
		files["rc.d-routerd_dnsmasq"] = render.DnsmasqRCScript("/usr/local/etc/routerd/dnsmasq.conf", "/var/run/routerd", "/var/db/routerd/dnsmasq", "/usr/local/sbin/dnsmasq")
	}
	if len(data.DHCPClient) > 0 {
		files["dhclient.conf"] = data.DHCPClient
	}
	if len(data.MPD5) > 0 {
		files["mpd5.conf"] = data.MPD5
	}
	if len(data.PF) > 0 {
		files["pf.conf"] = data.PF
	}
	if len(data.PackageInstall) > 0 {
		files["install-packages.sh"] = data.PackageInstall
	}
	for name, content := range data.RCDScripts {
		files["rc.d-"+name] = content
	}
	for name, content := range files {
		path := strings.TrimRight(*outDir, "/") + "/" + name
		perm := os.FileMode(0644)
		if name == "mpd5.conf" {
			perm = 0600
		} else if strings.HasPrefix(name, "rc.d-") || name == "install-packages.sh" {
			perm = 0755
		}
		if err := os.WriteFile(path, content, perm); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote %s\n", path)
	}
	return nil
}
