// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"gopkg.in/yaml.v3"
)

type importInterfaces []string

func (v *importInterfaces) String() string { return strings.Join(*v, ", ") }
func (v *importInterfaces) Set(value string) error {
	if value == "" || filepath.Base(value) != value || value == "." || value == ".." {
		return fmt.Errorf("invalid interface name %q", value)
	}
	for _, existing := range *v {
		if existing == value {
			return nil
		}
	}
	*v = append(*v, value)
	return nil
}

type importAddress struct {
	Family    string `json:"family"`
	Local     string `json:"local"`
	PrefixLen int    `json:"prefixlen"`
	Scope     string `json:"scope"`
	Dynamic   bool   `json:"dynamic"`
}

type importLink struct {
	IfName   string          `json:"ifname"`
	Flags    []string        `json:"flags"`
	MTU      int             `json:"mtu"`
	Master   string          `json:"master"`
	AddrInfo []importAddress `json:"addr_info"`
}

func importCommand(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var interfaces importInterfaces
	fs.Var(&interfaces, "interface", "physical interface to inspect (repeatable)")
	configPath := fs.String("config", "", "existing router configuration for interface names")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: routerctl import --interface <name> [--interface <name> ...] [--config <router.yaml>]")
		fmt.Fprintln(stderr, "Outputs an incomplete YAML candidate; merge it into a complete configuration before applying.")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 || len(interfaces) == 0 {
		return errors.New("import requires at least one --interface and no positional arguments")
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("interface import is not yet supported on %s", runtime.GOOS)
	}
	name := "imported"
	aliases := map[string]api.Resource{}
	usedNames := map[string]bool{}
	if *configPath != "" {
		router, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		name = router.Metadata.Name
		for _, resource := range router.Spec.Resources {
			if resource.Kind != "Interface" {
				continue
			}
			spec, err := resource.InterfaceSpec()
			if err != nil {
				return err
			}
			if previous, found := aliases[spec.IfName]; found && previous.Metadata.Name != resource.Metadata.Name {
				return fmt.Errorf("existing configuration defines interface %q more than once", spec.IfName)
			}
			aliases[spec.IfName] = resource
			usedNames[resource.Metadata.Name] = true
		}
	}
	router := api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: name}}
	var warnings []string
	for _, ifname := range interfaces {
		if _, err := os.Stat(filepath.Join("/sys/class/net", ifname, "device")); err != nil {
			return fmt.Errorf("interface %s is not a discoverable physical interface: %w", ifname, err)
		}
		out, err := exec.Command("ip", "-j", "address", "show", "dev", ifname).CombinedOutput()
		if err != nil {
			return fmt.Errorf("observe interface %s: %w: %s", ifname, err, strings.TrimSpace(string(out)))
		}
		var links []importLink
		if err := json.Unmarshal(out, &links); err != nil {
			return fmt.Errorf("decode interface %s: %w", ifname, err)
		}
		if len(links) != 1 || links[0].IfName != ifname {
			return fmt.Errorf("interface %s not found in kernel output", ifname)
		}
		link := links[0]
		up := false
		for _, f := range link.Flags {
			if f == "UP" {
				up = true
			}
		}
		if !up {
			warnings = append(warnings, fmt.Sprintf("%s: administratively down; current schema does not express explicit down", ifname))
		}
		if link.Master != "" {
			warnings = append(warnings, fmt.Sprintf("%s: master %s must be imported separately", ifname, link.Master))
		}
		mtu := link.MTU
		if mtu < 576 || mtu > 9216 {
			warnings = append(warnings, fmt.Sprintf("%s: MTU %d cannot be represented by Interface", ifname, mtu))
			mtu = 0
		}
		resource, found := aliases[ifname]
		if found {
			spec, err := resource.InterfaceSpec()
			if err != nil {
				return err
			}
			if spec.AdminUp != up || (spec.MTU != 0 && spec.MTU != mtu) {
				warnings = append(warnings, fmt.Sprintf("%s: observed link settings differ from the existing Interface definition", ifname))
			}
			resource.Status = nil
		} else {
			resourceName := ifname
			if usedNames[resourceName] {
				resourceName = "import-" + ifname
				for suffix := 2; usedNames[resourceName]; suffix++ {
					resourceName = fmt.Sprintf("import-%s-%d", ifname, suffix)
				}
			}
			resource = api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: resourceName}, Spec: api.InterfaceSpec{IfName: ifname, Managed: true, AdminUp: up, MTU: mtu}}
		}
		usedNames[resource.Metadata.Name] = true
		router.Spec.Resources = append(router.Spec.Resources, resource)
		for _, address := range link.AddrInfo {
			_, err := netip.ParseAddr(address.Local)
			if err != nil {
				return fmt.Errorf("interface %s has invalid address %q: %w", ifname, address.Local, err)
			}
			origin := "origin unknown"
			if address.Dynamic {
				origin = "dynamic"
			}
			warnings = append(warnings, fmt.Sprintf("%s: address %s/%d (%s, scope %s) requires source configuration", ifname, address.Local, address.PrefixLen, origin, address.Scope))
		}
		for _, family := range []string{"-4", "-6"} {
			out, err := exec.Command("ip", "-j", family, "route", "show", "table", "all", "dev", ifname).CombinedOutput()
			if err != nil {
				return fmt.Errorf("observe %s routes for %s: %w: %s", family, ifname, err, strings.TrimSpace(string(out)))
			}
			var routes []json.RawMessage
			if err := json.Unmarshal(out, &routes); err != nil {
				return fmt.Errorf("decode %s routes for %s: %w", family, ifname, err)
			}
			for _, route := range routes {
				warnings = append(warnings, fmt.Sprintf("%s: %s route requires source configuration: %s", ifname, family, string(route)))
			}
		}
	}
	data, err := yaml.Marshal(router)
	if err != nil {
		return err
	}
	fmt.Fprintln(stderr, "WARNING: this is a partial candidate, not a complete machine configuration; merge and review all networking before apply.")
	fmt.Fprintln(stderr, "WARNING: addresses, routes, policy rules, dynamic network sources, link attributes, and existing network managers are not imported.")
	for _, warning := range warnings {
		fmt.Fprintln(stderr, "WARNING:", warning)
	}
	if _, err := io.WriteString(stdout, "# Partial candidate: merge into a complete machine configuration before apply.\n"); err != nil {
		return err
	}
	_, err = stdout.Write(data)
	return err
}
