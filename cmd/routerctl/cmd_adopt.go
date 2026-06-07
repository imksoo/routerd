// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/resource"
)

func adoptCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("adopt", flag.ContinueOnError)
	fs.SetOutput(stdout)
	configPath := fs.String("config", defaultConfigPath(), "config path")
	ledgerPath := fs.String("ledger-file", defaultLedgerPath(), "routerd ownership ledger file")
	candidatesOnly := fs.Bool("candidates", false, "list adoption candidates without changing host state or the ownership ledger")
	applyFlag := fs.Bool("apply", false, "record adoption candidates in the ownership ledger without changing host state")
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"既存 host artifact の adoption candidate を表示または ownership ledger に記録する。",
			"routerctl adopt --candidates --config router.yaml\n"+
				"routerctl adopt --apply --config router.yaml")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *candidatesOnly == *applyFlag {
		return errors.New("adopt requires exactly one of --candidates or --apply")
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	ledger, err := resource.LoadLedger(*ledgerPath)
	if err != nil {
		return err
	}
	defer func() { _ = ledger.Close() }()
	engine := apply.New()
	candidates, artifacts, err := engine.AdoptionCandidateArtifacts(router, ledger)
	if err != nil {
		return err
	}
	result := &apply.Result{
		Generation:         time.Now().Unix(),
		Timestamp:          time.Now().UTC(),
		Phase:              "Healthy",
		AdoptionCandidates: candidates,
	}
	if *applyFlag {
		if drifted := driftedAdoptionCandidates(candidates); len(drifted) > 0 {
			result.Phase = "Blocked"
			result.Warnings = append(result.Warnings, fmt.Sprintf("%d adoption candidates have observed attributes that differ from desired state; apply or update config before adopting", len(drifted)))
			if err := writeJSON(stdout, result); err != nil {
				return err
			}
			return errors.New("adoption blocked by drifted candidates")
		}
		ledger.Remember(artifacts)
		if err := ledger.Save(*ledgerPath); err != nil {
			return err
		}
		result.AdoptedArtifacts = adoptedArtifactsForResult(artifacts)
		result.AdoptionCandidates = nil
	}
	return writeJSON(stdout, result)
}

func driftedAdoptionCandidates(candidates []apply.AdoptionCandidate) []apply.AdoptionCandidate {
	var drifted []apply.AdoptionCandidate
	for _, candidate := range candidates {
		for key, desiredValue := range candidate.Desired {
			if candidate.Observed[key] != desiredValue {
				drifted = append(drifted, candidate)
				break
			}
		}
	}
	return drifted
}

func adoptedArtifactsForResult(artifacts []resource.Artifact) []apply.AdoptedArtifact {
	out := make([]apply.AdoptedArtifact, 0, len(artifacts))
	seen := map[string]bool{}
	for _, artifact := range artifacts {
		if seen[artifact.Identity()] {
			continue
		}
		seen[artifact.Identity()] = true
		out = append(out, apply.AdoptedArtifact{
			Kind:  artifact.Kind,
			Name:  artifact.Name,
			Owner: artifact.Owner,
		})
	}
	return out
}
