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
	ifIndex                        int
	scope                          string
	parentToken                    string
	present, usable, deprecated    bool
	dadFailed                      bool
	sameHost                       []string
	lifetimeKnown                  bool
	preferredSeconds, validSeconds uint32
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
			if osName == platform.OSFreeBSD {
				// ifconfig -L omits both values only when both are infinite.
				observation.lifetimeKnown = true
				observation.preferredSeconds, observation.validSeconds = infinitePDLifetime, infinitePDLifetime
				for j := i + 2; j < len(fields); j++ {
					if fields[j] != "pltime" && fields[j] != "vltime" {
						continue
					}
					if j+1 == len(fields) {
						return observation, fmt.Errorf("incomplete IPv6 native lifetime")
					}
					value := uint64(infinitePDLifetime)
					if fields[j+1] != "infty" && fields[j+1] != "infinite" {
						var err error
						value, err = strconv.ParseUint(fields[j+1], 10, 32)
						if err != nil {
							return observation, fmt.Errorf("invalid IPv6 native lifetime: %w", err)
						}
					}
					if fields[j] == "pltime" {
						observation.preferredSeconds = uint32(value)
					} else {
						observation.validSeconds = uint32(value)
					}
					j++
				}
			}
			failed := fieldsContain(fields, "dadfailed") || fieldsContain(fields, "duplicated")
			observation.dadFailed = failed
			observation.usable = !failed && (!fieldsContain(fields, "tentative") || staged)
			observation.deprecated = fieldsContain(fields, "deprecated") && !failed
		}
	}
	return observation, nil
}

func (c LANAddressController) nativeParentReader() nativeParentReader {
 router := c.DeclaredRouter
 if router == nil { router = c.Router }
 return nativeParentReader{Router:router, Store:c.Store, Segments:c.Segments, DSLite:c.DSLite}
}

// Withdrawal follows the parent cookie recorded with the confirmed child,
// including after the parent declaration disappears. Presence without that
// journal/cookie proof remains unknown and cannot authorize a delete.
func (c LANAddressController) observeRecordedDelegatedAddress(ctx context.Context, receipt *delegatedNativeReceipt, address string) (delegatedAddressObservation, error) {
	if receipt.ParentToken == "" {
		return c.observeCurrentDelegatedAddress(ctx, receipt.IfName, address, false)
	}
	owner := c.nativeParentReader()
	before, err := owner.observeRecorded(ctx, receipt.IfName, receipt.ParentToken)
	if err != nil {
		return delegatedAddressObservation{}, err
	}
	if !before.Exists {
		return delegatedAddressObservation{scope: before.Scope}, nil
	}
	observed, err := c.observeDelegatedAddress(ctx, receipt.IfName, address, false)
	if err != nil {
		return observed, err
	}
	after, err := owner.observeRecorded(ctx, receipt.IfName, receipt.ParentToken)
	if err != nil {
		return observed, err
	}
	if !after.Exists || before.Index != after.Index || before.Scope != after.Scope || before.Token != after.Token || observed.ifIndex != after.Index || observed.scope != after.Scope {
		return observed, fmt.Errorf("recorded delegated address parent changed while observing")
	}
	observed.parentToken = after.Token
	return observed, nil
}

func (c LANAddressController) delegatedSegmentParent(ctx context.Context, ifname string) (nativeParentLink, error) {
 link, err := c.nativeParentReader().observeDeclared(ctx, ifname)
 if err != nil { return link, err }
 if link.Declared && (!link.Exists || link.IfName != ifname) { return link, fmt.Errorf("delegated address native parent is not acquired") }
 return link, nil
}

func (c LANAddressController) observeCurrentDelegatedAddress(ctx context.Context, ifname, address string, staged bool) (delegatedAddressObservation, error) {
	before, err := c.delegatedSegmentParent(ctx, ifname)
	if err != nil {
		return delegatedAddressObservation{}, err
	}
	observed, err := c.observeDelegatedAddress(ctx, ifname, address, staged)
	if err != nil {
		return observed, err
	}
	after, err := c.delegatedSegmentParent(ctx, ifname)
	if err != nil {
		return observed, err
	}
	if before.Exists != after.Exists || before.Token != after.Token || before.Index != after.Index || before.Scope != after.Scope {
		return observed, fmt.Errorf("delegated address native parent changed while observing")
	}
	if after.Exists {
		if observed.ifIndex != after.Index || observed.scope != after.Scope {
			return observed, fmt.Errorf("delegated address native parent differs from native interface")
		}
		observed.parentToken = after.Token
	}
	return observed, nil
}

func (c LANAddressController) observeDelegatedAddress(ctx context.Context, ifname, address string, staged bool) (delegatedAddressObservation, error) {
	if c.ObserveAddress != nil {
		return c.ObserveAddress(ctx, ifname, address, staged)
	}
	name, args := "ip", []string{"-6", "-o", "addr", "show", "dev", ifname}
	if c.currentOS() == platform.OSFreeBSD {
		name, args = "ifconfig", []string{"-L", ifname}
	}
	identity, err := (IPv4StaticAddressController{}).staticAddresses(ctx, ifname)
	if err != nil {
		return delegatedAddressObservation{}, err
	}
	if identity.Index == 0 {
		return delegatedAddressObservation{}, nil
	}
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return delegatedAddressObservation{}, fmt.Errorf("observe delegated IPv6 address on %s: %w", ifname, err)
	}
	observation, err := parseDelegatedAddressObservation(out, address, c.currentOS(), staged)
	if err != nil {
		return observation, err
	}
	after, err := (IPv4StaticAddressController{}).staticAddresses(ctx, ifname)
	if err != nil {
		return observation, err
	}
	if identity.Index != after.Index || identity.Scope != after.Scope {
		return observation, fmt.Errorf("delegated address interface identity changed while observing")
	}
	observation.ifIndex, observation.scope = identity.Index, identity.Scope
	return observation, nil
}

// Presence proves the tuple, while the platform's lifetime readback proves the
// requested refresh. A failed or ignored lifetime update must remain pending.
func (c LANAddressController) checkDelegatedLifetimeReadback(observed delegatedAddressObservation, preferred, valid uint32) error {
	if c.currentOS() != platform.OSFreeBSD || !observed.present {
		return nil
	}
	if !observed.lifetimeKnown || observed.preferredSeconds > observed.validSeconds || observed.preferredSeconds > preferred || observed.validSeconds > valid || observed.validSeconds == 0 {
		return fmt.Errorf("delegated IPv6 lifetime readback differs from requested effect")
	}
	return nil
}
