// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	defaultSAMEnrollmentRefreshBefore = 10 * time.Minute
	defaultSAMEnrollmentBackoffMin    = 10 * time.Second
	defaultSAMEnrollmentBackoffMax    = 15 * time.Minute
)

type SAMEnrollmentClientStore interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
	UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord) error
	GetDynamicConfigPartsBySource(source string) ([]routerstate.DynamicConfigPartRecord, error)
}

type SAMEnrollmentJoinClient interface {
	SubmitSAMEnrollmentClaim(context.Context, controlapi.SAMEnrollmentClaimSubmitRequest) (*controlapi.SAMEnrollmentClaimSubmitResult, error)
	GetSAMRRSet(context.Context, controlapi.SAMRRSetGetRequest) (*controlapi.SAMRRSetGetResult, error)
}

type SAMEnrollmentClientController struct {
	Router        *api.Router
	Store         SAMEnrollmentClientStore
	Now           func() time.Time
	ClientFactory func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient
}

func (c SAMEnrollmentClientController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	now := samEnrollmentClientNow(c.Now)
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClient" {
			continue
		}
		spec, err := resource.SAMEnrollmentClientSpec()
		if err != nil {
			return err
		}
		if err := c.reconcileOne(ctx, resource, spec, now); err != nil {
			return err
		}
	}
	return nil
}

func (c SAMEnrollmentClientController) reconcileOne(ctx context.Context, owner api.Resource, spec api.SAMEnrollmentClientSpec, now time.Time) error {
	status := c.Store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", owner.Metadata.Name)
	claimResource, claim, err := samEnrollmentClientClaim(c.Router, spec.ClaimRef)
	if err != nil {
		return c.saveSAMEnrollmentClientStatus(owner.Metadata.Name, samEnrollmentClientStatus{
			Phase:    "Degraded",
			ClaimRef: spec.ClaimRef,
			Reason:   err.Error(),
		}, now)
	}
	rrSetName, err := samEnrollmentClientRRSetName(claim.RRSetRef)
	if err != nil {
		return c.saveSAMEnrollmentClientStatus(owner.Metadata.Name, samEnrollmentClientStatus{
			Phase:    "Degraded",
			ClaimRef: spec.ClaimRef,
			Reason:   err.Error(),
		}, now)
	}
	claimDigest := samEnrollmentClientClaimDigest(claimResource)
	source := "SAMRRSet/" + rrSetName
	rrState, err := c.fetchedRRSetState(source, now)
	if err != nil {
		return err
	}
	refreshBefore := samEnrollmentClientDurationDefault(spec.StateTTLRefreshBefore, defaultSAMEnrollmentRefreshBefore)
	reason := samEnrollmentClientRefreshReason(rrState, claimDigest, samEnrollmentStatusString(status, "claimDigest"), refreshBefore, now)
	if reason == "" {
		next := rrState.ExpiresAt.Add(-refreshBefore)
		if next.Before(now) {
			next = now.Add(refreshBefore)
		}
		return c.saveSAMEnrollmentClientStatus(owner.Metadata.Name, samEnrollmentClientStatus{
			Phase:         "Ready",
			ClaimRef:      spec.ClaimRef,
			ObservedRRSet: source,
			LastAttempt:   parseSAMEnrollmentClientTime(samEnrollmentStatusString(status, "lastAttempt")),
			LastSuccess:   parseSAMEnrollmentClientTime(samEnrollmentStatusString(status, "lastSuccess")),
			NextAttempt:   next,
			Backoff:       "",
			FailureCount:  0,
			ClaimDigest:   claimDigest,
			Reason:        "rrset-current",
		}, now)
	}
	nextAttempt := parseSAMEnrollmentClientTime(samEnrollmentStatusString(status, "nextAttempt"))
	if !nextAttempt.IsZero() && now.Before(nextAttempt) {
		return c.saveSAMEnrollmentClientStatus(owner.Metadata.Name, samEnrollmentClientStatus{
			Phase:         "Backoff",
			ClaimRef:      spec.ClaimRef,
			ObservedRRSet: sourceIf(rrState.Found, source),
			LastAttempt:   parseSAMEnrollmentClientTime(samEnrollmentStatusString(status, "lastAttempt")),
			LastSuccess:   parseSAMEnrollmentClientTime(samEnrollmentStatusString(status, "lastSuccess")),
			NextAttempt:   nextAttempt,
			Backoff:       samEnrollmentStatusString(status, "backoff"),
			FailureCount:  samEnrollmentStatusInt(status, "failureCount"),
			ClaimDigest:   claimDigest,
			Reason:        reason,
		}, now)
	}
	err = c.joinFetchAndPersist(ctx, spec, claimResource, rrSetName, now)
	if err != nil {
		failures := samEnrollmentStatusInt(status, "failureCount") + 1
		backoff := samEnrollmentClientBackoff(spec, failures)
		return c.saveSAMEnrollmentClientStatus(owner.Metadata.Name, samEnrollmentClientStatus{
			Phase:         "Degraded",
			ClaimRef:      spec.ClaimRef,
			ObservedRRSet: sourceIf(rrState.Found, source),
			LastAttempt:   now,
			LastSuccess:   parseSAMEnrollmentClientTime(samEnrollmentStatusString(status, "lastSuccess")),
			NextAttempt:   now.Add(backoff),
			Backoff:       backoff.String(),
			FailureCount:  failures,
			ClaimDigest:   claimDigest,
			Reason:        err.Error(),
		}, now)
	}
	records, err := c.Store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return err
	}
	refreshed := latestActiveSAMEnrollmentRRSet(records, now)
	return c.saveSAMEnrollmentClientStatus(owner.Metadata.Name, samEnrollmentClientStatus{
		Phase:         "Ready",
		ClaimRef:      spec.ClaimRef,
		ObservedRRSet: source,
		LastAttempt:   now,
		LastSuccess:   now,
		NextAttempt:   refreshed.ExpiresAt.Add(-refreshBefore),
		Backoff:       "",
		FailureCount:  0,
		ClaimDigest:   claimDigest,
		Reason:        reason,
	}, now)
}

func (c SAMEnrollmentClientController) joinFetchAndPersist(ctx context.Context, spec api.SAMEnrollmentClientSpec, claim api.Resource, rrSetName string, now time.Time) error {
	var lastErr error
	var submitted []struct {
		client SAMEnrollmentJoinClient
		result *controlapi.SAMEnrollmentClaimSubmitResult
	}
	for _, client := range c.clients(spec) {
		submit, err := client.SubmitSAMEnrollmentClaim(ctx, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim})
		if err != nil {
			lastErr = err
			continue
		}
		submitted = append(submitted, struct {
			client SAMEnrollmentJoinClient
			result *controlapi.SAMEnrollmentClaimSubmitResult
		}{client: client, result: submit})
	}
	for _, item := range submitted {
		rrSet, err := item.client.GetSAMRRSet(ctx, controlapi.SAMRRSetGetRequest{Name: rrSetName, ClaimRef: "SAMEnrollmentClaim/" + claim.Metadata.Name})
		if err != nil {
			lastErr = err
			continue
		}
		expiresAt := item.result.ExpiresAt
		if expiresAt.IsZero() {
			expiresAt = now.Add(DefaultLeaseTTL)
		}
		record, err := samEnrollmentClientRRSetRecord(rrSet.RRSet, item.result.ObservedAt, expiresAt)
		if err != nil {
			lastErr = err
			continue
		}
		return c.Store.UpsertDynamicConfigPart(record)
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no SAM enrollment bootstrap endpoint configured")
}

func (c SAMEnrollmentClientController) clients(spec api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
	if c.ClientFactory != nil {
		return c.ClientFactory(spec)
	}
	var out []SAMEnrollmentJoinClient
	if socket := strings.TrimSpace(spec.RRSocket); socket != "" {
		out = append(out, controlapi.NewUnixClient(socket))
	}
	for _, endpoint := range spec.BootstrapEndpoints {
		if endpoint = strings.TrimSpace(endpoint); endpoint != "" {
			out = append(out, controlapi.NewHTTPClient(endpoint))
		}
	}
	return out
}

type samEnrollmentClientStatus struct {
	Phase         string
	ClaimRef      string
	ObservedRRSet string
	LastAttempt   time.Time
	LastSuccess   time.Time
	NextAttempt   time.Time
	Backoff       string
	FailureCount  int
	ClaimDigest   string
	Reason        string
}

func (c SAMEnrollmentClientController) saveSAMEnrollmentClientStatus(name string, status samEnrollmentClientStatus, now time.Time) error {
	out := map[string]any{
		"phase":        firstNonEmpty(status.Phase, "Unknown"),
		"claimRef":     status.ClaimRef,
		"failureCount": status.FailureCount,
		"claimDigest":  status.ClaimDigest,
		"updatedAt":    now.UTC().Format(time.RFC3339),
		"conditions": []map[string]any{{
			"type":   "Ready",
			"status": status.Phase == "Ready",
			"reason": status.Reason,
		}},
	}
	if status.ObservedRRSet != "" {
		out["observedRRSet"] = status.ObservedRRSet
	}
	if !status.LastAttempt.IsZero() {
		out["lastAttempt"] = status.LastAttempt.UTC().Format(time.RFC3339)
	}
	if !status.LastSuccess.IsZero() {
		out["lastSuccess"] = status.LastSuccess.UTC().Format(time.RFC3339)
	}
	if !status.NextAttempt.IsZero() {
		out["nextAttempt"] = status.NextAttempt.UTC().Format(time.RFC3339)
	}
	if status.Backoff != "" {
		out["backoff"] = status.Backoff
	}
	if status.Reason != "" {
		out["reason"] = status.Reason
	}
	return c.Store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", name, out)
}

type samEnrollmentRRSetState struct {
	Found      bool
	ObservedAt time.Time
	ExpiresAt  time.Time
	Digest     string
}

func (c SAMEnrollmentClientController) fetchedRRSetState(source string, now time.Time) (samEnrollmentRRSetState, error) {
	records, err := c.Store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return samEnrollmentRRSetState{}, err
	}
	return latestActiveSAMEnrollmentRRSet(records, now), nil
}

func latestActiveSAMEnrollmentRRSet(records []routerstate.DynamicConfigPartRecord, now time.Time) samEnrollmentRRSetState {
	var out samEnrollmentRRSetState
	for _, record := range records {
		if record.EffectiveStatus(now) != "active" {
			continue
		}
		if !out.Found || record.ObservedAt.After(out.ObservedAt) {
			out = samEnrollmentRRSetState{Found: true, ObservedAt: record.ObservedAt, ExpiresAt: record.ExpiresAt, Digest: record.Digest}
		}
	}
	return out
}

func samEnrollmentClientRefreshReason(rrState samEnrollmentRRSetState, claimDigest, previousClaimDigest string, refreshBefore time.Duration, now time.Time) string {
	if !rrState.Found {
		return "rrset-missing"
	}
	if previousClaimDigest != "" && previousClaimDigest != claimDigest {
		return "claim-changed"
	}
	if rrState.ExpiresAt.IsZero() || !now.Add(refreshBefore).Before(rrState.ExpiresAt) {
		return "rrset-expiring"
	}
	return ""
}

func samEnrollmentClientRRSetRecord(resource api.Resource, observedAt, expiresAt time.Time) (routerstate.DynamicConfigPartRecord, error) {
	if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMRRSet" || strings.TrimSpace(resource.Metadata.Name) == "" {
		return routerstate.DynamicConfigPartRecord{}, fmt.Errorf("fetched resource must be %s/SAMRRSet", api.MobilityAPIVersion)
	}
	if _, err := resource.SAMRRSetSpec(); err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	if expiresAt.IsZero() {
		expiresAt = observedAt.Add(DefaultLeaseTTL)
	}
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{
			Name: safeName("fetched-sam-rrset-" + resource.Metadata.Name),
			OwnerRefs: []api.OwnerRef{{
				APIVersion: api.MobilityAPIVersion,
				Kind:       "SAMRRSet",
				Name:       resource.Metadata.Name,
			}},
		},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:      "SAMRRSet/" + resource.Metadata.Name,
			Generation:  dynamicGeneration,
			ObservedAt:  observedAt.UTC(),
			ExpiresAt:   expiresAt.UTC(),
			Resources:   []api.Resource{resource},
			Directives:  []dynamicconfig.DynamicConfigDirective{},
			ActionPlans: []dynamicconfig.ActionPlan{},
		},
	}
	part.Spec.Digest = digestDynamicPart(part)
	return dynamicPartRecord(part)
}

func samEnrollmentClientClaim(router *api.Router, ref string) (api.Resource, api.SAMEnrollmentClaimSpec, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "SAMEnrollmentClaim" || strings.TrimSpace(name) == "" {
		return api.Resource{}, api.SAMEnrollmentClaimSpec{}, fmt.Errorf("claimRef must reference SAMEnrollmentClaim/<name>")
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClaim" || resource.Metadata.Name != strings.TrimSpace(name) {
			continue
		}
		spec, err := resource.SAMEnrollmentClaimSpec()
		if err != nil {
			return api.Resource{}, api.SAMEnrollmentClaimSpec{}, err
		}
		return resource, spec, nil
	}
	return api.Resource{}, api.SAMEnrollmentClaimSpec{}, fmt.Errorf("%s not found", ref)
}

func samEnrollmentClientRRSetName(ref string) (string, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "SAMRRSet" || strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("claim rrSetRef must reference SAMRRSet/<name>")
	}
	return strings.TrimSpace(name), nil
}

func samEnrollmentClientClaimDigest(resource api.Resource) string {
	data, _ := json.Marshal(resource.Spec)
	return digestBytes(data)
}

func digestBytes(data []byte) string {
	sum := digestSHA256(data)
	return "sha256:" + sum
}

func digestSHA256(data []byte) string {
	// Kept separate for tests and to avoid exporting planner digest helpers.
	return fmt.Sprintf("%x", sha256Bytes(data))
}

func sha256Bytes(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func samEnrollmentClientBackoff(spec api.SAMEnrollmentClientSpec, failures int) time.Duration {
	minBackoff := samEnrollmentClientDurationDefault(spec.RetryBackoff.Min, defaultSAMEnrollmentBackoffMin)
	maxBackoff := samEnrollmentClientDurationDefault(spec.RetryBackoff.Max, defaultSAMEnrollmentBackoffMax)
	if failures < 1 {
		failures = 1
	}
	multiplier := math.Pow(2, float64(failures-1))
	backoff := time.Duration(float64(minBackoff) * multiplier)
	if backoff > maxBackoff || backoff < 0 {
		backoff = maxBackoff
	}
	return backoff
}

func samEnrollmentClientDurationDefault(value string, fallback time.Duration) time.Duration {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}

func samEnrollmentClientNow(now func() time.Time) time.Time {
	if now == nil {
		return time.Now().UTC()
	}
	return now().UTC()
}

func parseSAMEnrollmentClientTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value)); err == nil {
		return parsed.UTC()
	}
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value)); err == nil {
		return parsed.UTC()
	}
	return time.Time{}
}

func sourceIf(ok bool, source string) string {
	if ok {
		return source
	}
	return ""
}

func samEnrollmentStatusString(status map[string]any, key string) string {
	if status == nil {
		return ""
	}
	return statusString(status[key])
}

func samEnrollmentStatusInt(status map[string]any, key string) int {
	if status == nil {
		return 0
	}
	switch typed := status[key].(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	case string:
		var n int
		_, _ = fmt.Sscanf(strings.TrimSpace(typed), "%d", &n)
		return n
	default:
		return 0
	}
}
