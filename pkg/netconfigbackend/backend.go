// SPDX-License-Identifier: BSD-3-Clause

package netconfigbackend

import (
	"fmt"
	"net/netip"
	"strings"

	"routerd/pkg/api"
	"routerd/pkg/platform"
	"routerd/pkg/render"
)

type Address struct {
	Resource  string
	Family    string
	Interface string
	Address   string
}

type Route struct {
	Resource    string
	Family      string
	Interface   string
	Destination string
	Via         string
	Metric      int
}

type Declarations struct {
	Addresses []Address
	Routes    []Route
}

type Backend interface {
	Name() string
	Declarations(*api.Router) (Declarations, error)
	Render(*api.Router) ([]render.File, error)
}

type Netplan struct {
	Path string
}

func (b Netplan) Name() string { return "netplan" }

func (b Netplan) Declarations(router *api.Router) (Declarations, error) {
	return DeclarationsFromRouter(router)
}

func (b Netplan) Render(router *api.Router) ([]render.File, error) {
	data, err := render.Netplan(router)
	if err != nil || len(data) == 0 {
		return nil, err
	}
	path := b.Path
	if path == "" {
		defaults, _ := platform.Current()
		path = defaults.NetplanFile
	}
	if err := validateOutputPath(b.Name(), path); err != nil {
		return nil, err
	}
	return []render.File{{Path: path, Data: data}}, nil
}

type Networkd struct{}

func (Networkd) Name() string { return "networkd" }

func (Networkd) Declarations(router *api.Router) (Declarations, error) {
	return DeclarationsFromRouter(router)
}

func (Networkd) Render(router *api.Router) ([]render.File, error) {
	return render.NetworkdDropins(router)
}

type NixOS struct {
	Path string
}

func (NixOS) Name() string { return "nixos" }

func (NixOS) Declarations(router *api.Router) (Declarations, error) {
	return DeclarationsFromRouter(router)
}

func (b NixOS) Render(router *api.Router) ([]render.File, error) {
	data, err := render.NixOSModule(router)
	if err != nil || len(data) == 0 {
		return nil, err
	}
	path := b.Path
	if path == "" {
		path = "/etc/nixos/routerd-generated.nix"
	}
	if err := validateOutputPath(b.Name(), path); err != nil {
		return nil, err
	}
	return []render.File{{Path: path, Data: data}}, nil
}

type RCConf struct {
	Path        string
	PasswordFor func(api.Resource, api.PPPoESessionSpec) (string, error)
}

func (RCConf) Name() string { return "rc.conf" }

func (RCConf) Declarations(router *api.Router) (Declarations, error) {
	return DeclarationsFromRouter(router)
}

func (b RCConf) Render(router *api.Router) ([]render.File, error) {
	passwordFor := b.PasswordFor
	if passwordFor == nil {
		passwordFor = func(api.Resource, api.PPPoESessionSpec) (string, error) { return "", nil }
	}
	data, err := render.FreeBSDWithPPPoEPasswords(router, passwordFor)
	if err != nil || len(data.RCConf) == 0 {
		return nil, err
	}
	path := b.Path
	if path == "" {
		path = "/etc/rc.conf.d/routerd"
	}
	if err := validateOutputPath(b.Name(), path); err != nil {
		return nil, err
	}
	return []render.File{{Path: path, Data: data.RCConf}}, nil
}

func DeclarationsFromRouter(router *api.Router) (Declarations, error) {
	var declarations Declarations
	if router == nil {
		return declarations, nil
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "IPv4StaticAddress":
			spec, err := resource.IPv4StaticAddressSpec()
			if err != nil {
				return declarations, err
			}
			declarations.Addresses = append(declarations.Addresses, Address{
				Resource:  resource.ID(),
				Family:    "ipv4",
				Interface: spec.Interface,
				Address:   spec.Address,
			})
		case "IPv6DelegatedAddress":
			spec, err := resource.IPv6DelegatedAddressSpec()
			if err != nil {
				return declarations, err
			}
			declarations.Addresses = append(declarations.Addresses, Address{
				Resource:  resource.ID(),
				Family:    "ipv6",
				Interface: spec.Interface,
				Address:   spec.AddressSuffix,
			})
		case "IPv4StaticRoute":
			spec, err := resource.IPv4StaticRouteSpec()
			if err != nil {
				return declarations, err
			}
			declarations.Routes = append(declarations.Routes, Route{
				Resource:    resource.ID(),
				Family:      "ipv4",
				Interface:   spec.Interface,
				Destination: spec.Destination,
				Via:         spec.Via,
				Metric:      spec.Metric,
			})
		case "IPv6StaticRoute":
			spec, err := resource.IPv6StaticRouteSpec()
			if err != nil {
				return declarations, err
			}
			if _, err := netip.ParseAddr(spec.Via); err != nil {
				return declarations, fmt.Errorf("%s spec.via is invalid: %w", resource.ID(), err)
			}
			declarations.Routes = append(declarations.Routes, Route{
				Resource:    resource.ID(),
				Family:      "ipv6",
				Interface:   spec.Interface,
				Destination: spec.Destination,
				Via:         spec.Via,
				Metric:      spec.Metric,
			})
		}
	}
	return declarations, nil
}

func validateOutputPath(backend, path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("%s output path is empty", backend)
	}
	if strings.Contains(path, "\x00") {
		return fmt.Errorf("%s output path contains NUL byte", backend)
	}
	return nil
}
