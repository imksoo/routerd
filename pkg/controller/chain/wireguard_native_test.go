// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/wireguard"
)

const wgFixturePrivate = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAEA="
const wgFixturePublic = "L+V9o0fNYkMVKNqsX7spBzD/9oSvxM/C7ZCZX1jLO3Q="
const wgFixtureToken = "00112233445566778899aabbccddeeff"

type wireGuardNativeFixture struct {
	controller *WireGuardController
	links      map[string]nativeWireGuardLink
	dumps      map[string]string
	rules      map[string]bool
	mtus       map[string]int
}

// This fixture supplies the kernel/command observations at the existing IO
// collaborators. It does not decide ownership, readiness, or reconciliation.
func withWireGuardNativeFixture(t *testing.T, c *WireGuardController) *wireGuardNativeFixture {
	t.Helper()
	if c.NativeScope == nil {
		c.NativeScope = func(context.Context) (string, error) { return "wg-fixture-kernel", nil }
	}
	f := &wireGuardNativeFixture{controller: c, links: map[string]nativeWireGuardLink{}, dumps: map[string]string{}, rules: map[string]bool{}, mtus: map[string]int{}}
	c.SetAliasByIndex = func(index int, name, proof string) error {
		link, exists := f.links[name]
		if !exists || link.IfIndex != index || link.Kind != "wireguard" || link.Token != "" {
			return fmt.Errorf("acknowledged WireGuard native identity changed")
		}
		var owned bool
		link.Token, owned = strings.CutPrefix(proof, "routerd-wg:")
		if !owned {
			return fmt.Errorf("invalid WireGuard native alias")
		}
		f.links[name] = link
		return nil
	}
	run, stdin := c.Command, c.CommandStdin
	c.Command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "ifconfig" && len(args) == 1 {
			if args[0] == "-l" {
				names := []string{}
				for name, link := range f.links {
					if link.Exists {
						names = append(names, name)
					}
				}
				return []byte(strings.Join(names, " ")), nil
			}
			link, exists := f.links[args[0]]
			if !exists || !link.Exists {
				return nil, fmt.Errorf("interface %s does not exist", args[0])
			}
			flags := "POINTOPOINT"
			if link.Up {
				flags = "UP," + flags
			}
			groups, description := "", ""
			if link.Kind == "wireguard" {
				groups = "wg"
			}
			if link.Token != "" {
				description = "routerd-wg:" + link.Token
			}
			return []byte(fmt.Sprintf("%s: flags=0<%s> mtu %d\n\tdescription: %s\n\tgroups: %s\n", args[0], flags, f.mtus[args[0]], description, groups)), nil
		}
		if name == "ip" && strings.Join(args, " ") == "-j -d link show" {
			links := []map[string]any{}
			for name, link := range f.links {
				flags := []string{}
				if link.Up {
					flags = append(flags, "UP")
				}
				alias := ""
				if link.Token != "" {
					alias = "routerd-wg:" + link.Token
				}
				links = append(links, map[string]any{"ifname": name, "ifindex": link.IfIndex, "ifalias": alias, "flags": flags, "linkinfo": map[string]string{"info_kind": link.Kind}})
			}
			return json.Marshal(links)
		}
		if name == "wg" && len(args) == 3 && args[0] == "show" && args[2] == "dump" {
			if dump, ok := f.dumps[args[1]]; ok {
				return []byte(dump), nil
			}
		}
		if name == "ip" && len(args) == 6 && strings.Join(args[:5], " ") == "-o link show dev" {
			return nil, fmt.Errorf("unexpected MTU query")
		}
		if name == "ip" && len(args) == 5 && strings.Join(args[:4], " ") == "-o link show dev" {
			if mtu := f.mtus[args[4]]; mtu > 0 {
				return []byte(fmt.Sprintf("7: %s: <UP> mtu %d\n", args[4], mtu)), nil
			}
		}
		var out []byte
		var err error
		if run != nil {
			out, err = run(ctx, name, args...)
		}
		if err != nil {
			return out, err
		}
		if name == "ifconfig" && len(args) >= 2 {
			if args[0] == "wg" && args[1] == "create" {
				native, token := "", ""
				for i := 2; i+1 < len(args); i++ {
					if args[i] == "name" {
						native = args[i+1]
					}
					if args[i] == "description" {
						if value, owned := strings.CutPrefix(args[i+1], "routerd-wg:"); owned {
							token = value
						}
					}
				}
				if native == "" || f.links[native].Exists {
					return nil, fmt.Errorf("native WireGuard name is absent or already exists")
				}
				f.links[native] = nativeWireGuardLink{Exists: true, Kind: "wireguard", Token: token}
				f.dumps[native] = "(none)\t(none)\t0\toff\n"
			} else {
				switch args[1] {
				case "destroy":
					delete(f.links, args[0])
					delete(f.dumps, args[0])
					delete(f.mtus, args[0])
				case "up":
					link := f.links[args[0]]
					link.Up = true
					f.links[args[0]] = link
				case "mtu":
					if len(args) == 3 {
						f.mtus[args[0]], err = strconv.Atoi(args[2])
						if err != nil {
							return nil, err
						}
					}
				}
			}
		}
		if name == "ip" && len(args) > 2 {
			if args[0] == "link" && args[1] == "add" {
				native, index := "", 0
				for i := 2; i+1 < len(args); i++ {
					if args[i] == "name" {
						native = args[i+1]
					}
					if args[i] == "index" {
						index, _ = strconv.Atoi(args[i+1])
					}
				}
				if index < 1 {
					return nil, fmt.Errorf("WireGuard creation did not request an exact native index")
				}
				// Linux 6.8 does not install an alias during link creation.
				f.links[native] = nativeWireGuardLink{Exists: true, IfIndex: index, Kind: "wireguard"}
				f.dumps[native] = "(none)\t(none)\t0\toff\n"
			}
			if args[0] == "link" && args[1] == "delete" {
				delete(f.links, args[len(args)-1])
				delete(f.dumps, args[len(args)-1])
			}
			if args[0] == "link" && args[1] == "set" {
				if len(args) == 5 && args[2] == "up" && args[3] == "dev" {
					link := f.links[args[4]]
					link.Up = true
					f.links[args[4]] = link
				}
				for i := 2; i+1 < len(args); i++ {
					if args[i] == "mtu" && i >= 4 {
						f.mtus[args[3]], _ = strconv.Atoi(args[i+1])
					}
				}
			}
		}
		if name == "iptables" && len(args) > 0 {
			key := strings.Join(args[2:], " ")
			if args[0] == "-I" {
				key = strings.Join(args[3:], " ")
			}
			switch args[0] {
			case "-C":
				if !f.rules[key] {
					return []byte("Bad rule (does a matching rule exist in that chain?)"), fmt.Errorf("exit status 1")
				}
			case "-I":
				f.rules[key] = true
			case "-D":
				delete(f.rules, key)
			}
		}
		return out, nil
	}
	c.CommandStdin = func(ctx context.Context, data []byte, name string, args ...string) ([]byte, error) {
		var out []byte
		var err error
		if stdin != nil {
			out, err = stdin(ctx, data, name, args...)
		} else if run != nil {
			out, err = run(ctx, name, args...)
		}
		if err != nil {
			return out, err
		}
		if name == "wg" && len(args) >= 2 && args[0] == "setconf" {
			// Record only the native config text accepted by this IO boundary.
			fields := map[string]string{}
			var peers []map[string]string
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "[Peer]" {
					peers = append(peers, map[string]string{})
					continue
				}
				key, value, ok := strings.Cut(line, "=")
				if !ok {
					continue
				}
				key, value = strings.TrimSpace(key), strings.TrimSpace(value)
				if len(peers) == 0 {
					fields[key] = value
				} else {
					peers[len(peers)-1][key] = value
				}
			}
			pub, err := wireguard.PublicKeyFromPrivateKey(fields["PrivateKey"])
			if err != nil {
				return nil, err
			}
			port, mark := fields["ListenPort"], fields["FwMark"]
			if port == "" {
				port = "0"
			}
			if mark == "" {
				mark = "off"
			}
			dump := fields["PrivateKey"] + "\t" + pub + "\t" + port + "\t" + mark + "\n"
			for _, peer := range peers {
				endpoint, keep, psk := peer["Endpoint"], peer["PersistentKeepalive"], peer["PresharedKey"]
				if endpoint == "" {
					endpoint = "(none)"
				}
				if keep == "" {
					keep = "0"
				}
				if psk == "" {
					psk = "(none)"
				}
				dump += peer["PublicKey"] + "\t" + psk + "\t" + endpoint + "\t" + strings.ReplaceAll(peer["AllowedIPs"], " ", "") + "\t1710000000\t100\t200\t" + keep + "\n"
			}
			f.dumps[args[1]] = dump
		}
		return out, nil
	}
	return f
}

func (f *wireGuardNativeFixture) own(t *testing.T, resource, name string, ports ...int) {
	t.Helper()
	index := 7
	if f.controller.currentOS() == platform.OSFreeBSD {
		index = 0 // The FreeBSD ifconfig observation does not expose an index.
	}
	f.links[name] = nativeWireGuardLink{Exists: true, IfIndex: index, Token: wgFixtureToken, Kind: "wireguard", Up: true}
	scope, err := f.controller.wireGuardScope(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.controller.saveNativeWireGuardRecord(wireGuardNativeRecord{Resource: resource, Name: name, Token: wgFixtureToken, Scope: scope, IfIndex: index, OS: f.controller.currentOS(), Ports: ports}); err != nil {
		t.Fatal(err)
	}
	for _, port := range ports {
		f.rules[strings.Join(wireGuardInputRule("-C", port, wgFixtureToken)[2:], " ")] = true
	}
}
