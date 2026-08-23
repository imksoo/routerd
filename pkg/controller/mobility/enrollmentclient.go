// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/samenrollment"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	defaultSAMEnrollmentRefreshBefore  = 10 * time.Minute
	defaultSAMEnrollmentBackoffMin     = 10 * time.Second
	defaultSAMEnrollmentBackoffMax     = 15 * time.Minute
	defaultSAMEnrollmentRequestTimeout = 10 * time.Second
	// Direct peer membership is independent of the long-lived RR lease. Keep
	// the bounded revalidation cadence aligned with this controller's one
	// minute schedule so a newly admitted or revoked leaf cannot leave a
	// stale, higher-preference direct path in place until lease expiry.
	defaultSAMEnrollmentDirectTopologyRefresh = time.Minute
	// A direct-topology recovery starts from an already verified RR fallback.
	// It is not a generic enrollment failure: waiting a full minute after an RR
	// returns turns the normal 60s + transport/BGP cadence into a roughly
	// two-minute direct-mesh recovery. Retry at the normal minimum backoff
	// instead, while retaining the all-RR agreement requirement below.
	defaultSAMEnrollmentDirectTopologyRecoveryBackoffMax = defaultSAMEnrollmentBackoffMin
)

type SAMEnrollmentClientStore interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
	UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord) error
	GetDynamicConfigPartsBySource(source string) ([]routerstate.DynamicConfigPartRecord, error)
}

// SAMEnrollmentJoinClient represents one bootstrap endpoint. The controller
// may issue topology GETs concurrently to distinct endpoint instances, but
// always serializes claim submissions.
type SAMEnrollmentJoinClient interface {
	SubmitSAMEnrollmentClaim(context.Context, controlapi.SAMEnrollmentClaimSubmitRequest) (*controlapi.SAMEnrollmentClaimSubmitResult, error)
	GetSAMEnrollmentTopology(context.Context, controlapi.SAMEnrollmentTopologyGetRequest) (*controlapi.SAMEnrollmentTopologyGetResult, error)
}

type SAMEnrollmentClientController struct {
	Router         *api.Router
	Store          SAMEnrollmentClientStore
	Now            func() time.Time
	ClientFactory  func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient
	requestTimeout time.Duration
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

// NextReconcileAfter returns the earliest persisted enrollment deadline.
//
// A SAMEnrollmentClient stores retry and renewal state in nextAttempt. Direct
// topology revalidation is instead due at lastSuccess plus its bounded
// refresh interval. Keep this as a read-only view of the existing typed
// status: it introduces neither another retry loop nor a second source of
// enrollment state.
func (c SAMEnrollmentClientController) NextReconcileAfter() time.Duration {
	if c.Router == nil || c.Store == nil {
		return 0
	}
	now := controllerNow(c.Now)
	var earliest time.Time
	consider := func(deadline time.Time) {
		if !deadline.After(now) {
			return
		}
		if earliest.IsZero() || deadline.Before(earliest) {
			earliest = deadline
		}
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClient" {
			continue
		}
		spec, err := resource.SAMEnrollmentClientSpec()
		if err != nil {
			continue
		}
		name := strings.TrimSpace(resource.Metadata.Name)
		if name == "" {
			continue
		}
		status := decodeStatusValue[samEnrollmentClientStatus](c.Store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", name))
		consider(status.NextAttempt)
		if status.LastSuccess.IsZero() {
			continue
		}
		_, claim, err := samEnrollmentClientClaim(c.Router, spec.ClaimRef)
		if err != nil || !claim.DirectMesh {
			continue
		}
		consider(status.LastSuccess.Add(defaultSAMEnrollmentDirectTopologyRefresh))
	}
	if earliest.IsZero() {
		return 0
	}
	return earliest.Sub(now)
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
	if !claim.DirectMesh {
		// A local direct-mesh opt-out must take effect even when the RR is
		// unreachable. Remove the already-fetched higher-preference group
		// before attempting the ordinary claim refresh, and keep retrying this
		// local safety action until its atomic RR-only replacement succeeds.
		stripped, stripErr := c.persistCachedRRSetWithoutDirect(rrSetName, now)
		if stripErr != nil {
			failures := previous.FailureCount + 1
			backoff := samEnrollmentClientBackoff(spec, failures)
			cachedRRSet := ""
			if rrState.Found {
				cachedRRSet = source
			}
			return c.saveSAMEnrollmentClientStatus(owner.Metadata.Name, samEnrollmentClientStatus{
				Phase:         "Degraded",
				ClaimRef:      spec.ClaimRef,
				ObservedRRSet: cachedRRSet,
				LastAttempt:   now,
				LastSuccess:   previous.LastSuccess,
				NextAttempt:   now.Add(backoff),
				Backoff:       backoff.String(),
				FailureCount:  failures,
				// Preserve the prior digest so an unsuccessful opt-out can never
				// be mistaken for a completed claim transition.
				ClaimDigest: previous.ClaimDigest,
				Reason:      fmt.Sprintf("withdraw cached direct topology: %v", stripErr),
			}, now)
		}
		if stripped {
			rrState, err = c.fetchedRRSetState(source, claim.PolicyRef, now)
			if err != nil {
				return err
			}
		}
	}
	observedRRSet := ""
	if rrState.Found {
		observedRRSet = source
	}
	observedDirectPeerGroup := ""
	if claim.DirectMesh && rrState.PeerGroupName != "" {
		observedDirectPeerGroup = "SAMPeerGroup/" + rrState.PeerGroupName
	}
	refreshBefore := samEnrollmentClientRefreshBefore(spec, rrState)
	reason := samEnrollmentClientRefreshReason(rrState, claimDigest, previous.ClaimDigest, refreshBefore, now)
	if reason == "" && claim.DirectMesh && (previous.DirectTopologyPending || samEnrollmentClientDirectTopologyRefreshDue(previous.LastSuccess, now)) {
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
	// v2333 rewrote a failed direct-refresh diagnostic to the generic trigger
	// on its immediate status-driven reconcile. Recognize only that legacy
	// shape and shorten it for one GET-only recovery probe. Newer failures keep
	// their real reason and retain the configured exponential backoff.
	legacyDirectProbe := reason == "direct-topology-refresh" &&
		!previous.DirectTopologyPending &&
		previous.FailureCount > 0 &&
		strings.TrimSpace(previous.Backoff) != "" &&
		strings.TrimSpace(previous.Reason) == "direct-topology-refresh"
	legacyProbeBackoff := time.Duration(0)
	if legacyDirectProbe && !previous.LastAttempt.IsZero() {
		legacyProbeBackoff = samEnrollmentClientConvergenceBackoff(spec, previous.FailureCount)
		retryAt := previous.LastAttempt.Add(legacyProbeBackoff)
		if nextAttempt.IsZero() || retryAt.Before(nextAttempt) {
			nextAttempt = retryAt
		}
	}
	// NextAttempt has two meanings: a successful RR lease schedules its normal
	// renewal at expiry, while a failed request schedules a retry backoff.  A
	// direct topology refresh must not inherit the former.  Otherwise a leaf
	// that joined before its peer waits for the whole (often 24 hour) RR lease
	// before it can discover that peer.  Only an explicit failed-request
	// backoff suppresses a new refresh or a claim/configuration transition.
	if !nextAttempt.IsZero() && now.Before(nextAttempt) && (previous.FailureCount > 0 || strings.TrimSpace(previous.Backoff) != "") {
		backoffReason := reason
		// An immediate status wake-up after a same-claim direct refresh must not
		// hide the actual endpoint/validation failure behind the generic refresh
		// trigger. A claim/lease transition, however, is new operator-relevant
		// information and must replace an older direct-refresh diagnostic.
		if reason == "direct-topology-refresh" && strings.TrimSpace(previous.Reason) != "" {
			backoffReason = previous.Reason
		}
		backoff := previous.Backoff
		if legacyProbeBackoff > 0 {
			backoff = legacyProbeBackoff.String()
		}
		return c.saveSAMEnrollmentClientStatus(owner.Metadata.Name, samEnrollmentClientStatus{
			Phase:                   "Backoff",
			ClaimRef:                spec.ClaimRef,
			ObservedRRSet:           observedRRSet,
			ObservedDirectPeerGroup: observedDirectPeerGroup,
			LastAttempt:             previous.LastAttempt,
			LastSuccess:             previous.LastSuccess,
			NextAttempt:             nextAttempt,
			Backoff:                 backoff,
			FailureCount:            previous.FailureCount,
			ClaimDigest:             previous.ClaimDigest,
			DirectTopologyPending:   previous.DirectTopologyPending,
			// Keep the failing endpoint/validation diagnostic visible while the
			// scheduled retry is waiting. A status-triggered reconcile otherwise
			// overwrites it immediately with the generic refresh trigger, which
			// made the v2333 incident impossible to diagnose after the fact.
			Reason: backoffReason,
		}, now)
	}
	if reason == "direct-topology-refresh" {
		err = c.refreshDirectTopologyAndPersist(ctx, spec, claimResource, claim, rrSetName, rrState, now, !legacyDirectProbe)
	} else {
		// A differing active identity may be deliberately replaced only for an
		// initial admission (no recorded local identity) or a local claim
		// change. RRSet expiry/loss is a renewal of the existing identity and
		// must not let a stale leaf overwrite a newer active claim.
		allowIdentityReplacement := strings.TrimSpace(previous.ClaimDigest) == "" || previous.ClaimDigest != claimDigest
		// A direct claim has a higher-preference data path. Before every normal
		// direct admission or renewal, query every RR so a revoke retained by one
		// replica cannot be overwritten after another replica restarted.
		err = c.joinFetchAndPersist(ctx, spec, claimResource, claim, rrSetName, now, claim.DirectMesh, allowIdentityReplacement)
	}
	if err != nil {
		var converging *samEnrollmentDirectTopologyConvergingError
		// A verified RRSet makes RR-only forwarding safe while the optional
		// direct view is rebuilt. Treat a failed direct snapshot GET like the
		// existing convergence state only when every failing endpoint failed at
		// the transport layer. A reachable endpoint's revoke, identity, schema,
		// or payload response remains a normal failure with its configured
		// exponential backoff.
		directTransportRetry := reason == "direct-topology-refresh" &&
			rrState.Found &&
			samEnrollmentClientDirectTopologyTransportFailure(err)
		if errors.As(err, &converging) || directTransportRetry {
			// A newly admitted or restarted RR can project peers one leaf at a
			// time, or an RR can be temporarily unreachable during a restart.
			// Retain only the independently valid RRSet and retry the optional
			// direct view promptly. Never install a subset or a one-RR-only
			// direct topology.
			if _, fallbackErr := c.persistCachedRRSetWithoutDirect(rrSetName, now); fallbackErr != nil {
				err = fmt.Errorf("%w (RR fallback retention failed: %v)", err, fallbackErr)
			} else {
				attempts := previous.FailureCount + 1
				backoff := samEnrollmentClientConvergenceBackoff(spec, attempts)
				// Every direct topology read must complete before we can decide
				// whether the RRs agree, and may take longer than the bounded
				// recovery backoff. Anchor the deadline at completion, not reconcile
				// start, so this status update cannot wake the controller into an
				// immediate timeout-rate retry loop.
				retryFrom := controllerNow(c.Now)
				if retryFrom.Before(now) {
					retryFrom = now
				}
				return c.saveSAMEnrollmentClientStatus(owner.Metadata.Name, samEnrollmentClientStatus{
					Phase:                 "Backoff",
					ClaimRef:              spec.ClaimRef,
					ObservedRRSet:         source,
					LastAttempt:           now,
					LastSuccess:           previous.LastSuccess,
					NextAttempt:           retryFrom.Add(backoff),
					Backoff:               backoff.String(),
					FailureCount:          attempts,
					ClaimDigest:           claimDigest,
					DirectTopologyPending: true,
					Reason:                err.Error(),
				}, now)
			}
		}
		failures := previous.FailureCount + 1
		backoff := samEnrollmentClientBackoff(spec, failures)
		if stripped, fallbackErr := c.persistCachedRRSetWithoutDirect(rrSetName, now); fallbackErr != nil {
			err = fmt.Errorf("%w (RR fallback retention failed: %v)", err, fallbackErr)
		} else if stripped {
			// A direct peer group has a higher BGP preference than the RR
			// topology. Every failed enrollment refresh must therefore remove a
			// cached group, including a directMesh true->false or policy change.
			// Retain the independently valid cached RRSet so traffic keeps its
			// safe path.
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
			// claimDigest denotes the last successfully confirmed claim. Do not
			// advance it for a failed rotation or partial multi-RR admission, or
			// a later GET-only direct refresh could re-enable topology from an RR
			// that never accepted the current client claim.
			ClaimDigest: previous.ClaimDigest,
			// A malformed, unreachable, revoked, or unattested response is no
			// longer the narrow all-RRs-valid convergence state.
			DirectTopologyPending: false,
			Reason:                err.Error(),
		}, now)
	}
	records, err := c.Store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return err
	}
	refreshed := latestActiveSAMEnrollmentRRSet(records, claim.PolicyRef, now)
	refreshedRefreshBefore := samEnrollmentClientRefreshBefore(spec, refreshed)
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
		NextAttempt:             refreshed.ExpiresAt.Add(-refreshedRefreshBefore),
		Backoff:                 "",
		FailureCount:            0,
		ClaimDigest:             claimDigest,
		Reason:                  reason,
	}, now)
}

type samEnrollmentSubmittedClient struct {
	client SAMEnrollmentJoinClient
	result *controlapi.SAMEnrollmentClaimSubmitResult
}

type samEnrollmentFetchedTopology struct {
	rrSet           api.Resource
	directPeerGroup *api.Resource
	// directPayloadError is retained when an RR attests the claim but supplies
	// a malformed optional direct group. It must not be confused with a valid
	// empty group while a restarted RR is relearning peers.
	directPayloadError error
	// directAttested means this endpoint explicitly bound its optional direct
	// group to the exact locally configured client claim. An RR-only result
	// remains useful when false, but it must not count toward direct agreement.
	directAttested bool
}

// validateSAMEnrollmentClaimSubmitResult treats the submit response as an
// admission receipt, not merely a successful HTTP response.  In particular,
// do not use a topology from an endpoint that accepted a different claim or
// source after a same-name rollback or endpoint misconfiguration.
func validateSAMEnrollmentClaimSubmitResult(result *controlapi.SAMEnrollmentClaimSubmitResult, claimResource api.Resource) error {
	if result == nil {
		return fmt.Errorf("SAM enrollment claim submit returned no result")
	}
	want := "SAMEnrollmentClaim/" + strings.TrimSpace(claimResource.Metadata.Name)
	if !result.Accepted {
		return fmt.Errorf("SAM enrollment claim %s was not accepted", want)
	}
	if strings.TrimSpace(result.ClaimRef) != want {
		return fmt.Errorf("SAM enrollment claim submit returned claimRef %q, want %q", result.ClaimRef, want)
	}
	if strings.TrimSpace(result.DynamicSource) != want {
		return fmt.Errorf("SAM enrollment claim submit returned dynamicSource %q, want %q", result.DynamicSource, want)
	}
	return nil
}

func (c SAMEnrollmentClientController) joinFetchAndPersist(ctx context.Context, spec api.SAMEnrollmentClientSpec, claimResource api.Resource, claim api.SAMEnrollmentClaimSpec, rrSetName string, now time.Time, preflightDirect, allowIdentityReplacement bool) error {
	var lastErr error
	var submitted []samEnrollmentSubmittedClient
	clients, err := c.clients(spec)
	if err != nil {
		return err
	}
	if preflightDirect {
		// Query every RR before POSTing a high-preference direct claim. Re-admit
		// only when failed endpoints report this exact identity missing (healthy
		// endpoints may retain it). Any revoke, malformed response, or transport
		// failure leaves the RR fallback in place. The server distinguishes a
		// different active identity only for initial admission or a local
		// claim-change path; ordinary renewal remains fail-closed.
		if err := c.preflightDirectEnrollment(ctx, clients, claimResource, claim, rrSetName); err != nil && !samEnrollmentClientDirectAdmissionPermitted(err, claimResource, allowIdentityReplacement) {
			return err
		}
	}
	for _, client := range clients {
		requestCtx, cancel := c.enrollmentRequestContext(ctx)
		submit, err := client.SubmitSAMEnrollmentClaim(requestCtx, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claimResource})
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		if err := validateSAMEnrollmentClaimSubmitResult(submit, claimResource); err != nil {
			lastErr = err
			continue
		}
		submitted = append(submitted, samEnrollmentSubmittedClient{client: client, result: submit})
	}
	if claim.DirectMesh {
		return c.fetchAndPersistAgreedDirectTopology(ctx, submitted, len(clients), claimResource, claim, rrSetName, now, lastErr)
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

// preflightDirectEnrollment decides whether it is safe to submit a direct
// claim again. It deliberately does not compare direct-peer-group digests:
// a disagreement is handled by the normal post-submit path, which preserves
// the verified RRSet while withdrawing only the optional direct group. The
// preflight's sole job is to stop an automatic POST when any RR reports a
// revoke, an old ambiguous response, an unattested claim, or a request error.
func (c SAMEnrollmentClientController) preflightDirectEnrollment(ctx context.Context, clients []SAMEnrollmentJoinClient, claimResource api.Resource, claim api.SAMEnrollmentClaimSpec, rrSetName string) error {
	if len(clients) == 0 {
		return fmt.Errorf("no SAM enrollment bootstrap endpoint configured")
	}
	var endpointErrors []error
	for _, client := range clients {
		topology, err := c.fetchSAMEnrollmentTopology(ctx, client, claimResource, claim, rrSetName)
		if err != nil {
			endpointErrors = append(endpointErrors, err)
			continue
		}
		if !topology.directAttested {
			endpointErrors = append(endpointErrors, fmt.Errorf("enrollment endpoint did not attest the current claim for direct topology"))
		}
	}
	if len(endpointErrors) > 0 {
		return &samEnrollmentDirectTopologyFetchErrors{Errors: endpointErrors}
	}
	return nil
}

// fetchAndPersistAgreedDirectTopology lets an otherwise-degraded RR pair keep
// serving ordinary RR paths, but withholds the optional high-preference direct
// group unless every configured enrollment endpoint accepted the claim and
// projected the same topology.
func (c SAMEnrollmentClientController) fetchAndPersistAgreedDirectTopology(ctx context.Context, submitted []samEnrollmentSubmittedClient, endpointCount int, claimResource api.Resource, claim api.SAMEnrollmentClaimSpec, rrSetName string, now time.Time, lastErr error) error {
	if len(submitted) == 0 {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("no SAM enrollment bootstrap endpoint configured")
	}
	allAgreed := len(submitted) == endpointCount
	var first samEnrollmentFetchedTopology
	firstSet := false
	firstDigest := ""
	topologyMismatch := false
	observedAt, expiresAt := now, now.Add(DefaultLeaseTTL)
	for _, item := range submitted {
		topology, err := c.fetchSAMEnrollmentTopology(ctx, item.client, claimResource, claim, rrSetName)
		if err != nil {
			lastErr = err
			allAgreed = false
			continue
		}
		if !topology.directAttested {
			allAgreed = false
			lastErr = fmt.Errorf("enrollment endpoint did not attest the current claim for direct topology")
		}
		if topology.directPayloadError != nil {
			allAgreed = false
			lastErr = fmt.Errorf("enrollment endpoint returned invalid direct topology: %w", topology.directPayloadError)
		}
		if !firstSet {
			first = topology
			firstSet = true
			if !item.result.ObservedAt.IsZero() {
				observedAt = item.result.ObservedAt
			}
			if !item.result.ExpiresAt.IsZero() {
				expiresAt = item.result.ExpiresAt
			}
			firstDigest, err = samEnrollmentDirectTopologyDigest(topology.directPeerGroup)
			if err != nil {
				return err
			}
			continue
		}
		digest, err := samEnrollmentDirectTopologyDigest(topology.directPeerGroup)
		if err != nil {
			return err
		}
		if digest != firstDigest {
			allAgreed = false
			topologyMismatch = true
		}
	}
	if !firstSet {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("no SAM enrollment topology could be fetched")
	}
	if allAgreed {
		return c.persistFetchedSAMEnrollmentTopology(first, observedAt, expiresAt)
	}
	first.directPeerGroup = nil
	if err := c.persistFetchedSAMEnrollmentTopology(first, observedAt, expiresAt); err != nil {
		return err
	}
	if lastErr != nil {
		return fmt.Errorf("direct topology was not agreed by all enrollment endpoints: %w", lastErr)
	}
	if topologyMismatch {
		return &samEnrollmentDirectTopologyConvergingError{}
	}
	return fmt.Errorf("direct topology was not agreed by all enrollment endpoints")
}

// refreshDirectTopologyAndPersist revalidates only the optional direct-peer
// snapshot. It intentionally does not submit or renew the local claim: the
// existing RR lease remains the authority for claim lifetime. Its caller
// removes a high-preference direct group on every enrollment-refresh failure
// while retaining the independently valid RR fallback from the same dynamic
// part. allowReadmission is false for the one-time legacy recovery probe, so
// that migration path remains strictly GET-only.
func (c SAMEnrollmentClientController) refreshDirectTopologyAndPersist(ctx context.Context, spec api.SAMEnrollmentClientSpec, claimResource api.Resource, claim api.SAMEnrollmentClaimSpec, rrSetName string, rrState samEnrollmentRRSetState, now time.Time, allowReadmission bool) error {
	clients, err := c.clients(spec)
	if err != nil {
		return err
	}
	topology, err := c.fetchAgreedDirectTopology(ctx, clients, claimResource, claim, rrSetName)
	if err != nil {
		// A route reflector can restart with an empty volatile enrollment
		// store while its long-lived RRSet is still usable on every leaf. A
		// direct refresh normally performs GET only so it does not needlessly
		// rewrite an accepted claim. The RR's identity-aware "accepted client
		// identity not found" response is the narrow exception: it proves that
		// this leaf's current claim must be re-admitted before that RR can
		// project the optional direct topology again. Reuse the normal
		// all-endpoint admission path rather than retaining a stale direct group
		// or waiting for the much later RR lease renewal.
		if allowReadmission && samEnrollmentClientReadmissionRequired(err, claimResource) {
			// fetchAgreedDirectTopology already queried every RR and established
			// that no replica holds an explicit revoke, so do not issue a redundant
			// second GET before re-admitting this unchanged claim.
			return c.joinFetchAndPersist(ctx, spec, claimResource, claim, rrSetName, now, false, false)
		}
		return err
	}
	return c.persistFetchedSAMEnrollmentTopology(topology, now, rrState.ExpiresAt)
}

// samEnrollmentClientReadmissionRequired recognizes only the identity-aware
// 400 returned by a current enrollment server which has lost this leaf's
// accepted dynamic claim. Keep this narrow: an older RR's ambiguous legacy
// not-found response, every other request failure, and malformed data retain
// the RR-only fallback rather than turning uncertainty into a claim rewrite.
// Every RR is examined first, so an explicit revocation at one endpoint wins
// over another endpoint's empty volatile store.
func samEnrollmentClientReadmissionRequired(err error, claimResource api.Resource) bool {
	claimName := strings.TrimSpace(claimResource.Metadata.Name)
	if claimName == "" {
		return false
	}
	endpointErrors := []error{err}
	var aggregate *samEnrollmentDirectTopologyFetchErrors
	if errors.As(err, &aggregate) {
		endpointErrors = aggregate.Errors
	}
	missing := false
	claimRef := "SAMEnrollmentClaim/" + claimName
	for _, endpointErr := range endpointErrors {
		var apiErr *controlapi.APIError
		if !errors.As(endpointErr, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
			return false
		}
		switch strings.TrimSpace(apiErr.Message) {
		case "bad request: accepted " + claimRef + " " + controlapi.SAMEnrollmentTopologyIdentityAbsentMessage:
			missing = true
		case "bad request: accepted " + claimRef + " is revoked":
			// A revoke tombstone is authoritative even if a different RR
			// restarted empty. Never turn it into an automatic submit.
			return false
		default:
			return false
		}
	}
	return missing
}

// samEnrollmentClientDirectAdmissionPermitted is broader than direct-refresh
// recovery only when the local configuration intentionally changes identity or
// has never recorded one. A same-identity renewal never accepts an RR that
// reports a different active identity.
func samEnrollmentClientDirectAdmissionPermitted(err error, claimResource api.Resource, allowIdentityReplacement bool) bool {
	claimName := strings.TrimSpace(claimResource.Metadata.Name)
	if claimName == "" {
		return false
	}
	endpointErrors := []error{err}
	var aggregate *samEnrollmentDirectTopologyFetchErrors
	if errors.As(err, &aggregate) {
		endpointErrors = aggregate.Errors
	}
	permitted := false
	claimRef := "SAMEnrollmentClaim/" + claimName
	for _, endpointErr := range endpointErrors {
		var apiErr *controlapi.APIError
		if !errors.As(endpointErr, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
			return false
		}
		switch strings.TrimSpace(apiErr.Message) {
		case "bad request: accepted " + claimRef + " " + controlapi.SAMEnrollmentTopologyIdentityAbsentMessage:
			permitted = true
		case "bad request: accepted " + claimRef + " " + controlapi.SAMEnrollmentTopologyIdentityMismatchMessage:
			if !allowIdentityReplacement {
				return false
			}
			permitted = true
		default:
			return false
		}
	}
	return permitted
}

func (c SAMEnrollmentClientController) fetchAndPersistTopology(ctx context.Context, client SAMEnrollmentJoinClient, claimResource api.Resource, claim api.SAMEnrollmentClaimSpec, rrSetName string, observedAt, expiresAt time.Time) error {
	topology, err := c.fetchSAMEnrollmentTopology(ctx, client, claimResource, claim, rrSetName)
	if err != nil {
		return err
	}
	return c.persistFetchedSAMEnrollmentTopology(topology, observedAt, expiresAt)
}

func (c SAMEnrollmentClientController) fetchAgreedDirectTopology(ctx context.Context, clients []SAMEnrollmentJoinClient, claimResource api.Resource, claim api.SAMEnrollmentClaimSpec, rrSetName string) (samEnrollmentFetchedTopology, error) {
	if len(clients) == 0 {
		return samEnrollmentFetchedTopology{}, fmt.Errorf("no SAM enrollment bootstrap endpoint configured")
	}
	// This helper is used only by the GET-only direct-topology refresh path.
	// Each configured RR is independent, so overlap its bounded request with
	// the others, then evaluate every result in configured order. In
	// particular, do not return early: a later RR can retain a revoke or a
	// different topology. Claim submissions remain serial in
	// joinFetchAndPersist.
	type fetchResult struct {
		topology samEnrollmentFetchedTopology
		err      error
	}
	results := make([]fetchResult, len(clients))
	var reads sync.WaitGroup
	reads.Add(len(clients))
	for index, client := range clients {
		go func(index int, client SAMEnrollmentJoinClient) {
			defer reads.Done()
			results[index].topology, results[index].err = c.fetchSAMEnrollmentTopology(ctx, client, claimResource, claim, rrSetName)
		}(index, client)
	}
	reads.Wait()

	var first samEnrollmentFetchedTopology
	firstSet := false
	firstDigest := ""
	topologyMismatch := false
	var endpointErrors []error
	unattested := false
	for _, result := range results {
		if result.err != nil {
			// Query every configured RR before deciding whether a missing claim
			// warrants readmission. A later RR can hold an explicit revocation
			// tombstone; returning at the first missing response would otherwise
			// allow that tombstone to be overwritten.
			endpointErrors = append(endpointErrors, result.err)
			continue
		}
		topology := result.topology
		if !topology.directAttested {
			unattested = true
			continue
		}
		if topology.directPayloadError != nil {
			endpointErrors = append(endpointErrors, fmt.Errorf("enrollment endpoint returned invalid direct topology: %w", topology.directPayloadError))
			continue
		}
		digest, err := samEnrollmentDirectTopologyDigest(topology.directPeerGroup)
		if err != nil {
			return samEnrollmentFetchedTopology{}, err
		}
		if !firstSet {
			first = topology
			firstSet = true
			firstDigest = digest
			continue
		}
		if digest != firstDigest {
			topologyMismatch = true
		}
	}
	if unattested {
		return samEnrollmentFetchedTopology{}, fmt.Errorf("enrollment endpoint did not attest the current claim for direct topology")
	}
	if len(endpointErrors) > 0 {
		return samEnrollmentFetchedTopology{}, &samEnrollmentDirectTopologyFetchErrors{Errors: endpointErrors}
	}
	if topologyMismatch {
		return samEnrollmentFetchedTopology{}, &samEnrollmentDirectTopologyConvergingError{}
	}
	return first, nil
}

// samEnrollmentDirectTopologyConvergingError means every reachable RR
// attested the local claim, but their complete peer-group snapshots have not
// caught up yet. It is deliberately distinct from a transport, validation, or
// revocation error: the caller retains RR-only forwarding and retries without
// admitting a one-RR direct topology.
type samEnrollmentDirectTopologyConvergingError struct{}

func (*samEnrollmentDirectTopologyConvergingError) Error() string {
	return "direct topology is converging across enrollment endpoints"
}

// samEnrollmentDirectTopologyFetchErrors preserves every endpoint response
// from a direct refresh. Callers need the complete set to distinguish a
// recoverable empty RR store from an explicit revocation at another RR.
type samEnrollmentDirectTopologyFetchErrors struct {
	Errors []error
}

func (e *samEnrollmentDirectTopologyFetchErrors) Error() string {
	if e == nil || len(e.Errors) == 0 {
		return "direct enrollment topology request failed"
	}
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}
	parts := make([]string, 0, len(e.Errors))
	for _, err := range e.Errors {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	return "direct enrollment topology requests failed: " + strings.Join(parts, "; ")
}

func (e *samEnrollmentDirectTopologyFetchErrors) Unwrap() []error {
	if e == nil {
		return nil
	}
	return e.Errors
}

// samEnrollmentClientDirectTopologyTransportFailure recognizes the narrow
// direct-refresh case where every failed RR request reached no HTTP response.
// The existing RRSet remains the safe forwarding fallback, so this path can
// use the bounded direct convergence retry. API responses remain deliberately
// outside this classification: a revoke, identity mismatch, malformed payload,
// or validation error must retain the ordinary exponential backoff.
func samEnrollmentClientDirectTopologyTransportFailure(err error) bool {
	var aggregate *samEnrollmentDirectTopologyFetchErrors
	if !errors.As(err, &aggregate) || aggregate == nil || len(aggregate.Errors) == 0 {
		return false
	}
	for _, endpointErr := range aggregate.Errors {
		if !samEnrollmentClientTransportFailure(endpointErr) {
			return false
		}
	}
	return true
}

func samEnrollmentClientTransportFailure(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var apiErr *controlapi.APIError
	if errors.As(err, &apiErr) {
		return false
	}
	var requestErr *url.Error
	if !errors.As(err, &requestErr) || requestErr == nil || requestErr.Err == nil {
		return false
	}
	if errors.Is(requestErr.Err, context.Canceled) {
		return false
	}
	var networkErr net.Error
	return errors.As(requestErr.Err, &networkErr) || errors.Is(requestErr.Err, context.DeadlineExceeded)
}

func (c SAMEnrollmentClientController) fetchSAMEnrollmentTopology(ctx context.Context, client SAMEnrollmentJoinClient, claimResource api.Resource, claim api.SAMEnrollmentClaimSpec, rrSetName string) (samEnrollmentFetchedTopology, error) {
	claimDigest := samEnrollmentClientClaimDigest(claimResource)
	if claimDigest == "" {
		return samEnrollmentFetchedTopology{}, fmt.Errorf("cannot calculate enrollment claim digest for %s", claimResource.ID())
	}
	requestCtx, cancel := c.enrollmentRequestContext(ctx)
	defer cancel()
	topology, err := client.GetSAMEnrollmentTopology(requestCtx, controlapi.SAMEnrollmentTopologyGetRequest{
		Name:                rrSetName,
		ClaimRef:            "SAMEnrollmentClaim/" + claimResource.Metadata.Name,
		ClaimDigest:         claimDigest,
		ClaimIdentityDigest: samenrollment.ClientIdentityDigest(claim),
	})
	if err != nil {
		return samEnrollmentFetchedTopology{}, err
	}
	if topology == nil {
		return samEnrollmentFetchedTopology{}, fmt.Errorf("SAM enrollment topology returned no result")
	}
	if topology.RRSet.Metadata.Name != rrSetName {
		return samEnrollmentFetchedTopology{}, fmt.Errorf("enrollment topology returned SAMRRSet/%s, want SAMRRSet/%s", topology.RRSet.Metadata.Name, rrSetName)
	}
	rrSetSpec, err := topology.RRSet.SAMRRSetSpec()
	if err != nil {
		return samEnrollmentFetchedTopology{}, err
	}
	if strings.TrimSpace(rrSetSpec.EnrollmentPolicyRef) != strings.TrimSpace(claim.PolicyRef) {
		return samEnrollmentFetchedTopology{}, fmt.Errorf("enrollment topology SAMRRSet/%s enrollmentPolicyRef %q does not match claim policyRef %q", rrSetName, rrSetSpec.EnrollmentPolicyRef, claim.PolicyRef)
	}
	if !claim.DirectMesh && topology.PeerGroup != nil {
		return samEnrollmentFetchedTopology{}, fmt.Errorf("enrollment topology returned a direct SAMPeerGroup for non-direct claim %s", claimResource.ID())
	}
	directAttested := strings.TrimSpace(topology.ClaimDigest) == claimDigest
	directPeerGroup := topology.PeerGroup
	if claim.DirectMesh && !directAttested {
		// An old, stale, or partially upgraded RR may still provide the stable
		// RRSet.  Its direct payload has no authority until its response proves
		// it projected the current accepted claim, so drop it before any
		// validation or persistence.
		directPeerGroup = nil
	}
	var directPayloadError error
	if err := config.ValidateFetchedSAMEnrollmentTopology(topology.RRSet, directPeerGroup); err != nil {
		if !claim.DirectMesh || directPeerGroup == nil {
			return samEnrollmentFetchedTopology{}, err
		}
		// Direct peers are opportunistic. Validate their full runtime shape
		// before persistence, then retain only the independently valid RRSet
		// when the direct payload is stale, malformed, or incompatible.
		directPayloadError = err
		directPeerGroup = nil
		if fallbackErr := config.ValidateFetchedSAMEnrollmentTopology(topology.RRSet, nil); fallbackErr != nil {
			return samEnrollmentFetchedTopology{}, fallbackErr
		}
	}
	return samEnrollmentFetchedTopology{rrSet: topology.RRSet, directPeerGroup: directPeerGroup, directPayloadError: directPayloadError, directAttested: directAttested}, nil
}

func (c SAMEnrollmentClientController) persistFetchedSAMEnrollmentTopology(topology samEnrollmentFetchedTopology, observedAt, expiresAt time.Time) error {
	if expiresAt.IsZero() {
		expiresAt = observedAt.Add(DefaultLeaseTTL)
	}
	recordOptions := codec.FetchedSAMEnrollmentTopologyRecordOptions{
		Name:                              safeName("fetched-sam-enrollment-topology-" + topology.rrSet.Metadata.Name),
		Generation:                        dynamicGeneration,
		DefaultTTL:                        DefaultLeaseTTL,
		IncludeEmptyDirectivesActionPlans: true,
		Digest:                            digestDynamicPart,
	}
	record, err := codec.FetchedSAMEnrollmentTopologyRecord(topology.rrSet, topology.directPeerGroup, observedAt, expiresAt, recordOptions)
	if err != nil && topology.directPeerGroup != nil {
		// Keep the codec fallback as a final structural guard. Semantic
		// validation above is deliberately separate from serialization.
		record, err = codec.FetchedSAMEnrollmentTopologyRecord(topology.rrSet, nil, observedAt, expiresAt, recordOptions)
	}
	if err != nil {
		return err
	}
	return c.Store.UpsertDynamicConfigPart(record)
}

func samEnrollmentDirectTopologyDigest(peerGroup *api.Resource) (string, error) {
	if peerGroup == nil {
		return "", nil
	}
	spec, err := peerGroup.SAMPeerGroupSpec()
	if err != nil {
		return "", err
	}
	canonical := spec
	canonical.Nodes = append([]api.SAMNodeSpec(nil), spec.Nodes...)
	sort.Slice(canonical.Nodes, func(i, j int) bool {
		return canonical.Nodes[i].NodeRef < canonical.Nodes[j].NodeRef
	})
	for i := range canonical.Nodes {
		canonical.Nodes[i].MACAddresses = append([]string(nil), canonical.Nodes[i].MACAddresses...)
		sort.Strings(canonical.Nodes[i].MACAddresses)
		canonical.Nodes[i].WireGuard.AllowedIPs = append([]string(nil), canonical.Nodes[i].WireGuard.AllowedIPs...)
		sort.Strings(canonical.Nodes[i].WireGuard.AllowedIPs)
	}
	if spec.OwnedPrefixesByNode != nil {
		canonical.OwnedPrefixesByNode = make(map[string][]string, len(spec.OwnedPrefixesByNode))
		for nodeRef, prefixes := range spec.OwnedPrefixesByNode {
			canonical.OwnedPrefixesByNode[nodeRef] = append([]string(nil), prefixes...)
			sort.Strings(canonical.OwnedPrefixesByNode[nodeRef])
		}
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:]), nil
}

// persistCachedRRSetWithoutDirect atomically withdraws a fetched direct group
// while retaining its accompanying RRSet. The cached policy may differ from
// the current claim during a policy migration, so selection is intentionally
// by source and RRSet name rather than by the current policy reference.
func (c SAMEnrollmentClientController) persistCachedRRSetWithoutDirect(rrSetName string, now time.Time) (bool, error) {
	source := "SAMRRSet/" + strings.TrimSpace(rrSetName)
	records, err := c.Store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return false, err
	}
	var selected *routerstate.DynamicConfigPartRecord
	var selectedRRSet api.Resource
	for _, record := range records {
		if record.EffectiveStatus(now) != "active" {
			continue
		}
		resources, err := codec.DecodeGenericResources(record)
		if err != nil {
			continue
		}
		var rrSet api.Resource
		hasDirectPeerGroup := false
		for _, resource := range resources {
			if resource.APIVersion != api.MobilityAPIVersion {
				continue
			}
			if resource.Kind == "SAMRRSet" && strings.TrimSpace(resource.Metadata.Name) == strings.TrimSpace(rrSetName) {
				rrSet = resource
			}
			if resource.Kind == "SAMPeerGroup" {
				hasDirectPeerGroup = true
			}
		}
		if rrSet.Metadata.Name == "" || !hasDirectPeerGroup || (selected != nil && !record.ObservedAt.After(selected.ObservedAt)) {
			continue
		}
		copy := record
		selected = &copy
		selectedRRSet = rrSet
	}
	if selected == nil {
		return false, nil
	}
	fallback, err := codec.FetchedSAMEnrollmentTopologyRecord(selectedRRSet, nil, selected.ObservedAt, selected.ExpiresAt, codec.FetchedSAMEnrollmentTopologyRecordOptions{
		Name:                              safeName("fetched-sam-enrollment-topology-" + selectedRRSet.Metadata.Name),
		Generation:                        dynamicGeneration,
		DefaultTTL:                        DefaultLeaseTTL,
		IncludeEmptyDirectivesActionPlans: true,
		Digest:                            digestDynamicPart,
	})
	if err != nil {
		return false, err
	}
	if err := c.Store.UpsertDynamicConfigPart(fallback); err != nil {
		return false, err
	}
	return true, nil
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

func (c SAMEnrollmentClientController) enrollmentRequestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := c.requestTimeout
	if timeout <= 0 {
		timeout = defaultSAMEnrollmentRequestTimeout
	}
	return context.WithTimeout(ctx, timeout)
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
	DirectTopologyPending   bool
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
	if status.DirectTopologyPending {
		out["directTopologyPending"] = true
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

// samEnrollmentClientRefreshBefore returns the requested renewal lead time,
// constrained only when it is at least as long as the lease observed from an
// RR response. A policy may legitimately use the server's default five-minute
// lease while a client leaves its ten-minute refresh-before default in place.
// Renewing halfway through that observed lease avoids an immediate renewal
// loop without rejecting an otherwise valid configuration. A shorter explicit
// refresh-before remains unchanged.
func samEnrollmentClientRefreshBefore(spec api.SAMEnrollmentClientSpec, rrState samEnrollmentRRSetState) time.Duration {
	requested := durationDefault(spec.StateTTLRefreshBefore, defaultSAMEnrollmentRefreshBefore)
	if requested <= 0 || rrState.ObservedAt.IsZero() || rrState.ExpiresAt.IsZero() {
		return requested
	}
	lease := rrState.ExpiresAt.Sub(rrState.ObservedAt)
	if lease <= 0 || requested < lease {
		return requested
	}
	if safe := lease / 2; safe > 0 {
		return safe
	}
	return requested
}

func samEnrollmentClientRefreshReason(rrState samEnrollmentRRSetState, claimDigest, previousClaimDigest string, refreshBefore time.Duration, now time.Time) string {
	if !rrState.Found {
		return "rrset-missing"
	}
	if previousClaimDigest != claimDigest {
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
	claim, err := resource.SAMEnrollmentClaimSpec()
	if err != nil {
		return ""
	}
	return samenrollment.ClaimDigest(claim)
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

func samEnrollmentClientConvergenceBackoff(spec api.SAMEnrollmentClientSpec, attempts int) time.Duration {
	backoff := samEnrollmentClientBackoff(spec, attempts)
	minBackoff := durationDefault(spec.RetryBackoff.Min, defaultSAMEnrollmentBackoffMin)
	if minBackoff <= defaultSAMEnrollmentDirectTopologyRecoveryBackoffMax && backoff > defaultSAMEnrollmentDirectTopologyRecoveryBackoffMax {
		return defaultSAMEnrollmentDirectTopologyRecoveryBackoffMax
	}
	return backoff
}
