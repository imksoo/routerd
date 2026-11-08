// SPDX-License-Identifier: BSD-3-Clause

package vxlan

import "strconv"

type Config struct {
	Name              string
	IfName            string
	VNI               int
	LocalAddress      string
	Peers             []string
	UnderlayInterface string
	UDPPort           int
	MTU               int
	Bridge            string
	OuterDF           string
}

func Commands(cfg Config) [][]string {
	ifname := defaultString(cfg.IfName, cfg.Name)
	port := cfg.UDPPort
	if port == 0 {
		port = 4789
	}
	args := [][]string{{
		"ip", "link", "add", ifname,
		"type", "vxlan",
		"id", strconv.Itoa(cfg.VNI),
		"local", cfg.LocalAddress,
		"dev", cfg.UnderlayInterface,
		"dstport", strconv.Itoa(port),
		"nolearning",
	}}
	outerDF := cfg.OuterDF
	if outerDF == "" {
		outerDF = "inherit"
	}
	args[0] = append(args[0], "df", outerDF)
	if cfg.MTU != 0 {
		args = append(args, []string{"ip", "link", "set", "dev", ifname, "mtu", strconv.Itoa(cfg.MTU)})
	}
	if cfg.Bridge != "" {
		args = append(args, []string{"ip", "link", "set", "dev", ifname, "master", cfg.Bridge})
	}
	args = append(args, []string{"ip", "link", "set", "dev", ifname, "up"})
	for _, peer := range cfg.Peers {
		args = append(args, []string{"bridge", "fdb", "append", "00:00:00:00:00:00", "dev", ifname, "dst", peer})
	}
	return args
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
