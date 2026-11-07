// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/state"
)

// A managed server owns the shared provider's config and service. Chrony also
// replaces timesyncd, as in the existing provider conflict policy. Retain that
// single owner while When is false so active clients keep their upstreams.
func sharedNTPProvider(client, server string) bool {
	return client == server || client == "systemd-timesyncd" && server == "chrony"
}

func (c NTPClientController) sharedNTPServer(clientProvider string) (api.Resource, bool, error) {
	declared := c.DeclaredRouter
	if declared == nil {
		declared = c.Router
	}
	if declared == nil {
		return api.Resource{}, false, errors.New("NTP declaration is unavailable")
	}
	var selected api.Resource
	for _, resource := range declared.Spec.Resources {
		if resource.Kind != "NTPServer" {
			continue
		}
		spec, err := resource.NTPServerSpec()
		if err != nil {
			return api.Resource{}, false, err
		}
		if !spec.Managed || !sharedNTPProvider(clientProvider, firstNonEmpty(spec.Provider, defaultNTPServerProvider())) {
			continue
		}
		if selected.Kind != "" {
			return api.Resource{}, false, errors.New("multiple managed servers declare the shared NTP provider")
		}
		selected = resource
	}
	return selected, selected.Kind != "", nil
}

func sharedNTPClientServers(router *api.Router, store Store, serverProvider string) ([]string, error) {
	if router == nil {
		return nil, errors.New("current NTP client declarations are unavailable")
	}
	var servers []string
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "NTPClient" {
			continue
		}
		spec, err := resource.NTPClientSpec()
		if err != nil {
			return nil, err
		}
		if !spec.Managed || !sharedNTPProvider(firstNonEmpty(spec.Provider, defaultNTPProvider()), serverProvider) {
			continue
		}
		current, _, err := resolveNTPServers(store, spec.Source, spec.Servers, spec.ServerFrom, spec.FallbackServers)
		if err != nil {
			return nil, err
		}
		servers = append(servers, current...)
	}
	return compactNTPList(servers), nil
}

func (c NTPClientController) sharedNTPReady(ctx context.Context, server api.Resource, provider string, servers []string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	owner := ntpConfigOwner{store: c.Store, kind: "NTPServer"}
	record, err := owner.get(server.Metadata.Name)
	if err != nil || record == nil {
		return false, err
	}
	if record.Provider != provider {
		return false, fmt.Errorf("shared NTP config has a different provider")
	}
	if record.Withdrawing || record.ReloadPending || record.NextDigest != "" {
		return false, nil
	}
	if err := owner.check(*record); err != nil {
		return false, err
	}
	// check() can wait for SQLite. Read the exact confirmed fragment afterward.
	data, err := ntpConfigFile(record.Path)
	if err != nil {
		return false, err
	}
	if !record.matches(data) {
		return false, errors.New("shared NTP config changed while confirming client inputs")
	}
	for _, server := range servers {
		if !strings.Contains("\n"+string(data), "\nserver "+server+" iburst\n") {
			return false, nil
		}
	}
	return true, ctx.Err()
}

// Delegation has no client-owned file receipt. Retire only its public result;
// this display data never grants authority over the server's file or service.
func (c NTPClientController) cleanupRemovedNTPDelegations() error {
	lister, ok := c.Store.(state.ObjectStatusLister)
	if !ok {
		return nil
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return err
	}
	declared := c.DeclaredRouter
	if declared == nil {
		declared = c.Router
	}
	if declared == nil {
		return errors.New("NTP declaration is unavailable")
	}
	for _, object := range statuses {
		if object.APIVersion != api.SystemAPIVersion || object.Kind != "NTPClient" || object.Status["managedBy"] != "NTPServer" {
			continue
		}
		present := false
		for _, resource := range declared.Spec.Resources {
			if resource.Kind == "NTPClient" && resource.Metadata.Name == object.Name {
				present = true
				break
			}
		}
		if !present && !c.DryRun {
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "NTPClient", object.Name, map[string]any{"phase": "Removed", "reason": "DelegationRemoved", "dryRun": false}); err != nil {
				return err
			}
		}
	}
	return nil
}
