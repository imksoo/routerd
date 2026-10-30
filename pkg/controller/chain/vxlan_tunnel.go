// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"strings"
	"time"
)

type vxlanGateReader struct {
	DeclaredRouter *api.Router
	Store          resourcequery.StateStore
	StartedAt      time.Time
}

var errVXLANGateRevisionChanged = errors.New("VXLAN gate dependency revision changed")

type vxlanGateState string

const (
	vxlanGateEnabled vxlanGateState = "Enabled"
	vxlanGateFalse   vxlanGateState = "WhenFalse"
	vxlanGateUnknown vxlanGateState = "WhenUnknown"
)

func (c vxlanGateReader) gateState(resource api.Resource) (vxlanGateState, error) {
	when := resourcequery.ResourceWhen(resource)
	if !resourcequery.ResourceWhenPresent(when) {
		return vxlanGateEnabled, nil
	}
	state, ok := c.Store.(resourcequery.StateStore)
	if !ok || state == nil {
		return vxlanGateUnknown, fmt.Errorf("state-variable reading is unavailable")
	}
	if vxlanWhenAcceptsUnknown(when) {
		return vxlanGateUnknown, nil
	}
	matches, err := resourcequery.ResourceWhenMatches(when, state)
	if err != nil {
		return vxlanGateUnknown, err
	}
	if matches {
		fresh, err := c.whenFreshThisBoot(when, state)
		if err != nil {
			return vxlanGateUnknown, err
		}
		if !fresh {
			return vxlanGateUnknown, nil
		}
		return vxlanGateEnabled, nil
	}
	indeterminate, err := resourcequery.ResourceWhenIndeterminate(when, state)
	if err != nil {
		return vxlanGateUnknown, err
	}
	if indeterminate {
		return vxlanGateUnknown, nil
	}
	return vxlanGateFalse, nil
}

func vxlanWhenAcceptsUnknown(when api.ResourceWhenSpec) bool {
	for _, match := range when.State {
		if strings.EqualFold(match.Status, string(routerstate.StatusUnknown)) || strings.EqualFold(match.Equals, "unknown") {
			return true
		}
		for _, value := range match.In {
			if strings.EqualFold(value, "unknown") {
				return true
			}
		}
	}
	for _, child := range append(append([]api.ResourceWhenSpec(nil), when.All...), when.Any...) {
		if vxlanWhenAcceptsUnknown(child) {
			return true
		}
	}
	return false
}

func (c vxlanGateReader) whenFreshThisBoot(when api.ResourceWhenSpec, state resourcequery.StateStore) (bool, error) {
	for name, match := range when.State {
		fresh, err := stateReferenceFresh(name, match, state, c.StartedAt)
		if err != nil || !fresh {
			return false, err
		}
	}
	for _, child := range append(append([]api.ResourceWhenSpec(nil), when.All...), when.Any...) {
		fresh, err := c.whenFreshThisBoot(child, state)
		if err != nil || !fresh {
			return false, err
		}
	}
	return true, nil
}

func stateReferenceFresh(ref string, match api.StateMatchSpec, state resourcequery.StateStore, startedAt time.Time) (bool, error) {
	maxAge := 30 * time.Second
	if match.MaxAge != "" {
		parsed, err := time.ParseDuration(match.MaxAge)
		if err != nil || parsed <= 0 {
			return false, nil
		}
		maxAge = parsed
	}
	now := state.Now().UTC()
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(ref, "${"), "}"))
	if left, _, ok := strings.Cut(trimmed, ".status."); ok {
		kind, name, valid := resourcequery.SplitResource(left)
		store, storeOK := state.(interface {
			ReadObjectStatus(string, string, string) (map[string]any, error)
		})
		if !valid || !storeOK {
			return false, fmt.Errorf("object-status reading is unavailable for %s", ref)
		}
		status, err := store.ReadObjectStatus(resourcequery.APIVersionForKind(kind), kind, name)
		if err != nil {
			return false, err
		}
		for _, key := range []string{"observedAt", "updatedAt", "lastTransitionAt"} {
			if observed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(fmt.Sprint(status[key]))); err == nil {
				return !observed.Before(startedAt) && now.Sub(observed) <= maxAge, nil
			}
		}
		return false, nil
	}
	value, err := state.ReadVariable(ref)
	if err != nil {
		return false, err
	}
	return !value.UpdatedAt.IsZero() && !value.UpdatedAt.Before(startedAt) && now.Sub(value.UpdatedAt) <= maxAge, nil
}

func (c vxlanGateReader) NextExpiryAfter() time.Duration {
	state, ok := c.Store.(resourcequery.StateStore)
	if !ok || state == nil || c.DeclaredRouter == nil {
		return 0
	}
	now := state.Now().UTC()
	var earliest time.Time
	for _, resource := range c.DeclaredRouter.Spec.Resources {
		if resource.Kind != "VXLANTunnel" {
			continue
		}
		walkWhen(resourcequery.ResourceWhen(resource), func(ref string, match api.StateMatchSpec) {
			updated, ok := stateReferenceUpdatedAt(ref, state)
			if !ok {
				return
			}
			maxAge := 30 * time.Second
			if match.MaxAge != "" {
				if parsed, err := time.ParseDuration(match.MaxAge); err == nil && parsed > 0 {
					maxAge = parsed
				} else {
					return
				}
			}
			deadline := updated.Add(maxAge)
			if earliest.IsZero() || deadline.Before(earliest) {
				earliest = deadline
			}
		})
	}
	if earliest.IsZero() {
		return 0
	}
	if !earliest.After(now) {
		// The reconciliation that just preceded this scheduling decision has
		// already observed and failed closed on the expired value. Returning the
		// normal interval prevents a permanently stale producer from resetting
		// the framework ticker to 1ms forever.
		return 0
	}
	return earliest.Sub(now)
}

func walkWhen(when api.ResourceWhenSpec, visit func(string, api.StateMatchSpec)) {
	for ref, match := range when.State {
		visit(ref, match)
	}
	for _, child := range when.All {
		walkWhen(child, visit)
	}
	for _, child := range when.Any {
		walkWhen(child, visit)
	}
}

func stateReferenceUpdatedAt(ref string, state resourcequery.StateStore) (time.Time, bool) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(ref, "${"), "}"))
	if left, _, ok := strings.Cut(trimmed, ".status."); ok {
		kind, name, valid := resourcequery.SplitResource(left)
		store, storeOK := state.(resourcequery.Store)
		if !valid || !storeOK {
			return time.Time{}, false
		}
		status := store.ObjectStatus(resourcequery.APIVersionForKind(kind), kind, name)
		for _, key := range []string{"observedAt", "updatedAt", "lastTransitionAt"} {
			if observed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(fmt.Sprint(status[key]))); err == nil {
				return observed.UTC(), true
			}
		}
		return time.Time{}, false
	}
	value := state.Get(ref)
	return value.UpdatedAt.UTC(), !value.UpdatedAt.IsZero()
}

// dependencyRevision is a deterministic version tuple over exactly the
// producer values referenced by spec.when. It provides a conservative CAS:
// any producer change between gate evaluation and a kernel commit invalidates
// the commit, including changes that leave the boolean predicate unchanged.
func (c vxlanGateReader) dependencyRevision(resource api.Resource) (string, bool, error) {
	state, ok := c.Store.(resourcequery.StateStore)
	if !ok || state == nil {
		return "", false, fmt.Errorf("state-variable reading is unavailable")
	}
	entries := map[string]any{}
	var readErr error
	walkWhen(resourcequery.ResourceWhen(resource), func(ref string, _ api.StateMatchSpec) {
		if readErr != nil {
			return
		}
		trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(ref, "${"), "}"))
		if left, _, found := strings.Cut(trimmed, ".status."); found {
			kind, name, valid := resourcequery.SplitResource(left)
			store, storeOK := state.(interface {
				ReadObjectStatus(string, string, string) (map[string]any, error)
			})
			if valid && storeOK {
				entries[ref], readErr = store.ReadObjectStatus(resourcequery.APIVersionForKind(kind), kind, name)
			} else {
				readErr = fmt.Errorf("object-status reading is unavailable for %s", ref)
			}
			return
		}
		entries[ref], readErr = state.ReadVariable(ref)
	})
	if readErr != nil {
		return "", false, readErr
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return "", false, err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), true, nil
}

func (c vxlanGateReader) requireCurrentGate(resource api.Resource, revision string, guarded bool) error {
	gate, err := c.gateState(resource)
	if err != nil {
		return err
	}
	if gate != vxlanGateEnabled {
		return errVXLANGateRevisionChanged
	}
	if guarded {
		current, ok, err := c.dependencyRevision(resource)
		if err != nil {
			return err
		}
		if !ok || current != revision {
			return errVXLANGateRevisionChanged
		}
	}
	return nil
}
