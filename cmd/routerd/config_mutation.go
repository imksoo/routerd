// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/eventlog"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type serveConfigMutator struct {
	configPath string
	statePath  string
	baseOpts   applyOptions
	cache      *resultCache
	logger     *eventlog.Logger
	getRouter  func() *api.Router
	setRouter  func(*api.Router)
}

func (m serveConfigMutator) apply(r *http.Request, req controlapi.ApplyRequest) (*controlapi.ApplyResult, error) {
	if strings.TrimSpace(req.CandidateYAML) == "" {
		return nil, fmt.Errorf("%w: apply requires candidateYaml", controlapi.ErrBadRequest)
	}
	nextYAML, nextRouter, err := m.mutatedCandidate(req.CandidateYAML, req.Replace)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
	}
	if req.DryRun {
		result, err := m.planRouter(nextRouter, nextYAML)
		if err != nil {
			return nil, err
		}
		apiResult := controlapi.NewApplyResult(result)
		return &apiResult, nil
	}
	if req.NoReconcile {
		result, err := m.commitOnly(nextRouter, nextYAML)
		if err != nil {
			return nil, err
		}
		m.setRouter(nextRouter)
		apiResult := controlapi.NewApplyResult(result)
		return &apiResult, nil
	}
	result, err := m.reconcile(nextRouter, nextYAML)
	if err != nil {
		return nil, err
	}
	m.setRouter(nextRouter)
	m.cache.Store(result)
	apiResult := controlapi.NewApplyResult(result)
	return &apiResult, nil
}

func (m serveConfigMutator) plan(r *http.Request, req controlapi.PlanRequest) (*controlapi.PlanResult, error) {
	router := m.getRouter()
	configYAML := ""
	if strings.TrimSpace(req.CandidateYAML) != "" {
		var err error
		configYAML, router, err = m.mutatedCandidate(req.CandidateYAML, req.Replace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
		}
	}
	result, err := m.planRouter(router, configYAML)
	if err != nil {
		return nil, err
	}
	apiResult := controlapi.NewPlanResult(result)
	return &apiResult, nil
}

func (m serveConfigMutator) validate(r *http.Request, req controlapi.ValidateRequest) (*controlapi.ValidateResult, error) {
	var router *api.Router
	var err error
	if strings.TrimSpace(req.CandidateYAML) == "" {
		_, router, err = m.currentCanonical()
	} else {
		_, router, err = m.mutatedCandidate(req.CandidateYAML, req.Replace)
	}
	if err != nil {
		result := controlapi.NewValidateResult(false, nil, err.Error())
		return &result, nil
	}
	warnings := config.Warnings(router)
	result := controlapi.NewValidateResult(true, warnings, "")
	return &result, nil
}

func (m serveConfigMutator) delete(r *http.Request, req controlapi.DeleteRequest) (*controlapi.DeleteResult, error) {
	if strings.TrimSpace(req.Target) == "" {
		return nil, controlapi.ErrBadRequest
	}
	target, err := deleteTargetFromArg(req.Target)
	if err != nil {
		if !req.Force {
			return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
		}
		target, err = forceDeleteTargetFromArg(req.Target, m.statePath, req.TargetAPIVersion)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
		}
	}
	currentYAML, _, err := m.currentCanonical()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
	}
	nextYAML, nextRouter, removed, err := config.DeleteResourceYAML([]byte(currentYAML), config.MutationTarget{
		APIVersion: target.APIVersion,
		Kind:       target.Kind,
		Name:       target.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
	}
	if !removed {
		return nil, fmt.Errorf("%w: %s/%s not found in canonical config", controlapi.ErrBadRequest, target.Kind, target.Name)
	}
	result := controlapi.DeleteResult{
		TypeMeta: controlapi.TypeMeta{APIVersion: controlapi.APIVersion, Kind: "DeleteResult"},
		Deleted:  []string{target.APIVersion + "/" + target.Kind + "/" + target.Name},
		DryRun:   req.DryRun,
	}
	if req.DryRun {
		plan, err := m.planRouter(nextRouter, string(nextYAML))
		if err != nil {
			return nil, err
		}
		result.Result = plan
		return &result, nil
	}
	if req.NoReconcile {
		committed, err := m.commitOnly(nextRouter, string(nextYAML))
		if err != nil {
			return nil, err
		}
		m.setRouter(nextRouter)
		result.Result = committed
		return &result, nil
	}
	applied, err := m.reconcile(nextRouter, string(nextYAML))
	if err != nil {
		return nil, err
	}
	m.setRouter(nextRouter)
	m.cache.Store(applied)
	result.Result = applied
	return &result, nil
}

func (m serveConfigMutator) mutatedCandidate(candidateYAML string, replace bool) (string, *api.Router, error) {
	currentYAML, _, err := m.currentCanonical()
	if err != nil {
		if !replace {
			return "", nil, err
		}
		nextYAML, nextRouter, replaceErr := config.UpsertCandidateYAML(nil, []byte(candidateYAML), true)
		if replaceErr != nil {
			return "", nil, replaceErr
		}
		return string(nextYAML), nextRouter, nil
	}
	nextYAML, nextRouter, err := config.UpsertCandidateYAML([]byte(currentYAML), []byte(candidateYAML), replace)
	if err != nil {
		return "", nil, err
	}
	return string(nextYAML), nextRouter, nil
}

func (m serveConfigMutator) currentCanonical() (string, *api.Router, error) {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return "", nil, err
	}
	canonical, router, err := config.CanonicalRouterYAML(data, m.configPath)
	if err != nil {
		return "", nil, err
	}
	return string(canonical), router, nil
}

func (m serveConfigMutator) planRouter(router *api.Router, configYAML string) (*apply.Result, error) {
	if router == nil {
		return nil, errors.New("router is nil")
	}
	opts := m.baseOpts
	opts.DryRun = true
	opts.SkipConfigCommit = true
	opts.StatusFile = ""
	opts.ConfigYAMLOverride = configYAML
	result, err := runApplyChainOnce(context.Background(), router, opts, io.Discard, m.logger)
	if err == nil {
		return result, nil
	}
	if errors.Is(err, routerstate.ErrSchemaNotInitialized) {
		if m.logger != nil {
			m.logger.Emit(eventlog.LevelError, "plan", "routerd plan could not read state database", map[string]string{
				"error":     err.Error(),
				"statePath": m.statePath,
			})
		}
		return nil, errors.New("routerd state database is not initialized; restart routerd serve and verify its --state path")
	}
	return nil, err
}

func (m serveConfigMutator) reconcile(router *api.Router, configYAML string) (*apply.Result, error) {
	if m.baseOpts.Sandbox {
		committed, err := m.commitOnly(router, configYAML)
		if err != nil {
			return nil, err
		}
		result, err := m.planRouter(router, configYAML)
		if err != nil {
			return nil, err
		}
		result.Generation = committed.Generation
		return result, nil
	}
	opts := m.baseOpts
	opts.DryRun = false
	opts.SkipConfigCommit = false
	opts.ConfigYAMLOverride = configYAML
	return runApplyChainOnce(context.Background(), router, opts, io.Discard, m.logger)
}

func (m serveConfigMutator) commitOnly(router *api.Router, configYAML string) (*apply.Result, error) {
	store, err := routerstate.OpenSQLite(m.statePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	generation, err := store.BeginGeneration(routerConfigHash(router))
	if err != nil {
		return nil, err
	}
	finished := false
	defer func() {
		if !finished {
			_ = store.FinishGeneration(generation, "Errored", nil)
		}
	}()
	if err := store.RecordGenerationConfig(generation, configYAML); err != nil {
		return nil, err
	}
	if err := config.AtomicWriteFile(m.configPath, []byte(configYAML)); err != nil {
		return nil, err
	}
	if err := recordLastAppliedPath(router, store, m.configPath); err != nil {
		return nil, err
	}
	if err := store.FinishGeneration(generation, "Committed", nil); err != nil {
		return nil, err
	}
	finished = true
	if m.logger != nil {
		m.logger.Emit(eventlog.LevelInfo, "apply", "committed canonical router config without reconcile", map[string]string{"config": m.configPath})
	}
	return &apply.Result{
		Generation: generation,
		Timestamp:  time.Now().UTC(),
		Phase:      "Committed",
	}, nil
}
