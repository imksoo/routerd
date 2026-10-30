// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

// ntpd has one config for client and server roles. A declared managed server
// remains its single generator while When is false, using only active clients'
// upstreams for that interval. Chrony's separate fragments do not use this path.
func (c NTPClientController) sharedNTPDServer() (api.Resource, bool, error) {
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
		if !spec.Managed || firstNonEmpty(spec.Provider, defaultNTPServerProvider()) != "ntpd" {
			continue
		}
		if selected.Kind != "" {
			return api.Resource{}, false, errors.New("multiple managed servers declare the shared ntpd config")
		}
		selected = resource
	}
	return selected, selected.Kind != "", nil
}

func ntpdActiveClientServers(router *api.Router, store Store) ([]string, error) {
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
		if !spec.Managed || firstNonEmpty(spec.Provider, defaultNTPProvider()) != "ntpd" {
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

func (c NTPClientController) sharedNTPDReady(ctx context.Context, server api.Resource, servers []string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	owner := ntpConfigOwner{store: c.Store, kind: "NTPServer"}
	record, err := owner.get(server.Metadata.Name)
	if err != nil || record == nil {
		return false, err
	}
	defaults, _ := platform.Current()
	if record.Provider != "ntpd" || record.Path != c.ntpConfigPath("ntpd", defaults) {
		return false, fmt.Errorf("shared ntpd config has a different provider or path")
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
	if ntpConfigDigest(data) != record.Digest {
		return false, errors.New("shared ntpd config changed while confirming client inputs")
	}
	for _, server := range servers {
		if !strings.Contains("\n"+string(data), "\nserver "+server+" iburst\n") {
			return false, nil
		}
	}
	return true, ctx.Err()
}
