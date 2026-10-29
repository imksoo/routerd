// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/platform"
)

type delegatedAddressObservation struct {
	present, usable, deprecated bool
	dadFailed                   bool
	sameHost                    []string
}

func parseDelegatedAddressObservation(out []byte, address string, osName platform.OS, staged bool) (delegatedAddressObservation, error) {
	wanted, err := netip.ParsePrefix(address)
	if err != nil {
		return delegatedAddressObservation{}, err
	}
	var observation delegatedAddressObservation
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] != "inet6" {
				continue
			}
			value := strings.Split(fields[i+1], "%")[0]
			if osName == platform.OSFreeBSD {
				bits := ""
				for j := i + 2; j+1 < len(fields); j++ {
					if fields[j] == "prefixlen" {
						bits = fields[j+1]
						break
					}
				}
				if bits == "" {
					return observation, fmt.Errorf("IPv6 address observation has no prefix length")
				}
				if _, err := strconv.Atoi(bits); err != nil {
					return observation, err
				}
				value += "/" + bits
			}
			actual, err := netip.ParsePrefix(value)
			if err != nil {
				return observation, fmt.Errorf("invalid IPv6 native address observation: %w", err)
			}
			if actual.Addr() != wanted.Addr() {
				continue
			}
			observation.sameHost = append(observation.sameHost, actual.String())
			if actual != wanted {
				continue
			}
			observation.present = true
			failed := fieldsContain(fields, "dadfailed") || fieldsContain(fields, "duplicated")
			observation.dadFailed = failed
			observation.usable = !failed && (!fieldsContain(fields, "tentative") || staged)
			observation.deprecated = fieldsContain(fields, "deprecated") && !failed
		}
	}
	return observation, nil
}

func (c LANAddressController) observeCurrentDelegatedAddress(ctx context.Context, ifname, address string, staged bool) (delegatedAddressObservation, error) {
	ready := c.AddressPresent
	if staged && c.StagingAddressPresent != nil {
		ready = c.StagingAddressPresent
	}
	if ready != nil {
		present := ready(ctx, ifname, address)
		return delegatedAddressObservation{present: present, usable: present, deprecated: true}, nil
	}
	if c.AddressList != nil {
		addresses, err := c.AddressList(ctx, ifname)
		if err != nil {
			return delegatedAddressObservation{}, err
		}
		var out strings.Builder
		for _, candidate := range addresses {
			out.WriteString("inet6 " + candidate + "\n")
		}
		return parseDelegatedAddressObservation([]byte(out.String()), address, platform.OSLinux, staged)
	}
	name, args := "ip", []string{"-6", "-o", "addr", "show", "dev", ifname}
	if c.currentOS() == platform.OSFreeBSD {
		name, args = "ifconfig", []string{ifname}
	}
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return delegatedAddressObservation{}, fmt.Errorf("observe delegated IPv6 address on %s: %w", ifname, err)
	}
	return parseDelegatedAddressObservation(out, address, c.currentOS(), staged)
}
