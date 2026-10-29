// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/resourcequery"
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
				"from / to / proto / dport から静的な nftables new-flow 判定を表示する。\n"+
				"実行中のルールや必要な観測を確認できない場合は indeterminate を返す。\n"+
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
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected firewall test argument %q", fs.Arg(0))
	}
	decision, err := firewallDecisionForCLI(router, render.FirewallNewFlowQuery{FromZone: *from, ToZone: *to, Protocol: strings.ToLower(strings.TrimSpace(*proto)), DestinationPort: *dport})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "action=%s source=%s from=%s to=%s proto=%s dport=%d scope=static-nftables-new-flow", decision.Action, decision.Source, *from, *to, *proto, *dport)
	if err != nil {
		return err
	}
	if decision.Reason != "" {
		if _, err := fmt.Fprintf(stdout, " reason=%q", decision.Reason); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(stdout)
	return err
}

func firewallDecisionForCLI(router *api.Router, query render.FirewallNewFlowQuery) (render.FirewallDecision, error) {
	for _, resource := range router.Spec.Resources {
		if resourcequery.ResourceWhenPresent(resourcequery.ResourceWhen(resource)) {
			return render.FirewallDecision{Action: "indeterminate", Source: resource.ID(), Reason: "current resource when-state is unavailable"}, nil
		}
		switch resource.Kind {
		case "Plugin", "DynamicConfigSource", "EventSubscription", "MobilityPool", "SAMTransportProfile", "SAMPeerGroup", "SAMEnrollmentClient":
			return render.FirewallDecision{Action: "indeterminate", Source: resource.ID(), Reason: "current effective dynamic configuration is unavailable"}, nil
		}
	}
	return render.NftablesNewFlowDecision(router, query)
}
