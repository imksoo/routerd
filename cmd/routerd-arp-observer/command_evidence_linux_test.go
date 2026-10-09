// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"golang.org/x/sys/unix"
)

// AF_UNIX datagrams exercise the actual write/probe/HTTP paths without opening
// a raw socket or changing the host network.
func commandEvidenceDaemon(t *testing.T) (*daemon, *os.File) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	writer, reader := os.NewFile(uintptr(fds[0]), "probe-writer"), os.NewFile(uintptr(fds[1]), "probe-reader")
	t.Cleanup(func() { _ = writer.Close(); _ = reader.Close() })
	if err := unix.SetsockoptTimeval(fds[1], unix.SOL_SOCKET, unix.SO_RCVTIMEO, &unix.Timeval{Sec: 2}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &daemon{
		opts: options{resource: "arp", ifname: "test", eventInterface: "lan", poolName: "test",
			socketPath: filepath.Join(t.TempDir(), "arp.sock"), prefix: netip.MustParsePrefix("192.0.2.0/24"),
			sourceType: sourceOnDemandARP, onDemand: true, sourceAddress: netip.MustParseAddr("192.0.2.10"),
			selfMAC: mustMAC(t, "02:00:00:00:00:10"), probeRetries: 2, probeTimeout: 20 * time.Millisecond,
			probeCooldown: time.Minute},
		startedAt: time.Now().UTC(), cancel: cancel, ignoredSenderMACsInitialized: true,
		lastProbeAt: map[string]time.Time{}, pendingProbe: map[string]time.Time{},
		activeSocket: &packetSocket{fd: fds[0]},
	}
	errc := make(chan error, 1)
	go func() { errc <- d.serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errc:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(2 * time.Second):
			t.Error("command server did not stop")
		}
	})
	waitForSocket(t, d.opts.socketPath)
	return d, reader
}

func commandEvidence(t *testing.T, d *daemon) commandProbeEvidence {
	t.Helper()
	status := d.status()
	var evidence commandProbeEvidence
	if err := json.Unmarshal([]byte(status.Observed["lastCommandProbe"]), &evidence); err != nil {
		t.Fatal(err)
	}
	if strconv.FormatUint(evidence.Sequence, 10) != status.Observed["commandProbeCount"] {
		t.Fatalf("record/counter mismatch: %#v", status.Observed)
	}
	if status.Resources[0].Observed["lastCommandProbe"] != status.Observed["lastCommandProbe"] {
		t.Fatal("resource and daemon diagnostics differ")
	}
	return evidence
}

func TestCommandProbeEvidenceBindsSuccessfulWritesAndReplacesLastRecord(t *testing.T) {
	d, peer := commandEvidenceDaemon(t)
	if _, exists := d.status().Observed["lastCommandProbe"]; exists {
		t.Fatal("unexecuted command has success evidence")
	}
	for i, address := range []string{"192.0.2.20", "192.0.2.30"} {
		before := time.Now().UTC()
		result := postCommandForTest(t, d.opts.socketPath, daemonapi.CommandRequest{
			Command: "probe-target", Attributes: map[string]string{"target": address},
		})
		after := time.Now().UTC()
		if !result.Accepted || result.Message != "target probe sent" {
			t.Fatalf("command failed: %#v", result)
		}
		evidence := commandEvidence(t, d)
		if evidence.Sequence != uint64(i+1) || evidence.Target != address || evidence.PacketsSent != 3 ||
			evidence.StartedAt.Before(before) || evidence.CompletedAt.Before(evidence.StartedAt) || evidence.CompletedAt.After(after) {
			t.Fatalf("incorrect evidence: %#v", evidence)
		}
		for n := 0; n < evidence.PacketsSent; n++ {
			frame := make([]byte, 1500)
			size, err := peer.Read(frame)
			if err != nil {
				t.Fatal(err)
			}
			packet, ok, err := parseEthernetARP(frame[:size])
			if err != nil || !ok || packet.TargetIP.String() != address || packet.SenderIP != d.opts.sourceAddress {
				t.Fatalf("record does not match actual write: %#v, %v", packet, err)
			}
		}
	}
}

func TestRejectedSuppressedAndFailedCommandsKeepLastSuccessEvidence(t *testing.T) {
	d, peer := commandEvidenceDaemon(t)
	request := func(target string) daemonapi.CommandResult {
		return postCommandForTest(t, d.opts.socketPath, daemonapi.CommandRequest{
			Command: "probe-target", Attributes: map[string]string{"target": target},
		})
	}
	if !request("192.0.2.20").Accepted {
		t.Fatal("initial command failed")
	}
	previous := commandEvidence(t, d)
	for _, target := range []string{"192.0.2.10", "198.51.100.1", "invalid", "192.0.2.20"} {
		result := request(target)
		if target == "192.0.2.20" {
			if !result.Accepted || result.Message != "probe suppressed by cooldown" {
				t.Fatalf("expected cooldown: %#v", result)
			}
		} else if result.Accepted {
			t.Fatalf("invalid target accepted: %s", target)
		}
		if got := commandEvidence(t, d); got != previous {
			t.Fatalf("non-probing command changed success evidence: %#v", got)
		}
	}
	if err := peer.Close(); err != nil {
		t.Fatal(err)
	}
	if result := request("192.0.2.40"); result.Accepted {
		t.Fatal("failed write accepted")
	}
	if got := commandEvidence(t, d); got != previous {
		t.Fatalf("failed command changed success evidence: %#v", got)
	}
}

func TestPartialCommandDoesNotPublishCompletionEvidence(t *testing.T) {
	d, peer := commandEvidenceDaemon(t)
	// Closing the peer after the first actual write makes the next retry fail.
	errc := make(chan error, 1)
	go func() {
		_, err := peer.Read(make([]byte, 1500))
		if err == nil {
			err = peer.Close()
		}
		errc <- err
	}()
	result := postCommandForTest(t, d.opts.socketPath, daemonapi.CommandRequest{
		Command: "probe-target", Attributes: map[string]string{"target": "192.0.2.20"},
	})
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	status := d.status()
	if result.Accepted || status.Observed["probeCount"] != "1" || status.Observed["commandProbeCount"] != "0" || status.Observed["lastCommandProbe"] != "" {
		t.Fatalf("partial command recorded as success: %#v, %#v", result, status.Observed)
	}
}

func TestAutonomousProbeDoesNotCreateCommandEvidence(t *testing.T) {
	d, _ := commandEvidenceDaemon(t)
	d.probeNextPrefixTarget(context.Background(), d.currentPacketSocket())
	status := d.status()
	if status.Observed["proactiveCount"] != "1" || status.Observed["probeCount"] != "3" ||
		status.Observed["commandProbeCount"] != "0" || status.Observed["lastCommandProbe"] != "" {
		t.Fatalf("autonomous work acquired command evidence: %#v", status.Observed)
	}
}

func TestConcurrentProactiveWritesDoNotInflateCommandEvidence(t *testing.T) {
	d, _ := commandEvidenceDaemon(t)
	done := make(chan struct{})
	go func() {
		d.probeNextPrefixTarget(context.Background(), d.currentPacketSocket())
		close(done)
	}()
	result := postCommandForTest(t, d.opts.socketPath, daemonapi.CommandRequest{
		Command: "probe-target", Attributes: map[string]string{"target": "192.0.2.20"},
	})
	<-done
	evidence := commandEvidence(t, d)
	status := d.status()
	if !result.Accepted || evidence.Target != "192.0.2.20" || evidence.PacketsSent != 3 ||
		status.Observed["probeCount"] != "6" || status.Observed["proactiveCount"] != "1" || evidence.Sequence != 1 {
		t.Fatalf("background writes changed command evidence: %#v, %#v", evidence, status.Observed)
	}
}
