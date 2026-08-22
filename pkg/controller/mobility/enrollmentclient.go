// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	defaultSAMEnrollmentRefreshBefore = 10 * time.Minute
	defaultSAMEnrollmentBackoffMin    = 10 * time.Second
	defaultSAMEnrollmentBackoffMax    = 15 * time.Minute
	// Direct peer membership is independent of the long-lived RR lease. Keep
	// the bounded revalidation cadence aligned with this controller's one
	// minute schedule so a newly admitted or revoked leaf cannot leave a
	// stale, higher-preference direct path in place until lease expiry.
	defaultSAMEnrollmentDirectTopologyRefresh = time.Minute
)

type SAMEnrollmentClientStore interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
	UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord) error
	GetDynamicConfigPartsBySource(source string) ([]routerstate.DynamicConfigPartRecord, error)
}

type SAMEnrollmentJoinClient interface {
	SubmitSAMEnrollmentClaim(context.Context, controlapi.SAMEnrollmentClaimSubmitRequest) (*controlapi.SAMEnrollmentClaimSubmitResult, error)
	GetSAMEnrollmentTopology(context.Context, controlapi.SAMEnrollmentTopologyGetRequest) (*controlapi.SAMEnrollmentTopologyGetResult, error)
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
	now := controllerNow(c.Now)
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
	// The object store exposes JSON-shaped status, but enrollment continuation
	// is a typed retry state. Decode it once at the controller boundary rather
	// than making the retry branches interpret individual map fields.
	previous := decodeStatusValue[samEnrollmentClientStatus](c.Store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", owner.Metadata.Name))
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
	rrState, err := c.fetchedRRSetState(source, claim.PolicyRef, now)
	if err != nil {
		return err
	}
	observedRRSet := ""
	if rrState.Found {
		observedRRSet = source
	}
	observedDirectPeerGroup := ""
	if claim.DirectMesh && rrState.PeerGroupName != "" {
		observedDirectPeerGroup = "SAMPeerGroup/" + rrState.PeerGroupName
	}
	refreshBefore := durationDefault(spec.StateTTLRefreshBefore, defaultSAMEnrollmentRefreshBefore)
	reason := samEnrollmentClientRefreshReason(rrState, claimDigest, previous.ClaimDigest, refreshBefore, now)
	if reason == "" && claim.DirectMesh && samEnrollmentClientDirectTopologyRefreshDue(previous.LastSuccess, now) {
		reason = "direct-topology-refresh"
	}
	if reason == "" {
		next := rrState.ExpiresAt.Add(-refreshBefore)
		if next.Before(now) {
			next = now.Add(refreshBefore)
		}
		return c.saveSAMEnrollmentClientStatus(owner.Metadata.Name, samEnrollmentClientStatus{
			Phase:                   "Ready",
			ClaimRef:                spec.ClaimRef,
			ObservedRRSet:           source,
			ObservedDirectPeerGroup: observedDirectPeerGroup,
			LastAttempt:             previous.LastAttempt,
			LastSuccess:             previous.LastSuccess,
			NextAttempt:             next,
			Backoff:                 "",
			FailureCount:            0,
			ClaimDigest:             claimDigest,
			Reason:                  "rrset-current",
		}, now)
	}
	nextAttempt := previous.NextAttempt
	if !nextAttempt.IsZero() && now.Before(nextAttempt) {
		return c.saveSAMEnrollmentClientStatus(owner.Metadata.Name, samEnrollmentClientStatus{
			Phase:                   "Backoff",
			ClaimRef:                spec.ClaimRef,
			ObservedRRSet:           observedRRSet,
			ObservedDirectPeerGroup: observedDirectPeerGroup,
			LastAttempt:             previous.LastAttempt,
			LastSuccess:             previous.LastSuccess,
			NextAttempt:             nextAttempt,
			Backoff:                 previous.Backoff,
			FailureCount:            previous.FailureCount,
			ClaimDigest:             claimDigest,
			Reason:                  reason,
		}, now)
	}
	if reason == "direct-topology-refresh" {
		err = c.refreshDirectTopologyAndPersist(ctx, spec, claimResource, claim, rrSetName, rrState, now)
	} else {
		err = c.joinFetchAndPersist(ctx, spec, claimResource, claim, rrSetName, now)
	}
	if err != nil {
		failures := previous.FailureCount + 1
		backoff := samEnrollmentClientBackoff(spec, failures)
		if claim.DirectMesh && rrState.Found {
			// A direct peer group has a higher BGP preference than the RR
			// topology. Every failed enrollment refresh must therefore remove
			// it, not only the periodic GET-only revalidation path. Retain the
			// independently valid cached RRSet so traffic keeps its safe path.
			if fallbackErr := c.persistCachedRRSetWithoutDirect(rrSetName, claim.PolicyRef, now); fallbackErr != nil {
				err = fmt.Errorf("%w (RR fallback retention failed: %v)", err, fallbackErr)
			}
			observedDirectPeerGroup = ""
		}
		return c.saveSAMEnrollmentClientStatus(owner.Metadata.Name, samEnrollmentClientStatus{
			Phase:                   "Degraded",
			ClaimRef:                spec.ClaimRef,
			ObservedRRSet:           observedRRSet,
			ObservedDirectPeerGroup: observedDirectPeerGroup,
			LastAttempt:             now,
			LastSuccess:             previous.LastSuccess,
			NextAttempt:             now.Add(backoff),
			Backoff:                 backoff.String(),
			FailureCount:            failures,
			ClaimDigest:             claimDigest,
			Reason:                  err.Error(),
		}, now)
	}
	records, err := c.Store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return err
	}
	refreshed := latestActiveSAMEnrollmentRRSet(records, claim.PolicyRef, now)
	if claim.DirectMesh && refreshed.PeerGroupName != "" {
		observedDirectPeerGroup = "SAMPeerGroup/" + refreshed.PeerGroupName
	} else {
		observedDirectPeerGroup = ""
	}
	return c.saveSAMEnrollmentClientStatus(owner.Metadata.Name, samEnrollmentClientStatus{
		Phase:                   "Ready",
		ClaimRef:                spec.ClaimRef,
		ObservedRRSet:           source,
		ObservedDirectPeerGroup: observedDirectPeerGroup,
		LastAttempt:             now,
		LastSuccess:             now,
		NextAttempt:             refreshed.ExpiresAt.Add(-refreshBefore),
		Backoff:                 "",
		FailureCount:            0,
		ClaimDigest:             claimDigest,
		Reason:                  reason,
	}, now)
}

func (c SAMEnrollmentClientController) joinFetchAndPersist(ctx context.Context, spec api.SAMEnrollmentClientSpec, claimResource api.Resource, claim api.SAMEnrollmentClaimSpec, rrSetName string, now time.Time) error {
	var lastErr error
	var submitted []struct {
		client SAMEnrollmentJoinClient
		result *controlapi.SAMEnrollmentClaimSubmitResult
	}
	clients, err := c.clients(spec)
	if err != nil {
		return err
	}
	for _, client := range clients {
		submit, err := client.SubmitSAMEnrollmentClaim(ctx, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claimResource})
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
		observedAt := item.result.ObservedAt
		if observedAt.IsZero() {
			observedAt = now
		}
		expiresAt := item.result.ExpiresAt
		if expiresAt.IsZero() {
			expiresAt = now.Add(DefaultLeaseTTL)
		}
		if err := c.fetchAndPersistTopology(ctx, item.client, claimResource, claim, rrSetName, observedAt, expiresAt); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no SAM enrollment bootstrap endpoint configured")
}

// refreshDirectTopologyAndPersist revalidates only the optional direct-peer
// snapshot. It intentionally does not submit or renew the local claim: the
// existing RR lease remains the authority for claim lifetime. Its caller
// removes a high-preference direct group on every enrollment-refresh failure
// while retaining the independently valid RR fallback from the same dynamic
// part.
func (c SAMEnrollmentClientController) refreshDirectTopologyAndPersist(ctx context.Context, spec api.SAMEnrollmentClientSpec, claimResource api.Resource, claim api.SAMEnrollmentClaimSpec, rrSetName string, rrState samEnrollmentRRSetState, now time.Time) error {
	clients, err := c.clients(spec)
	if err != nil {
		return err
	}
	var lastErr error
	for _, client := range clients {
		if err := c.fetchAndPersistTopology(ctx, client, claimResource, claim, rrSetName, now, rrState.ExpiresAt); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no SAM enrollment bootstrap endpoint configured")
	}
	return lastErr
}

func (c SAMEnrollmentClientController) fetchAndPersistTopology(ctx context.Context, client SAMEnrollmentJoinClient, claimResource api.Resource, claim api.SAMEnrollmentClaimSpec, rrSetName string, observedAt, expiresAt time.Time) error {
	topology, err := client.GetSAMEnrollmentTopology(ctx, controlapi.SAMEnrollmentTopologyGetRequest{Name: rrSetName, ClaimRef: "SAMEnrollmentClaim/" + claimResource.Metadata.Name})
	if err != nil {
		return err
	}
	if topology.RRSet.Metadata.Name != rrSetName {
		return fmt.Errorf("enrollment topology returned SAMRRSet/%s, want SAMRRSet/%s", topology.RRSet.Metadata.Name, rrSetName)
	}
	rrSetSpec, err := topology.RRSet.SAMRRSetSpec()
	if err != nil {
		return err
	}
	if strings.TrimSpace(rrSetSpec.EnrollmentPolicyRef) != strings.TrimSpace(claim.PolicyRef) {
		return fmt.Errorf("enrollment topology SAMRRSet/%s enrollmentPolicyRef %q does not match claim policyRef %q", rrSetName, rrSetSpec.EnrollmentPolicyRef, claim.PolicyRef)
	}
	if !claim.DirectMesh && topology.PeerGroup != nil {
		return fmt.Errorf("enrollment topology returned a direct SAMPeerGroup for non-direct claim %s", claimResource.ID())
	}
	if expiresAt.IsZero() {
		expiresAt = observedAt.Add(DefaultLeaseTTL)
	}
	recordOptions := codec.FetchedSAMEnrollmentTopologyRecordOptions{
		Name:                              safeName("fetched-sam-enrollment-topology-" + topology.RRSet.Metadata.Name),
		Generation:                        dynamicGeneration,
		DefaultTTL:                        DefaultLeaseTTL,
		IncludeEmptyDirectivesActionPlans: true,
		Digest:                            digestDynamicPart,
	}
	directPeerGroup := topology.PeerGroup
	if err := config.ValidateFetchedSAMEnrollmentTopology(topology.RRSet, directPeerGroup); err != nil {
		if !claim.DirectMesh || directPeerGroup == nil {
			return err
		}
		// Direct peers are opportunistic. Validate their full runtime shape
		// before persistence, then retain only the independently valid RRSet
		// when the direct payload is stale, malformed, or incompatible.
		directPeerGroup = nil
		if fallbackErr := config.ValidateFetchedSAMEnrollmentTopology(topology.RRSet, nil); fallbackErr != nil {
			return fallbackErr
		}
	}
	record, err := codec.FetchedSAMEnrollmentTopologyRecord(topology.RRSet, directPeerGroup, observedAt, expiresAt, recordOptions)
	if err != nil && claim.DirectMesh && directPeerGroup != nil {
		// Keep the codec fallback as a final structural guard. Semantic
		// validation above is deliberately separate from serialization.
		record, err = codec.FetchedSAMEnrollmentTopologyRecord(topology.RRSet, nil, observedAt, expiresAt, recordOptions)
	}
	if err != nil {
		return err
	}
	return c.Store.UpsertDynamicConfigPart(record)
}

func (c SAMEnrollmentClientController) persistCachedRRSetWithoutDirect(rrSetName, policyRef string, now time.Time) error {
	source := "SAMRRSet/" + strings.TrimSpace(rrSetName)
	records, err := c.Store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.EffectiveStatus(now) != "active" {
			continue
		}
		resources, err := codec.DecodeGenericResources(record)
		if err != nil {
			continue
		}
		for _, resource := range resources {
			if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMRRSet" || strings.TrimSpace(resource.Metadata.Name) != strings.TrimSpace(rrSetName) {
				continue
			}
			spec, err := resource.SAMRRSetSpec()
			if err != nil || strings.TrimSpace(spec.EnrollmentPolicyRef) != strings.TrimSpace(policyRef) {
				continue
			}
			fallback, err := codec.FetchedSAMEnrollmentTopologyRecord(resource, nil, record.ObservedAt, record.ExpiresAt, codec.FetchedSAMEnrollmentTopologyRecordOptions{
				Name:                              safeName("fetched-sam-enrollment-topology-" + resource.Metadata.Name),
				Generation:                        dynamicGeneration,
				DefaultTTL:                        DefaultLeaseTTL,
				IncludeEmptyDirectivesActionPlans: true,
				Digest:                            digestDynamicPart,
			})
			if err != nil {
				return err
			}
			return c.Store.UpsertDynamicConfigPart(fallback)
		}
	}
	return fmt.Errorf("active cached SAMRRSet/%s not found", rrSetName)
}

func (c SAMEnrollmentClientController) clients(spec api.SAMEnrollmentClientSpec) ([]SAMEnrollmentJoinClient, error) {
	if c.ClientFactory != nil {
		return c.ClientFactory(spec), nil
	}
	var out []SAMEnrollmentJoinClient
	if socket := strings.TrimSpace(spec.RRSocket); socket != "" {
		out = append(out, controlapi.NewUnixClient(socket))
	}
	endpoints := make([]string, 0, len(spec.BootstrapEndpoints))
	for _, endpoint := range spec.BootstrapEndpoints {
		if endpoint = strings.TrimSpace(endpoint); endpoint != "" {
			endpoints = append(endpoints, endpoint)
		}
	}
	if len(endpoints) == 0 {
		return out, nil
	}
	token, err := samEnrollmentClientControlAPIToken(spec.ControlAPITokenFrom)
	if err != nil {
		return nil, err
	}
	for _, endpoint := range endpoints {
		client, err := controlapi.NewHTTPClientWithTLS(endpoint, controlapi.TLSOptions{
			CAFile:             spec.ControlAPITLS.CAFile,
			CertFile:           spec.ControlAPITLS.CertFile,
			KeyFile:            spec.ControlAPITLS.KeyFile,
			ServerName:         spec.ControlAPITLS.ServerName,
			InsecureSkipVerify: spec.ControlAPITLS.InsecureSkipVerify,
		})
		if err != nil {
			return nil, err
		}
		if token != "" {
			client = client.WithBearerToken(token)
		}
		out = append(out, client)
	}
	return out, nil
}

func samEnrollmentClientControlAPIToken(source api.SecretValueSourceSpec) (string, error) {
	hasFile := strings.TrimSpace(source.File) != ""
	hasEnv := strings.TrimSpace(source.Env) != ""
	if !hasFile && !hasEnv {
		return "", nil
	}
	if hasFile == hasEnv {
		return "", fmt.Errorf("controlAPITokenFrom.file or controlAPITokenFrom.env must be set, but not both")
	}
	var value string
	switch {
	case hasFile:
		path := strings.TrimSpace(source.File)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read control API token file %q: %w", path, err)
		}
		value = string(data)
	case hasEnv:
		name := strings.TrimSpace(source.Env)
		found, ok := os.LookupEnv(name)
		if !ok {
			return "", fmt.Errorf("read control API token env %q: not set", name)
		}
		value = found
	}
	value = strings.TrimSpace(value)
	if source.Base64 {
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return "", fmt.Errorf("decode base64 control API token: %w", err)
		}
		value = strings.TrimSpace(string(decoded))
	}
	if value == "" {
		return "", fmt.Errorf("control API token must not be empty")
	}
	return value, nil
}

type samEnrollmentClientStatus struct {
	Phase                   string
	ClaimRef                string
	ObservedRRSet           string
	ObservedDirectPeerGroup string
	LastAttempt             time.Time
	LastSuccess             time.Time
	NextAttempt             time.Time
	Backoff                 string
	FailureCount            int
	ClaimDigest             string
	Reason                  string
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
	if status.ObservedDirectPeerGroup != "" {
		out["observedDirectPeerGroup"] = status.ObservedDirectPeerGroup
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
	Found         bool
	PeerGroupName string
	ObservedAt    time.Time
	ExpiresAt     time.Time
}

func (c SAMEnrollmentClientController) fetchedRRSetState(source, policyRef string, now time.Time) (samEnrollmentRRSetState, error) {
	records, err := c.Store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return samEnrollmentRRSetState{}, err
	}
	return latestActiveSAMEnrollmentRRSet(records, policyRef, now), nil
}

func latestActiveSAMEnrollmentRRSet(records []routerstate.DynamicConfigPartRecord, policyRef string, now time.Time) samEnrollmentRRSetState {
	var latest *routerstate.DynamicConfigPartRecord
	for _, record := range records {
		if record.EffectiveStatus(now) != "active" {
			continue
		}
		if latest == nil || record.ObservedAt.After(latest.ObservedAt) {
			copy := record
			latest = &copy
		}
	}
	if latest == nil {
		return samEnrollmentRRSetState{}
	}
	validRRSet, peerGroupName := samEnrollmentTopologyRecordPeerGroupName(*latest, policyRef)
	if !validRRSet {
		return samEnrollmentRRSetState{}
	}
	return samEnrollmentRRSetState{
		Found:         true,
		PeerGroupName: peerGroupName,
		ObservedAt:    latest.ObservedAt,
		ExpiresAt:     latest.ExpiresAt,
	}
}

func samEnrollmentTopologyRecordPeerGroupName(record routerstate.DynamicConfigPartRecord, policyRef string) (bool, string) {
	sourceKind, sourceName, sourceOK := strings.Cut(strings.TrimSpace(record.Source), "/")
	if !sourceOK || sourceKind != "SAMRRSet" || strings.TrimSpace(sourceName) == "" {
		return false, ""
	}
	resources, err := codec.DecodeGenericResources(record)
	if err != nil {
		return false, ""
	}
	validRRSet := false
	peerGroupName := ""
	for _, resource := range resources {
		if resource.APIVersion != api.MobilityAPIVersion {
			continue
		}
		switch resource.Kind {
		case "SAMRRSet":
			spec, err := resource.SAMRRSetSpec()
			if err == nil && strings.TrimSpace(resource.Metadata.Name) == strings.TrimSpace(sourceName) && strings.TrimSpace(spec.EnrollmentPolicyRef) == strings.TrimSpace(policyRef) {
				validRRSet = true
			}
		case "SAMPeerGroup":
			spec, err := resource.SAMPeerGroupSpec()
			if err == nil && strings.TrimSpace(resource.Metadata.Name) != "" && strings.TrimSpace(spec.EnrollmentPolicyRef) == strings.TrimSpace(policyRef) && strings.TrimSpace(spec.TransportFingerprint) != "" {
				peerGroupName = strings.TrimSpace(resource.Metadata.Name)
			}
		}
	}
	return validRRSet, peerGroupName
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

func samEnrollmentClientDirectTopologyRefreshDue(lastSuccess, now time.Time) bool {
	return lastSuccess.IsZero() || !now.Before(lastSuccess.Add(defaultSAMEnrollmentDirectTopologyRefresh))
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
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:])
}

func samEnrollmentClientBackoff(spec api.SAMEnrollmentClientSpec, failures int) time.Duration {
	minBackoff := durationDefault(spec.RetryBackoff.Min, defaultSAMEnrollmentBackoffMin)
	maxBackoff := durationDefault(spec.RetryBackoff.Max, defaultSAMEnrollmentBackoffMax)
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
