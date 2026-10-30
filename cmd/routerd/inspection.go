// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/resourcequery"
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
			resources, err := serveInspectionResources(r.Context(), currentRouter(), store, subject, req.EventsLimit)
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
		resources, err := serveInspectionResources(r.Context(), currentRouter(), store, target, req.EventsLimit)
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
	slots := make(chan struct{}, 4)
	return func(r *http.Request, req controlapi.ProbeRequest) (*controlapi.ProbeResult, error) {
		subject := strings.TrimSpace(req.Subject)
		if subject == "" {
			return nil, fmt.Errorf("%w: doctor --probe requires subject", controlapi.ErrBadRequest)
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			result := controlapi.NewProbeResult(subject, req.Target, []controlapi.ProbeCheck{{Name: "probe admission", Status: "warn", Kind: "scope", Detail: "unobserved: four active probe requests already own the available slots"}})
			return &result, nil
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		checks, err := serveProbeChecks(ctx, currentRouter(), store, subject, strings.TrimSpace(req.Target))
		if err != nil {
			return nil, err
		}
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		if len(checks) > 32 {
			checks = append(checks[:31], unobservedProbe("remaining checks", "response check limit reached; remaining checks are unprobed"))
		}
		result := controlapi.NewProbeResult(subject, req.Target, checks)
		return &result, nil
	}
}

func serveInspectionResources(ctx context.Context, router *api.Router, store *routerstate.SQLiteStore, target string, eventsLimit int) ([]controlapi.ResourceView, error) {
	if router == nil {
		return nil, fmt.Errorf("%w: router config unavailable", controlapi.ErrBadRequest)
	}
	kind, name, err := parseInspectionTarget(target)
	if err != nil {
		return nil, err
	}
	selected := selectInspectionResources(router.Spec.Resources, kind, name)
	if len(selected) == 0 {
		return nil, fmt.Errorf("%w: %s not found", controlapi.ErrBadRequest, target)
	}
	var out []controlapi.ResourceView
	for _, res := range selected {
		status := map[string]any{}
		if store != nil {
			status, err = store.ReadObjectStatus(res.APIVersion, res.Kind, res.Metadata.Name)
			if err != nil {
				return nil, err
			}
		}
		events, err := inspectionEventsForResource(ctx, store, res, eventsLimit)
		if err != nil {
			return nil, err
		}
		view := controlapi.ResourceViewFromResource(res, status, events)
		if store != nil && res.Kind == "DHCPv6PrefixDelegation" {
			lease, found, err := store.ReadPDLease(res.Metadata.Name)
			if err != nil {
				return nil, err
			}
			if found {
				lease = lease.Live(store.Now())
				view.PDLease = &lease
				delete(view.Status, "observation")
				for key := range routerstate.PDLeaseStatus(lease, store.Now()) {
					if key != "phase" && key != "health" && key != "conditions" {
						delete(view.Status, key)
					}
				}
			}
		}
		out = append(out, view)
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

func inspectionEventsForResource(ctx context.Context, store *routerstate.SQLiteStore, res api.Resource, limit int) ([]routerstate.Event, error) {
	if store == nil || limit == 0 {
		return []routerstate.Event{}, nil
	}
	if limit < 0 {
		limit = 10
	}
	return store.ReadEvents(ctx, res.APIVersion, res.Kind, res.Metadata.Name, limit)
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

func serveProbeChecks(ctx context.Context, router *api.Router, store *routerstate.SQLiteStore, subject, target string) ([]controlapi.ProbeCheck, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	area := canonicalInspectionSubject(subject)
	if router != nil && (area == "dns" || area == "egress") {
		var source resourcequery.StateStore
		if store != nil {
			source = store
		} else {
			for _, resource := range router.Spec.Resources {
				if resourcequery.ResourceWhenPresent(resourcequery.ResourceWhen(resource)) {
					return []controlapi.ProbeCheck{unobservedProbe("current probe graph", "conditional activation observations are unavailable")}, nil
				}
			}
		}
		effective, err := resourcequery.FilterRouterByResolvedWhen(router, source)
		if err != nil {
			return nil, err
		}
		kind := "DNSResolver"
		if area == "egress" {
			kind = "EgressRoutePolicy"
		}
		declared, _ := selectProbeResources(router.Spec.Resources, kind, target)
		active, _ := selectProbeResources(effective.Spec.Resources, kind, target)
		if len(declared) > 0 && len(active) == 0 {
			return []controlapi.ProbeCheck{unobservedProbe("current probe graph", "selected resources are not currently active with resolved conditions")}, nil
		}
		router = effective
	}
	switch area {
	case "egress":
		return daemonEgressProbe(ctx, router, store, target)
	case "dns":
		return daemonDNSProbe(ctx, router, store, target)
	case "lan-client":
		return daemonLANClientProbe(ctx, router, store, target)
	default:
		return []controlapi.ProbeCheck{{Name: "subject", Status: "fail", Kind: "scope", Detail: "unknown probe subject " + subject}}, nil
	}
}

func resourcePhaseProbe(router *api.Router, store *routerstate.SQLiteStore, kind, name string) ([]controlapi.ProbeCheck, error) {
	if router == nil {
		return []controlapi.ProbeCheck{{Name: kind, Status: "warn", Kind: "scope", Detail: "unobserved: router config unavailable"}}, nil
	}
	resources, more := selectProbeResources(router.Spec.Resources, kind, name)
	if len(resources) == 0 {
		return []controlapi.ProbeCheck{{Name: kind, Status: "fail", Detail: "resource not found"}}, nil
	}
	var out []controlapi.ProbeCheck
	for _, res := range resources {
		var status map[string]any
		if store != nil {
			var err error
			status, err = store.ReadObjectStatus(res.APIVersion, res.Kind, res.Metadata.Name)
			if err != nil {
				return nil, err
			}
		}
		phase, _ := status["phase"].(string)
		checkStatus := "warn"
		switch phase {
		case "Healthy", "Applied", "Active", "Ready":
			checkStatus = "pass"
		case "Error", "Failed", "Errored", "Blocked", "Drifted":
			checkStatus = "fail"
		}
		detail := "reported phase " + phase
		if phase == "" {
			detail = "unobserved: resource is declared without an observed phase"
		}
		out = append(out, controlapi.ProbeCheck{Name: res.Kind + "/" + res.Metadata.Name + " status", Status: checkStatus, Detail: detail, Kind: "reported-status"})
	}
	if more {
		out = append(out, unobservedProbe("remaining resources", "resource limit reached; remaining resources are unprobed"))
	}
	return out, nil
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

// Selection stops at the probe response's resource budget. Ordinary get/describe
// selection remains unrestricted by the separate active-probe budget.
func selectProbeResources(resources []api.Resource, kind, name string) ([]api.Resource, bool) {
	var selected []api.Resource
	for _, resource := range resources {
		if !strings.EqualFold(resource.Kind, kind) || name != "" && resource.Metadata.Name != name {
			continue
		}
		if len(selected) == 8 {
			return selected, true
		}
		selected = append(selected, resource)
	}
	return selected, false
}
