// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pppoeclient"
)

const freeBSDPPPoEObserveTimeout = time.Second

var freeBSDPPPoEObserveInterval = 500 * time.Millisecond

type freeBSDPPPoEObservation struct {
	CurrentAddress string
	PeerAddress    string
	BytesIn        uint64
	BytesOut       uint64
}

// runFreeBSDPPPoEObserverCommand is injected by unit tests. Production uses
// only base FreeBSD tools and gives each probe a short, independent deadline.
var runFreeBSDPPPoEObserverCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

var freeBSDPPPoEInterfaceExists = func(ctx context.Context, ifname string) (bool, error) {
	_, err := runFreeBSDPPPoEObserverCommand(ctx, "ifconfig", ifname)
	if err == nil {
		return true, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("inspect FreeBSD PPPoE interface %q ownership: %w", ifname, err)
}

func ensureFreeBSDPPPoEInterfaceAbsent(ctx context.Context, ifname string) error {
	exists, err := freeBSDPPPoEInterfaceExists(ctx, ifname)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("refuse to start FreeBSD PPPoE session: target interface %q already exists and is not routerd-owned", ifname)
	}
	return nil
}

var observeFreeBSDPPPoEInterface = func(ctx context.Context, ifname string) (freeBSDPPPoEObservation, error) {
	ifconfigOutput, err := runFreeBSDPPPoEObserverCommand(ctx, "ifconfig", ifname)
	if err != nil {
		return freeBSDPPPoEObservation{}, fmt.Errorf("observe FreeBSD PPPoE interface %q addresses: %w", ifname, err)
	}
	observation, err := parseFreeBSDPPPoEAddresses(string(ifconfigOutput))
	if err != nil {
		return freeBSDPPPoEObservation{}, err
	}
	netstatOutput, err := runFreeBSDPPPoEObserverCommand(ctx, "netstat", "-I", ifname, "-b")
	if err != nil {
		return freeBSDPPPoEObservation{}, fmt.Errorf("observe FreeBSD PPPoE interface %q counters: %w", ifname, err)
	}
	bytesIn, bytesOut, err := parseFreeBSDPPPoECounters(string(netstatOutput), ifname)
	if err != nil {
		return freeBSDPPPoEObservation{}, err
	}
	observation.BytesIn = bytesIn
	observation.BytesOut = bytesOut
	return observation, nil
}

// watchFreeBSDPPPoEInterface observes the kernel-owned point-to-point interface
// because mpd5 emits its runtime diagnostics through syslog rather than the
// daemon's stdout/stderr pipes. It deliberately does not interpret PPP control
// messages as proof that IPCP installed the interface. It stops with the child
// and keeps counters current after the first connected observation.
func (d *daemon) watchFreeBSDPPPoEInterface(cmd *exec.Cmd, ifname string) {
	ticker := time.NewTicker(freeBSDPPPoEObserveInterval)
	defer ticker.Stop()
	for {
		d.mu.Lock()
		active := d.cmd == cmd && d.snapshot.Phase != pppoeclient.PhaseFailed && d.snapshot.Phase != pppoeclient.PhaseDisconnecting
		d.mu.Unlock()
		if !active {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), freeBSDPPPoEObserveTimeout)
		observation, err := observeFreeBSDPPPoEInterface(ctx, ifname)
		cancel()
		if err == nil {
			d.recordFreeBSDPPPoEObservation(cmd, observation)
		}

		<-ticker.C
	}
}

func (d *daemon) recordFreeBSDPPPoEObservation(cmd *exec.Cmd, observation freeBSDPPPoEObservation) {
	if observation.CurrentAddress == "" || observation.PeerAddress == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cmd != cmd || d.snapshot.Phase == pppoeclient.PhaseFailed || d.snapshot.Phase == pppoeclient.PhaseDisconnecting {
		return
	}
	previous := d.snapshot.Phase
	d.snapshot.CurrentAddress = observation.CurrentAddress
	d.snapshot.PeerAddress = observation.PeerAddress
	d.snapshot.BytesIn = observation.BytesIn
	d.snapshot.BytesOut = observation.BytesOut
	d.snapshot.Phase = pppoeclient.PhaseConnected
	d.snapshot.LastError = ""
	d.snapshot.UpdatedAt = time.Now().UTC()
	if d.snapshot.ConnectedAt.IsZero() {
		d.snapshot.ConnectedAt = d.snapshot.UpdatedAt
	}
	_ = d.saveStateLocked()
	if previous != d.snapshot.Phase {
		d.recordPhaseLocked()
		d.publishLocked("routerd.pppoe.client.session.connected", daemonapi.SeverityInfo, pppoeclient.PhaseConnected, "FreeBSD PPPoE interface has assigned local and peer addresses", d.eventAttrsLocked())
	}
}

func parseFreeBSDPPPoEAddresses(output string) (freeBSDPPPoEObservation, error) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != "inet" || fields[2] != "-->" {
			continue
		}
		local, localErr := netip.ParseAddr(fields[1])
		peer, peerErr := netip.ParseAddr(fields[3])
		if localErr == nil && peerErr == nil && local.Is4() && peer.Is4() {
			return freeBSDPPPoEObservation{CurrentAddress: local.String(), PeerAddress: peer.String()}, nil
		}
	}
	return freeBSDPPPoEObservation{}, errors.New("FreeBSD PPPoE interface has no assigned IPv4 point-to-point addresses")
}

func parseFreeBSDPPPoECounters(output, ifname string) (uint64, uint64, error) {
	var inputIndex, outputIndex = -1, -1
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		for index, field := range fields {
			switch field {
			case "Ibytes":
				inputIndex = index
			case "Obytes":
				outputIndex = index
			}
		}
		if inputIndex < 0 || outputIndex < 0 || fields[0] != ifname || len(fields) <= inputIndex || len(fields) <= outputIndex {
			continue
		}
		bytesIn, inputErr := strconv.ParseUint(fields[inputIndex], 10, 64)
		bytesOut, outputErr := strconv.ParseUint(fields[outputIndex], 10, 64)
		if inputErr == nil && outputErr == nil {
			return bytesIn, bytesOut, nil
		}
	}
	return 0, 0, fmt.Errorf("FreeBSD PPPoE interface %q has no parseable byte counters", ifname)
}
