// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"github.com/imksoo/routerd/pkg/api"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/platform"
)

// Every fixture explicitly supplies a native interface identity. These readers
// describe the OS response only; they do not derive addresses from config,
// lease state, or an ownership receipt.
func lanProbeFixture(present func(context.Context, string, string) bool, list func(context.Context, string) ([]string, error), deprecated func(context.Context, string, string) bool) func(context.Context, string, string, bool) (delegatedAddressObservation, error) {
	return func(ctx context.Context, ifname, address string, staged bool) (delegatedAddressObservation, error) {
		observation := delegatedAddressObservation{ifIndex: 7, scope: "lan-test-kernel"}
		if present != nil {
			observation.present = present(ctx, ifname, address)
			observation.usable = observation.present
			observation.deprecated = observation.present
			if observation.present {
				observation.sameHost = []string{address}
			}
			if deprecated != nil {
				observation.deprecated = deprecated(ctx, ifname, address)
			}
			return observation, nil
		}
		if list != nil {
			addresses, err := list(ctx, ifname)
			if err != nil {
				return observation, err
			}
			var out strings.Builder
			for _, value := range addresses {
				out.WriteString("inet6 " + value + "\n")
			}
			parsed, err := parseDelegatedAddressObservation([]byte(out.String()), address, platform.OSLinux, staged)
			parsed.ifIndex, parsed.scope = observation.ifIndex, observation.scope
			return parsed, err
		}
		return observation, nil
	}
}

func lanPresentForTest(controller LANAddressController, ctx context.Context, ifname, address string) bool {
	observation, err := controller.observeCurrentDelegatedAddress(ctx, ifname, address, false)
	return err == nil && observation.present
}

// installLANIOFixture models only the native command/read boundary. Initial
// addresses are explicitly supplied; ownership is separately seeded by tests.
func installLANIOFixture(c *LANAddressController, initial ...string) map[string]bool {
	addresses := map[string]bool{}
	for _, address := range initial {
		addresses[address] = true
	}
	lifetimes := map[string][2]uint32{}
	probe := lanProbeFixture(func(_ context.Context, _ string, address string) bool { return addresses[address] }, nil, nil)
	c.ObserveAddress = func(ctx context.Context, ifname, address string, staged bool) (delegatedAddressObservation, error) {
		observed, err := probe(ctx, ifname, address, staged)
		if lifetime, ok := lifetimes[address]; ok {
			observed.lifetimeKnown = true
			observed.preferredSeconds, observed.validSeconds = lifetime[0], lifetime[1]
		}
		return observed, err
	}
	original := c.Command
	c.Command = func(ctx context.Context, name string, args ...string) error {
		if original != nil {
			if err := original(ctx, name, args...); err != nil {
				return err
			}
		}
		if name == "ip" && len(args) > 3 && args[1] == "addr" {
			if args[2] == "del" {
				delete(addresses, args[3])
			} else {
				addresses[args[3]] = true
			}
		}
		if name == "ifconfig" && len(args) > 3 && args[1] == "inet6" {
			if args[3] == "-alias" {
				for address := range addresses {
					if strings.Split(address, "/")[0] == args[2] {
						delete(addresses, address)
					}
				}
			} else if args[3] == "prefixlen" && len(args) > 4 {
				address := args[2] + "/" + args[4]
				addresses[address] = true
				lifetime := [2]uint32{infinitePDLifetime, infinitePDLifetime}
				for i := 5; i+1 < len(args); i++ {
					if args[i] == "pltime" || args[i] == "vltime" {
						value, err := strconv.ParseUint(args[i+1], 10, 32)
						if err != nil {
							return err
						}
						if args[i] == "pltime" {
							lifetime[0] = uint32(value)
						} else {
							lifetime[1] = uint32(value)
						}
					}
				}
				lifetimes[address] = lifetime
			}
		}
		return nil
	}
	return addresses
}

func recordLANReceiptForTest(t *testing.T, store Store, name, ifname, pdName string, addresses ...string) {
	t.Helper()
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", name)
	if err != nil {
		t.Fatal(err)
	}
	if status == nil {
		status = map[string]any{}
	}
	receipt := delegatedNativeReceipt{IfName: ifname, IfIndex: 7, Scope: "lan-test-kernel", PrefixDelegation: pdName}
	for _, address := range addresses {
		receipt.claim(address, time.Unix(0, 0).UTC(), time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC))
		receipt.confirm(address)
	}
	status["nativeReceipt"] = receipt
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", name, status); err != nil {
		t.Fatal(err)
	}
}
