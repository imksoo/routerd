// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"routerd/pkg/api"
)

type CARPOptions struct {
	PriorityByResource map[string]int
}

type CARPConfigData struct {
	Interfaces []CARPInterface
	Preempt    bool
}

type CARPInterface struct {
	Name          string
	Interface     string
	Address       string
	Family        string
	VirtualHostID int
	AdvBase       int
	AdvSkew       int
	Password      string
}

func CARPConfig(router *api.Router, aliases map[string]string) (CARPConfigData, error) {
	return CARPConfigWithOptions(router, aliases, CARPOptions{})
}

func CARPConfigWithOptions(router *api.Router, aliases map[string]string, opts CARPOptions) (CARPConfigData, error) {
	instances, err := vrrpInstances(router, aliases, KeepalivedOptions{PriorityByResource: opts.PriorityByResource})
	if err != nil {
		return CARPConfigData{}, err
	}
	config := CARPConfigData{}
	for _, instance := range instances {
		if instance.Preempt != nil && *instance.Preempt {
			config.Preempt = true
		}
		config.Interfaces = append(config.Interfaces, CARPInterface{
			Name:          instance.Name,
			Interface:     instance.Interface,
			Address:       instance.Address,
			Family:        instance.Family,
			VirtualHostID: instance.VirtualRouterID,
			AdvBase:       keepalivedAdvertSeconds(instance.AdvertInterval),
			AdvSkew:       carpAdvSkew(instance.Priority),
			Password:      strings.TrimSpace(instance.Authentication),
		})
	}
	sort.Slice(config.Interfaces, func(i, j int) bool {
		if config.Interfaces[i].Interface == config.Interfaces[j].Interface {
			return config.Interfaces[i].VirtualHostID < config.Interfaces[j].VirtualHostID
		}
		return config.Interfaces[i].Interface < config.Interfaces[j].Interface
	})
	return config, nil
}

func (c CARPConfigData) IfconfigCommands() [][]string {
	var commands [][]string
	for _, iface := range c.Interfaces {
		args := []string{
			iface.Interface,
			carpAddressFamily(iface.Family),
			"vhid", strconv.Itoa(iface.VirtualHostID),
			"advbase", strconv.Itoa(iface.AdvBase),
			"advskew", strconv.Itoa(iface.AdvSkew),
		}
		if iface.Password != "" {
			args = append(args, "pass", iface.Password)
		}
		args = append(args, "alias", iface.Address)
		commands = append(commands, args)
	}
	return commands
}

func (c CARPConfigData) RCConfLines() []string {
	var lines []string
	lines = append(lines, "routerd_carp_enable=\"YES\"")
	if len(c.Interfaces) == 0 {
		return lines
	}
	indexByInterface := map[string]int{}
	for _, iface := range c.Interfaces {
		index := indexByInterface[iface.Interface]
		indexByInterface[iface.Interface]++
		lines = append(lines, fmt.Sprintf("ifconfig_%s_alias%d=\"%s\"", iface.Interface, index, strings.Join(iface.rcConfArgs(), " ")))
	}
	return lines
}

func (c CARPConfigData) PreemptSysctlValue() string {
	if c.Preempt {
		return "1"
	}
	return "0"
}

func (i CARPInterface) rcConfArgs() []string {
	args := []string{
		carpAddressFamily(i.Family),
		"vhid", strconv.Itoa(i.VirtualHostID),
		"advbase", strconv.Itoa(i.AdvBase),
		"advskew", strconv.Itoa(i.AdvSkew),
	}
	if i.Password != "" {
		args = append(args, "pass", i.Password)
	}
	args = append(args, "alias", i.Address)
	return args
}

func carpAddressFamily(family string) string {
	if family == "ipv6" {
		return "inet6"
	}
	return "inet"
}

func carpAdvSkew(priority int) int {
	if priority <= 0 {
		priority = 100
	}
	if priority > 254 {
		priority = 254
	}
	skew := 254 - priority
	if skew < 1 {
		return 1
	}
	if skew > 254 {
		return 254
	}
	return skew
}
