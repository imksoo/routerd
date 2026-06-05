// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/netconfigbackend"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
)

func renderCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("render requires a target: nixos, freebsd, or alpine")
	}
	switch args[0] {
	case "nixos":
		return renderNixOSCommand(args[1:], stdout)
	case "freebsd":
		return renderFreeBSDCommand(args[1:], stdout)
	case "alpine":
		return renderAlpineCommand(args[1:], stdout)
	default:
		return fmt.Errorf("unknown render target %q", args[0])
	}
}

func renderAlpineCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("render alpine", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	outDir := fs.String("out-dir", "", "output directory for Alpine generated files; writes OpenRC scripts to stdout when empty")
	if err := fs.Parse(args); err != nil {
		return err
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if err := config.Validate(router); err != nil {
		return err
	}
	data, err := render.OpenRC(router)
	if err != nil {
		return err
	}
	files := map[string][]byte{}
	dnsmasqConfig, dnsmasqWarnings, err := render.DnsmasqConfig(router, render.DnsmasqRuntime{
		RuntimeDir: "/run/routerd",
		LeaseFile:  (platform.Defaults{StateDir: "/var/lib/routerd"}).DnsmasqLeaseFile(),
	})
	if err != nil {
		return err
	}
	for _, warning := range dnsmasqWarnings {
		fmt.Fprintf(stdout, "warning: %s\n", warning)
	}
	if len(dnsmasqConfig) > 0 {
		files["dnsmasq.conf"] = dnsmasqConfig
	}
	keepalivedConfig, err := render.KeepalivedConfig(router, routerInterfaceAliases(router.Spec.Resources))
	if err != nil {
		return err
	}
	if len(keepalivedConfig) > 0 {
		files["keepalived.conf"] = keepalivedConfig
	}
	for name, content := range data.InitScripts {
		files["openrc-"+name] = content
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	if *outDir == "" {
		for _, name := range names {
			fmt.Fprintf(stdout, "### %s\n", name)
			if _, err := stdout.Write(files[name]); err != nil {
				return err
			}
			if len(files[name]) == 0 || files[name][len(files[name])-1] != '\n' {
				fmt.Fprintln(stdout)
			}
		}
		return nil
	}
	if err := os.MkdirAll(*outDir, 0755); err != nil {
		return err
	}
	for _, name := range names {
		path := strings.TrimRight(*outDir, "/") + "/" + name
		perm := os.FileMode(0644)
		if strings.HasPrefix(name, "openrc-") {
			perm = 0755
		}
		if err := os.WriteFile(path, files[name], perm); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote %s\n", path)
	}
	return nil
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
	if err := config.Validate(router); err != nil {
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

func renderNixOSCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("render nixos", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	outPath := fs.String("out", "", "output path for routerd-generated.nix; writes to stdout when empty")
	if err := fs.Parse(args); err != nil {
		return err
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if err := config.Validate(router); err != nil {
		return err
	}
	files, err := netconfigbackend.NixOS{}.Render(router)
	if err != nil {
		return err
	}
	var data []byte
	if len(files) > 0 {
		data = files[0].Data
	}
	if *outPath == "" {
		_, err := stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepathDir(*outPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(*outPath, data, 0644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s\n", *outPath)
	return nil
}
