// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/dhcpv4client"
	"golang.org/x/sys/unix"
)

// Run the compiled test explicitly inside a disposable network/mount namespace.
// Ordinary go test never creates interfaces or namespaces.
func TestLinuxReceivesChecksumOffloadedOffer(t *testing.T) {
	if os.Getenv("ROUTERD_DHCPV4_OFFLOAD_TEST") != "1" {
		t.Skip("explicit isolated kernel test")
	}
	self, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	init, err := os.Readlink("/proc/1/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 || self == init {
		t.Fatal("requires root in a separate network namespace")
	}
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.CommandContext(t.Context(), "ip", args...).CombinedOutput(); err != nil {
			t.Fatalf("ip %v: %v: %s", args, err, out)
		}
	}
	ns := "rd4offload-" + strconv.Itoa(os.Getpid())
	run("netns", "add", ns)
	t.Cleanup(func() { _ = exec.Command("ip", "netns", "del", ns).Run() })
	run("link", "add", "name", "offload-client", "type", "veth", "peer", "name", "offload-server")
	t.Cleanup(func() { _ = exec.Command("ip", "link", "del", "offload-client").Run() })
	run("link", "set", "offload-server", "netns", ns)
	run("link", "set", "offload-client", "up")
	run("netns", "exec", ns, "ip", "link", "set", "lo", "up")
	run("netns", "exec", ns, "ip", "link", "set", "offload-server", "up")
	run("netns", "exec", ns, "ip", "addr", "add", "192.0.2.1/24", "dev", "offload-server")
	conn, err := listenDHCPv4("offload-client")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ifi, err := net.InterfaceByName("offload-client")
	if err != nil {
		t.Fatal(err)
	}
	packet := daemonReply(t, dhcpv4client.Message{XID: 1234, CHAddr: ifi.HardwareAddr}, dhcpv4client.MessageOffer)
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_CLOEXEC, int(htons(etherTypeIPv4)))
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err = unix.Bind(fd, &unix.SockaddrLinklayer{Protocol: htons(etherTypeIPv4), Ifindex: ifi.Index}); err != nil {
		t.Fatal(err)
	}
	if err = unix.SetsockoptInt(fd, unix.SOL_PACKET, unix.PACKET_AUXDATA, 1); err != nil {
		t.Fatal(err)
	}
	if err = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &unix.Timeval{Sec: 2}); err != nil {
		t.Fatal(err)
	}
	server := `import socket,sys
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM)
s.setsockopt(socket.SOL_SOCKET,socket.SO_BROADCAST,1)
s.setsockopt(socket.SOL_SOCKET,socket.SO_BINDTODEVICE,b'offload-server\0')
s.bind(('192.0.2.1',67))
s.sendto(bytes.fromhex(sys.argv[1]),('255.255.255.255',68))
`
	cmd := exec.CommandContext(t.Context(), "ip", "netns", "exec", ns, "python3", "-c", server, hex.EncodeToString(packet))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("UDP peer: %v: %s", err, out)
	}
	frame, aux := make([]byte, 2048), make([]byte, 128)
	_, oobn, _, _, err := unix.Recvmsg(fd, frame, aux, 0)
	if err != nil {
		t.Fatal(err)
	}
	controls, err := unix.ParseSocketControlMessage(aux[:oobn])
	if err != nil {
		t.Fatal(err)
	}
	var status uint32
	for _, c := range controls {
		if c.Header.Level == unix.SOL_PACKET && c.Header.Type == unix.PACKET_AUXDATA && len(c.Data) >= 20 {
			status = binary.NativeEndian.Uint32(c.Data)
		}
	}
	if status&unix.TP_STATUS_CSUMNOTREADY == 0 {
		t.Skipf("kernel did not supply pending checksum metadata: %#x", status)
	}
	t.Logf("kernel marked independently sent OFFER checksum pending: %#x", status)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2048)
	n, src, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("offloaded OFFER was not delivered: %v", err)
	}
	if !bytes.Equal(buf[:n], packet) || src.IP.String() != "192.0.2.1" || src.Port != 67 {
		t.Fatalf("unexpected received OFFER: source=%v bytes=%d", src, n)
	}
}

func TestPacketUDPChecksumPendingRejectsUnrelatedAncillaryData(t *testing.T) {
	for _, data := range [][]byte{nil, {1, 2, 3}, unix.UnixRights(8, 0, 0, 0, 0)} {
		if packetUDPChecksumPending(data) {
			t.Fatal("unrelated or malformed ancillary data authorized checksum bypass")
		}
	}
}
