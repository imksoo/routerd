// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/render"
)

func firewallCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return errors.New("firewall requires subcommand")
	}
	switch args[0] {
	case "test":
		return firewallTestCommand(args[1:], stdout)
	default:
		return fmt.Errorf("unknown firewall subcommand %q", args[0])
	}
}

func firewallTestCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("firewall test", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"FirewallZone / FirewallRule / 内部 hole / 暗黙 zone matrix を引いて、\n"+
				"from / to / proto / dport の組み合わせに対して accept か drop かを判定する。\n"+
				"引数は --from trust --to self の形でも from=trust to=self の形でも受け付ける。",
			"routerctl firewall test from=trust to=self proto=tcp dport=22\n"+
				"routerctl firewall test --from untrust --to trust --proto udp --dport 53\n"+
				"routerctl firewall test --config /etc/routerd/config.yaml from=trust to=untrust")
	}
	configPath := fs.String("config", defaultConfigPath(), "config path")
	from := fs.String("from", "", "source zone")
	to := fs.String("to", "self", "destination zone")
	src := fs.String("src", "", "source zone alias")
	dst := fs.String("dst", "", "destination zone alias")
	proto := fs.String("proto", "", "protocol")
	dport := fs.Int("dport", 0, "destination port")
	var normalized []string
	for _, arg := range args {
		if strings.Contains(arg, "=") && !strings.HasPrefix(arg, "-") {
			parts := strings.SplitN(arg, "=", 2)
			normalized = append(normalized, "--"+parts[0], parts[1])
			continue
		}
		normalized = append(normalized, arg)
	}
	if err := fs.Parse(normalized); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *from == "" && *src != "" {
		*from = *src
	}
	if *to == "self" && *dst != "" {
		*to = *dst
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	action, source := firewallDecisionForCLI(router, *from, *to, *proto, *dport)
	fmt.Fprintf(stdout, "action=%s source=%s from=%s to=%s proto=%s dport=%d\n", action, source, *from, *to, *proto, *dport)
	return nil
}

type firewallZoneCLI struct {
	Name string
	Role string
}

func firewallZonesForCLI(router *api.Router) []firewallZoneCLI {
	var out []firewallZoneCLI
	for _, res := range router.Spec.Resources {
		if res.APIVersion == api.FirewallAPIVersion && res.Kind == "FirewallZone" {
			spec, err := res.FirewallZoneSpec()
			if err == nil {
				out = append(out, firewallZoneCLI{Name: res.Metadata.Name, Role: spec.Role})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func firewallDecisionForCLI(router *api.Router, from, to, proto string, dport int) (string, string) {
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.FirewallAPIVersion || res.Kind != "FirewallRule" {
			continue
		}
		spec, err := res.FirewallRuleSpec()
		if err != nil || spec.FromZone != from || spec.ToZone != to {
			continue
		}
		if spec.Protocol != "" && proto != "" && spec.Protocol != proto {
			continue
		}
		if spec.Port != 0 && dport != 0 && spec.Port != dport {
			continue
		}
		return spec.Action, "user/" + res.Metadata.Name
	}
	for _, hole := range render.InternalFirewallHoles(router) {
		if hole.FromZone == from && hole.ToZone == to && (hole.Protocol == "" || proto == "" || hole.Protocol == proto) && (hole.Port == 0 || dport == 0 || hole.Port == dport) {
			return "accept", "internal/" + hole.Name
		}
	}
	roles := map[string]string{}
	for _, zone := range firewallZonesForCLI(router) {
		roles[zone.Name] = zone.Role
	}
	return firewallImplicitActionForCLI(roles[from], roles[to]), "implicit/matrix"
}

func firewallImplicitActionForCLI(fromRole, toRole string) string {
	if toRole == "" || toRole == "self" {
		if fromRole == "mgmt" || fromRole == "trust" {
			return "accept"
		}
		return "drop"
	}
	if fromRole == toRole {
		return "accept"
	}
	if fromRole == "mgmt" {
		return "accept"
	}
	if fromRole == "trust" && toRole != "mgmt" {
		return "accept"
	}
	return "drop"
}
