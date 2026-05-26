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
	"github.com/imksoo/routerd/pkg/eventlog"
	"github.com/imksoo/routerd/pkg/resource"
)

func configCommand(args []string, stdout io.Writer, name string) (err error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	statusFile := fs.String("status-file", defaultStatusFile(), "status file")
	statePath := fs.String("state-file", defaultStatePath, "routerd state database file")
	_ = fs.Bool("diff", false, "include planned artifact differences")
	if err := fs.Parse(args); err != nil {
		return err
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	logger, err := eventlog.New(router)
	if err != nil {
		return err
	}
	defer closeLogger(logger, name, &err)
	logger.Emit(eventlog.LevelInfo, name, "routerd command started", map[string]string{"config": *configPath})
	engine := apply.New()
	stateStore, err := loadTransientStateStore(*statePath)
	if err != nil {
		return err
	}
	_, err = recordObservedPrefixDelegationState(router, stateStore)
	if err != nil {
		return err
	}
	effectiveRouter := filterRouterByWhen(router, stateStore)
	configWarnings := config.Warnings(router)
	switch name {
	case "observe":
		result, err := engine.Observe(effectiveRouter)
		if err != nil {
			return err
		}
		result.Warnings = append(result.Warnings, configWarnings...)
		appendPrefixDelegationStateWarnings(result, router, stateStore)
		return writeResult(stdout, *statusFile, result)
	case "plan":
		result, err := engine.Plan(effectiveRouter)
		if err != nil {
			return err
		}
		result.Warnings = append(result.Warnings, configWarnings...)
		appendPrefixDelegationStateWarnings(result, router, stateStore)
		return writeResult(stdout, *statusFile, result)
	case "run":
		return errors.New("run is not implemented yet")
	default:
		return fmt.Errorf("unknown config command %s", name)
	}
}

func adoptCommand(args []string, stdout io.Writer) (err error) {
	fs := flag.NewFlagSet("adopt", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	statusFile := fs.String("status-file", defaultStatusFile(), "status file")
	ledgerPath := fs.String("ledger-file", defaultLedgerPath, "routerd ownership ledger file")
	candidatesOnly := fs.Bool("candidates", false, "list adoption candidates without changing host state or the ownership ledger")
	applyFlag := fs.Bool("apply", false, "record adoption candidates in the ownership ledger without changing host state")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *candidatesOnly == *applyFlag {
		return errors.New("adopt requires exactly one of --candidates or --apply")
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	logger, err := eventlog.New(router)
	if err != nil {
		return err
	}
	defer closeLogger(logger, "adopt", &err)
	logger.Emit(eventlog.LevelInfo, "adopt", "routerd command started", map[string]string{"config": *configPath})
	ledger, err := resource.LoadLedger(*ledgerPath)
	if err != nil {
		return err
	}
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
			if err := writeResult(stdout, *statusFile, result); err != nil {
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
	return writeResult(stdout, *statusFile, result)
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
