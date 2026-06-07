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

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
)

func renderCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("render requires a target: alpine")
	}
	switch args[0] {
	case "alpine":
		return renderAlpineCommand(args[1:], stdout)
	default:
		return fmt.Errorf("unknown render target %q", args[0])
	}
}

func renderAlpineCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("render alpine", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Render Alpine/OpenRC artifacts from startup config.",
			"routerctl render alpine --config /usr/local/etc/routerd/router.yaml\n"+
				"routerctl render alpine --config router.yaml --out-dir /tmp/routerd-render")
	}
	configPath := fs.String("config", defaultConfigPath(), "config path")
	outDir := fs.String("out-dir", "", "output directory for Alpine generated files; writes OpenRC scripts to stdout when empty")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
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
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	for _, name := range names {
		path := strings.TrimRight(*outDir, "/") + "/" + name
		perm := os.FileMode(0o644)
		if strings.HasPrefix(name, "openrc-") {
			perm = 0o755
		}
		if err := os.WriteFile(path, files[name], perm); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote %s\n", path)
	}
	return nil
}

func routerInterfaceAliases(resources []api.Resource) map[string]string {
	aliases := map[string]string{}
	for _, res := range resources {
		if res.Kind != "Interface" {
			continue
		}
		spec, err := res.InterfaceSpec()
		if err != nil || strings.TrimSpace(spec.IfName) == "" {
			continue
		}
		aliases[res.Metadata.Name] = spec.IfName
	}
	return aliases
}
