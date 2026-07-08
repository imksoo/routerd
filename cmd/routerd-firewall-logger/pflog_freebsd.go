// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/imksoo/routerd/pkg/logstore"
	routerotel "github.com/imksoo/routerd/pkg/otel"
)

const maxBPFDevices = 256

type bpfIfReq struct {
	Name [16]byte
	_    [16]byte
}

func watchPFStateExpireLoop(ctx context.Context, opts options, log *logstore.FirewallLog) {
	interval := 5 * time.Second
	known := map[string]logstore.ExpiredFlowEntry{}
	for {
		current, err := readPFStates(ctx, "pfctl")
		if err == nil {
			for key, flow := range known {
				if _, ok := current[key]; ok {
					continue
				}
				flow.Timestamp = time.Now().UTC()
				if err := log.RecordExpiredFlow(ctx, flow, opts.expiredFlowTTL, opts.expiredFlowLimit); err != nil {
					fmt.Fprintf(os.Stderr, "pf state expire watcher record failed: %v\n", err)
				}
			}
			known = current
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func readPFStates(ctx context.Context, pfctl string) (map[string]logstore.ExpiredFlowEntry, error) {
	cmd := exec.CommandContext(ctx, pfctl, "-ss", "-v")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	states := map[string]logstore.ExpiredFlowEntry{}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		flow, ok := parsePFStateLine(line, time.Now().UTC())
		if !ok {
			continue
		}
		states[expiredFlowKey(flow)] = flow
	}
	return states, scanner.Err()
}

func parsePFStateLine(line string, now time.Time) (logstore.ExpiredFlowEntry, bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "all" {
		return logstore.ExpiredFlowEntry{}, false
	}
	protocol := strings.ToLower(fields[1])
	arrow := -1
	for i, field := range fields {
		if field == "->" {
			arrow = i
			break
		}
	}
	if arrow < 3 || arrow+1 >= len(fields) {
		return logstore.ExpiredFlowEntry{}, false
	}
	left := fields[arrow-1]
	right := fields[arrow+1]
	leftHost, leftPort := splitPFEndpoint(left)
	rightHost, rightPort := splitPFEndpoint(right)
	if leftHost == "" || rightHost == "" {
		return logstore.ExpiredFlowEntry{}, false
	}
	return logstore.ExpiredFlowEntry{
		Timestamp:    now,
		L3Proto:      conntrackL3Proto(leftHost, rightHost),
		Protocol:     protocol,
		OrigSrc:      leftHost,
		OrigSrcPort:  leftPort,
		OrigDst:      rightHost,
		OrigDstPort:  rightPort,
		ReplySrc:     rightHost,
		ReplySrcPort: rightPort,
		ReplyDst:     leftHost,
		ReplyDstPort: leftPort,
		Raw:          line,
	}, true
}

func splitPFEndpoint(value string) (string, int) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "[") {
		end := strings.LastIndex(value, "]:")
		if end >= 0 {
			return strings.Trim(value[:end+1], "[]"), parseInt(value[end+2:])
		}
		return strings.Trim(value, "[]"), 0
	}
	separator := strings.LastIndex(value, ":")
	if separator < 0 {
		return strings.Trim(value, "[]"), 0
	}
	return strings.Trim(value[:separator], "[]"), parseInt(value[separator+1:])
}

func expiredFlowKey(flow logstore.ExpiredFlowEntry) string {
	return fmt.Sprintf("%s|%s|%s|%d|%s|%d", flow.L3Proto, flow.Protocol, flow.OrigSrc, flow.OrigSrcPort, flow.OrigDst, flow.OrigDstPort)
}

func runPflogDaemon(ctx context.Context, opts options, log *logstore.FirewallLog, recorder firewallEntryRecorder, telemetry *routerotel.Runtime) error {
	fd, err := openBPF()
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := attachBPFInterface(fd, opts.pflogInterface); err != nil {
		return err
	}
	_ = unix.IoctlSetInt(fd, unix.BIOCIMMEDIATE, 1)
	bufLen, err := unix.IoctlGetInt(fd, unix.BIOCGBLEN)
	if err != nil || bufLen <= 0 {
		bufLen = 4096
	}
	buf := make([]byte, bufLen)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, err := unix.Read(fd, buf)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return err
		}
		for _, packet := range bpfPackets(buf[:n]) {
			entry, payload, ok := firewallLogEntryAndPayloadFromPflogPacket(packet)
			if !ok {
				continue
			}
			if opts.dpiSocket != "" && len(payload) > 0 {
				entry = enrichEntryWithDPI(ctx, opts, entry, payload)
			}
			if err := recordFirewallEntryWithRecorder(ctx, recorder, log, entry, telemetry, opts); err != nil {
				return err
			}
		}
	}
}

func openBPF() (int, error) {
	if fd, err := unix.Open("/dev/bpf", unix.O_RDONLY, 0); err == nil {
		return fd, nil
	}
	var last error
	for i := 0; i < maxBPFDevices; i++ {
		fd, err := unix.Open(fmt.Sprintf("/dev/bpf%d", i), unix.O_RDONLY, 0)
		if err == nil {
			return fd, nil
		}
		last = err
	}
	return -1, fmt.Errorf("open bpf device: %w", last)
}

func attachBPFInterface(fd int, name string) error {
	var req bpfIfReq
	if len(name) >= len(req.Name) {
		return fmt.Errorf("pflog interface name too long: %s", name)
	}
	copy(req.Name[:], name)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.BIOCSETIF), uintptr(unsafe.Pointer(&req)))
	if errno != 0 {
		return os.NewSyscallError("BIOCSETIF", errno)
	}
	return nil
}

func bpfPackets(buf []byte) [][]byte {
	var out [][]byte
	for len(buf) >= unix.SizeofBpfHdr {
		hdr := (*unix.BpfHdr)(unsafe.Pointer(&buf[0]))
		hdrLen := int(hdr.Hdrlen)
		capLen := int(hdr.Caplen)
		if hdrLen <= 0 || capLen < 0 || len(buf) < hdrLen+capLen {
			break
		}
		out = append(out, buf[hdrLen:hdrLen+capLen])
		next := bpfWordAlign(hdrLen + capLen)
		if next <= 0 || next > len(buf) {
			break
		}
		buf = buf[next:]
	}
	return out
}

func bpfWordAlign(n int) int {
	return (n + 3) &^ 3
}

func firewallLogEntryFromPflogPacket(packet []byte) (logstore.FirewallLogEntry, bool) {
	entry, _, ok := firewallLogEntryAndPayloadFromPflogPacket(packet)
	return entry, ok
}

func firewallLogEntryAndPayloadFromPflogPacket(packet []byte) (logstore.FirewallLogEntry, []byte, bool) {
	if len(packet) < 1 {
		return logstore.FirewallLogEntry{}, nil, false
	}
	headerLen := int(packet[0])
	if headerLen <= 0 || headerLen > len(packet) {
		return logstore.FirewallLogEntry{}, nil, false
	}
	payload := packet[headerLen:]
	entry, ok := firewallLogEntryFromIPPacket(time.Now().UTC(), payload, "pflog-bpf")
	if !ok {
		return logstore.FirewallLogEntry{}, nil, false
	}
	if headerLen >= 6 {
		entry.Action = pflogAction(packet[2])
		entry.RuleName = fmt.Sprintf("pf-rule-%d", binary.BigEndian.Uint16(packet[4:6]))
	}
	return entry, payload, true
}

func pflogAction(action byte) string {
	switch action {
	case 0:
		return "pass"
	case 1:
		return "drop"
	case 2:
		return "scrub"
	default:
		return "drop"
	}
}
