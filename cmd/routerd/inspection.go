// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/hostdeps"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func serveGetHandler(currentRouter func() *api.Router, store *routerstate.SQLiteStore, status func(*http.Request) (*controlapi.Status, error), controllers func(*http.Request) (*controlapi.Controllers, error), runtimeStats func(*http.Request) (*controlapi.RuntimeStats, error)) func(*http.Request, controlapi.GetRequest) (*controlapi.GetResult, error) {
	return func(r *http.Request, req controlapi.GetRequest) (*controlapi.GetResult, error) {
		subject := strings.TrimSpace(req.Subject)
		if subject == "" {
			subject = "resources"
		}
		switch canonicalInspectionSubject(subject) {
		case "status":
			value, err := status(r)
			if err != nil {
				return nil, err
			}
			result := controlapi.NewGetResult("status")
			result.Status = &value.Status
			result.Raw = value
			return &result, nil
		case "controllers":
			value, err := controllers(r)
			if err != nil {
				return nil, err
			}
			result := controlapi.NewGetResult("controllers")
			result.Raw = value
			return &result, nil
		case "runtime":
			value, err := runtimeStats(r)
			if err != nil {
				return nil, err
			}
			result := controlapi.NewGetResult("runtime")
			result.Raw = value
			return &result, nil
		case "events":
			events, err := serveInspectionEvents(store, req)
			if err != nil {
				return nil, err
			}
			result := controlapi.NewGetResult("events")
			result.Events = events
			return &result, nil
		case "ledger":
			report, err := serveInspectionLedger(store, req.Limit)
			if err != nil {
				return nil, err
			}
			result := controlapi.NewGetResult("ledger")
			result.Ledger = &report
			return &result, nil
		default:
			resources, err := serveInspectionResources(currentRouter(), store, subject, req.EventsLimit)
			if err != nil {
				return nil, err
			}
			result := controlapi.NewGetResult(subject)
			result.Items = resources
			return &result, nil
		}
	}
}

func serveDescribeHandler(currentRouter func() *api.Router, store *routerstate.SQLiteStore) func(*http.Request, controlapi.DescribeRequest) (*controlapi.DescribeResult, error) {
	return func(r *http.Request, req controlapi.DescribeRequest) (*controlapi.DescribeResult, error) {
		target := strings.TrimSpace(req.Target)
		if target == "" {
			return nil, fmt.Errorf("%w: describe requires target", controlapi.ErrBadRequest)
		}
		resources, err := serveInspectionResources(currentRouter(), store, target, req.EventsLimit)
		if err != nil {
			return nil, err
		}
		if len(resources) != 1 {
			return nil, fmt.Errorf("%w: describe expected one resource, got %d", controlapi.ErrBadRequest, len(resources))
		}
		result := controlapi.NewDescribeResult(target, resources[0])
		return &result, nil
	}
}

func serveProbeHandler(currentRouter func() *api.Router, store *routerstate.SQLiteStore) func(*http.Request, controlapi.ProbeRequest) (*controlapi.ProbeResult, error) {
	return func(r *http.Request, req controlapi.ProbeRequest) (*controlapi.ProbeResult, error) {
		subject := strings.TrimSpace(req.Subject)
		if subject == "" {
			return nil, fmt.Errorf("%w: doctor --probe requires subject", controlapi.ErrBadRequest)
		}
		checks := serveProbeChecks(currentRouter(), store, subject, strings.TrimSpace(req.Target))
		result := controlapi.NewProbeResult(subject, req.Target, checks)
		return &result, nil
	}
}

func serveInspectionResources(router *api.Router, store *routerstate.SQLiteStore, target string, eventsLimit int) ([]controlapi.ResourceView, error) {
	if router == nil {
		return nil, fmt.Errorf("%w: router config unavailable", controlapi.ErrBadRequest)
	}
	kind, name, err := parseInspectionTarget(target)
	if err != nil {
		return nil, err
	}
	selected := selectInspectionResources(inspectionResources(router), kind, name)
	if len(selected) == 0 {
		return nil, fmt.Errorf("%w: %s not found", controlapi.ErrBadRequest, target)
	}
	var out []controlapi.ResourceView
	for _, res := range selected {
		status := map[string]any{}
		if store != nil {
			status = store.ObjectStatus(res.APIVersion, res.Kind, res.Metadata.Name)
		}
		out = append(out, controlapi.ResourceViewFromResource(res, status, inspectionEventsForResource(store, res, eventsLimit)))
	}
	return out, nil
}

func parseInspectionTarget(target string) (string, string, error) {
	target = strings.TrimSpace(target)
	if target == "" || target == "resources" || target == "all" {
		return "", "", nil
	}
	kind, name, _ := strings.Cut(target, "/")
	if strings.TrimSpace(kind) == "" {
		return "", "", fmt.Errorf("%w: empty resource kind", controlapi.ErrBadRequest)
	}
	if strings.Contains(target, "/") && strings.TrimSpace(name) == "" {
		return "", "", fmt.Errorf("%w: target %q has empty name", controlapi.ErrBadRequest, target)
	}
	return kind, name, nil
}

func selectInspectionResources(resources []api.Resource, kind, name string) []api.Resource {
	var out []api.Resource
	for _, res := range resources {
		if kind != "" && !strings.EqualFold(res.Kind, kind) {
			continue
		}
		if name != "" && res.Metadata.Name != name {
			continue
		}
		out = append(out, res)
	}
	return out
}

func inspectionResources(router *api.Router) []api.Resource {
	if router == nil {
		return nil
	}
	resources := append([]api.Resource(nil), router.Spec.Resources...)
	resources = appendMissingInspectionResources(resources, hostdeps.DerivedPackageResources(router)...)
	return resources
}

func appendMissingInspectionResources(resources []api.Resource, additions ...api.Resource) []api.Resource {
	seen := map[string]bool{}
	for _, res := range resources {
		seen[res.APIVersion+"/"+res.Kind+"/"+res.Metadata.Name] = true
	}
	for _, res := range additions {
		key := res.APIVersion + "/" + res.Kind + "/" + res.Metadata.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		resources = append(resources, res)
	}
	return resources
}

func inspectionEventsForResource(store *routerstate.SQLiteStore, res api.Resource, limit int) []routerstate.Event {
	if store == nil || limit == 0 {
		return []routerstate.Event{}
	}
	if limit < 0 {
		limit = 10
	}
	return store.Events(res.APIVersion, res.Kind, res.Metadata.Name, limit)
}

func serveInspectionEvents(store *routerstate.SQLiteStore, req controlapi.GetRequest) ([]routerstate.StoredEvent, error) {
	if store == nil {
		return []routerstate.StoredEvent{}, nil
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	return store.ListEvents(routerstate.EventQuery{
		Limit:    limit,
		SinceID:  req.SinceID,
		Topic:    req.Topic,
		Kind:     req.KindFilter,
		Name:     req.NameFilter,
		Resource: req.Resource,
	})
}

func serveInspectionLedger(store *routerstate.SQLiteStore, limit int) (controlapi.LedgerReport, error) {
	if store == nil {
		return controlapi.LedgerReport{}, nil
	}
	integrity, err := store.IntegrityCheck()
	if err != nil {
		return controlapi.LedgerReport{}, err
	}
	if limit <= 0 {
		limit = 20
	}
	generations, err := store.ListGenerations(limit)
	if err != nil {
		return controlapi.LedgerReport{}, err
	}
	return controlapi.LedgerReport{Integrity: integrity, Generations: generations}, nil
}

func serveProbeChecks(router *api.Router, store *routerstate.SQLiteStore, subject, target string) []controlapi.ProbeCheck {
	switch canonicalInspectionSubject(subject) {
	case "egress":
		return resourcePhaseProbe(router, store, "EgressRoutePolicy", target)
	case "dns":
		return resourcePhaseProbe(router, store, "DNSResolver", target)
	case "lan-client":
		if target == "" {
			return []controlapi.ProbeCheck{{Name: "target", Status: "fail", Detail: "lan-client probe requires target IP"}}
		}
		return []controlapi.ProbeCheck{{Name: "target", Status: "pass", Detail: target}}
	default:
		return []controlapi.ProbeCheck{{Name: "subject", Status: "fail", Detail: "unknown probe subject " + subject}}
	}
}

func resourcePhaseProbe(router *api.Router, store *routerstate.SQLiteStore, kind, name string) []controlapi.ProbeCheck {
	if router == nil {
		return []controlapi.ProbeCheck{{Name: kind, Status: "fail", Detail: "router config unavailable"}}
	}
	resources := selectInspectionResources(inspectionResources(router), kind, name)
	if len(resources) == 0 {
		return []controlapi.ProbeCheck{{Name: kind, Status: "fail", Detail: "resource not found"}}
	}
	var out []controlapi.ProbeCheck
	for _, res := range resources {
		status := map[string]any{}
		if store != nil {
			status = store.ObjectStatus(res.APIVersion, res.Kind, res.Metadata.Name)
		}
		phase, _ := status["phase"].(string)
		checkStatus := "warn"
		if phase == "" || phase == "Healthy" || phase == "Applied" || phase == "Active" || phase == "Ready" {
			checkStatus = "pass"
		}
		detail := phase
		if detail == "" {
			detail = "declared"
		}
		out = append(out, controlapi.ProbeCheck{Name: res.Kind + "/" + res.Metadata.Name, Status: checkStatus, Detail: detail})
	}
	return out
}

func canonicalInspectionSubject(subject string) string {
	key := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(subject), "_", "-"), " ", "-"))
	switch key {
	case "", "resources", "resource":
		return "resources"
	case "event":
		return "events"
	case "generation", "generations":
		return "ledger"
	case "controller":
		return "controllers"
	case "rt":
		return "runtime"
	default:
		return key
	}
}
