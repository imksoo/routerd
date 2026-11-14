// SPDX-License-Identifier: BSD-3-Clause

// Package ipiptunnel contains the concrete Linux IPIP read/compare path shared
// by plan and reconciliation. It never changes kernel or stored state.
package ipiptunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os/exec"
	"slices"
	"strings"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/hybrid"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

type Desired struct {
	Name, Local, Remote, Address    string
	TTL, MTU, UnderlayMTU, Overhead int
}

func Resolve(router api.Router, store resourcequery.Store, name string, spec api.TunnelInterfaceSpec) (Desired, string, error) {
	estimate, _ := hybrid.TunnelInterfaceMTUEstimate(router, spec)
	d := Desired{Name: strings.TrimSpace(name), TTL: spec.TTL, MTU: estimate.EstimatedMTU, UnderlayMTU: estimate.UnderlayMTU, Overhead: estimate.Overhead, Address: strings.TrimSpace(spec.Address)}
	if d.TTL == 0 {
		d.TTL = 64
	}
	for _, endpoint := range []struct {
		value  string
		source api.StatusValueSourceSpec
		dst    *string
	}{
		{spec.Local, spec.LocalFrom, &d.Local}, {spec.Remote, spec.RemoteFrom, &d.Remote},
	} {
		value := strings.TrimSpace(endpoint.value)
		source := endpoint.source
		if strings.TrimSpace(source.Resource) != "" {
			kind, name, ok := strings.Cut(strings.TrimSpace(source.Resource), "/")
			if !ok || kind == "" || name == "" || strings.TrimSpace(source.Field) == "" {
				return d, "", fmt.Errorf("endpoint source requires Kind/name and field")
			}
			version := resourcequery.APIVersionForKind(kind)
			for _, r := range router.Spec.Resources {
				if r.Kind == kind && r.Metadata.Name == name {
					version = r.APIVersion
					break
				}
			}
			value = ""
			if store != nil {
				value = strings.TrimSpace(statusvalue.Field(store.ObjectStatus(version, kind, name), strings.TrimSpace(source.Field)))
			}
			if value == "" {
				return d, source.Resource + "." + source.Field, nil
			}
		}
		if prefix, err := netip.ParsePrefix(value); err == nil {
			value = prefix.Addr().String()
		}
		addr, err := netip.ParseAddr(value)
		if err != nil || !addr.Is4() {
			return d, "", fmt.Errorf("endpoint %q must be an IPv4 address or prefix", value)
		}
		*endpoint.dst = addr.String()
	}
	if d.Address != "" {
		prefix, err := netip.ParsePrefix(d.Address)
		if err != nil || !prefix.Addr().Is4() {
			return d, "", fmt.Errorf("invalid IPv4 tunnel address %q", d.Address)
		}
		d.Address = prefix.String()
	}
	return d, "", nil
}

type Observed struct {
	Exists bool
	Name   string   `json:"ifname"`
	Flags  []string `json:"flags"`
	MTU    int      `json:"mtu"`
	Info   struct {
		Kind string `json:"info_kind"`
		Data struct {
			Local  string `json:"local"`
			Remote string `json:"remote"`
			TTL    *int   `json:"ttl"`
			Encap  string `json:"encap_type"`
		} `json:"info_data"`
	} `json:"linkinfo"`
	Addresses []string
}

func read(ctx context.Context, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "ip", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ip %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func Observe(ctx context.Context, name string) (Observed, error) {
	// List all links so absence is distinct from a failed command, including a
	// same-name link of the wrong kind which the controller must replace.
	out, err := read(ctx, "-j", "-d", "link", "show")
	if err != nil {
		return Observed{}, err
	}
	var links []json.RawMessage
	if err := json.Unmarshal(out, &links); err != nil {
		return Observed{}, fmt.Errorf("decode tunnel links: %w", err)
	}
	if links == nil {
		return Observed{}, fmt.Errorf("missing tunnel link list")
	}
	for _, raw := range links {
		var header struct {
			Name string `json:"ifname"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			return Observed{}, fmt.Errorf("decode link name: %w", err)
		}
		if header.Name == "" {
			return Observed{}, fmt.Errorf("missing link name")
		}
		if header.Name != name {
			continue
		}
		var o Observed
		if err := json.Unmarshal(raw, &o); err != nil {
			return o, fmt.Errorf("decode IPIP link %s: %w", name, err)
		}
		o.Exists = true
		if o.Flags == nil || o.MTU <= 0 || o.Info.Kind == "" {
			return o, fmt.Errorf("incomplete tunnel link data for %s", name)
		}
		if o.Info.Kind == "ipip" && (o.Info.Data.Local == "" || o.Info.Data.Remote == "" || o.Info.Data.TTL == nil) {
			return o, fmt.Errorf("incomplete IPIP endpoint/TTL data for %s", name)
		}
		o.Addresses, err = IPv4Addresses(ctx, name)
		return o, err
	}
	return Observed{}, nil
}

func IPv4Addresses(ctx context.Context, name string) ([]string, error) {
	out, err := read(ctx, "-j", "-4", "address", "show", "dev", name)
	if err != nil {
		return nil, err
	}
	var links []struct {
		Info []struct {
			Local  string `json:"local"`
			Prefix *int   `json:"prefixlen"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal(out, &links); err != nil {
		return nil, fmt.Errorf("decode tunnel addresses: %w", err)
	}
	// ip -4 omits links without IPv4 addresses. An empty list is the normal
	// initial state that reconciliation must fill, not an observation failure.
	addresses := []string{}
	for _, link := range links {
		for _, a := range link.Info {
			if a.Prefix == nil {
				return nil, fmt.Errorf("missing tunnel address prefix")
			}
			p, err := netip.ParsePrefix(fmt.Sprintf("%s/%d", a.Local, *a.Prefix))
			if err != nil || !p.Addr().Is4() {
				return nil, fmt.Errorf("invalid tunnel IPv4 address")
			}
			addresses = append(addresses, p.String())
		}
	}
	return addresses, nil
}

func (o Observed) LinkMatches(d Desired) bool {
	return o.Exists && o.Info.Kind == "ipip" && (o.Info.Data.Encap == "" || o.Info.Data.Encap == "none") && o.Info.Data.Local == d.Local && o.Info.Data.Remote == d.Remote && o.Info.Data.TTL != nil && *o.Info.Data.TTL == d.TTL
}

func (o Observed) Matches(d Desired) bool {
	addressesMatch := len(o.Addresses) == 0 && d.Address == "" || len(o.Addresses) == 1 && o.Addresses[0] == d.Address
	return o.LinkMatches(d) && o.MTU > 0 && (d.MTU == 0 || o.MTU == d.MTU) && slices.Contains(o.Flags, "UP") && addressesMatch
}
