package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"time"

	"golang.org/x/sys/unix"

	"routerd/pkg/pdclient"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return errors.New("missing command")
	}
	switch args[0] {
	case "selftest":
		return selftestCommand(args[1:], stdout)
	case "run":
		return runCommand(args[1:], stdout)
	case "help", "-h", "--help":
		usage(stdout)
		return nil
	default:
		usage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	resource := fs.String("resource", "wan-pd", "resource name")
	ifname := fs.String("interface", "", "uplink interface name")
	clientDUIDHex := fs.String("client-duid", "", "client DUID hex; default derives DUID-LL from interface MAC")
	iaid := fs.Uint("iaid", 1, "IA_PD IAID")
	timeout := fs.Duration("timeout", 60*time.Second, "overall acquisition timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *ifname == "" {
		return errors.New("--interface is required")
	}
	ifi, err := net.InterfaceByName(*ifname)
	if err != nil {
		return err
	}
	clientDUID := duidLL(ifi.HardwareAddr)
	if *clientDUIDHex != "" {
		clientDUID, err = parseHex(*clientDUIDHex)
		if err != nil {
			return fmt.Errorf("client DUID: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	conn, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6unspecified, Port: 546})
	if err != nil {
		return err
	}
	defer conn.Close()

	transport := &udpTransport{conn: conn, ifname: *ifname, ifindex: ifi.Index}
	client, err := pdclient.New(pdclient.Config{
		Resource:   *resource,
		Interface:  *ifname,
		ClientDUID: clientDUID,
		IAID:       uint32(*iaid),
	}, transport)
	if err != nil {
		return err
	}

	if err := client.Start(ctx); err != nil {
		return err
	}
	buf := make([]byte, 4096)
	for {
		if client.State == pdclient.StateBound {
			return writeRunResult(stdout, client, transport.sent)
		}
		_ = conn.SetReadDeadline(nextReadDeadline(ctx, 3*time.Second))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if timeoutError(err) && ctx.Err() == nil {
				if client.State == pdclient.StateSoliciting {
					if err := client.Start(ctx); err != nil {
						return err
					}
				}
				continue
			}
			_ = writeRunResult(stdout, client, transport.sent)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		msg, err := pdclient.DecodeMessage(buf[:n])
		if err != nil {
			continue
		}
		if len(msg.ClientDUID) > 0 && hex.EncodeToString(msg.ClientDUID) != hex.EncodeToString(clientDUID) {
			continue
		}
		if err := client.HandleMessage(ctx, msg); err != nil {
			return err
		}
	}
}

func nextReadDeadline(ctx context.Context, interval time.Duration) time.Time {
	deadline := time.Now().Add(interval)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		return ctxDeadline
	}
	return deadline
}

func timeoutError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func writeRunResult(stdout io.Writer, client *pdclient.Client, sent []sentPacket) error {
	result := struct {
		Snapshot pdclient.Snapshot `json:"snapshot"`
		Sent     []sentPacket      `json:"sent"`
	}{
		Snapshot: client.Snapshot(),
		Sent:     sent,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

type udpTransport struct {
	conn    *net.UDPConn
	ifname  string
	ifindex int
	sent    []sentPacket
}

func (t *udpTransport) Send(ctx context.Context, packet pdclient.OutboundPacket) error {
	if deadline, ok := ctx.Deadline(); ok {
		_ = t.conn.SetWriteDeadline(deadline)
	}
	if err := sendDHCPv6Multicast(t.conn, t.ifindex, packet.Payload); err != nil {
		return err
	}
	t.sent = append(t.sent, sentPacket{
		Interface:     t.ifname,
		MessageType:   packet.Message.Type,
		TransactionID: fmt.Sprintf("%06x", packet.Message.TransactionID),
		Bytes:         len(packet.Payload),
	})
	return nil
}

func sendDHCPv6Multicast(conn *net.UDPConn, ifindex int, payload []byte) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var sockErr error
	if err := raw.Write(func(fd uintptr) bool {
		sockErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_MULTICAST_IF, ifindex)
		if sockErr != nil {
			return true
		}
		addr := unix.SockaddrInet6{Port: 547, ZoneId: uint32(ifindex)}
		copy(addr.Addr[:], net.ParseIP("ff02::1:2").To16())
		sockErr = unix.Sendto(int(fd), payload, 0, &addr)
		return !errors.Is(sockErr, unix.EAGAIN) && !errors.Is(sockErr, unix.EWOULDBLOCK)
	}); err != nil {
		return err
	}
	return sockErr
}

func selftestCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("selftest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	resource := fs.String("resource", "wan-pd", "resource name")
	ifname := fs.String("interface", "wan0", "interface name")
	clientDUIDHex := fs.String("client-duid", "00030001020000000103", "client DUID hex")
	serverDUIDHex := fs.String("server-duid", "00030001020000000001", "server DUID hex")
	prefixText := fs.String("prefix", "2001:db8:1200:1240::/60", "delegated prefix")
	if err := fs.Parse(args); err != nil {
		return err
	}
	clientDUID, err := parseHex(*clientDUIDHex)
	if err != nil {
		return fmt.Errorf("client DUID: %w", err)
	}
	serverDUID, err := parseHex(*serverDUIDHex)
	if err != nil {
		return fmt.Errorf("server DUID: %w", err)
	}
	prefix, err := netip.ParsePrefix(*prefixText)
	if err != nil {
		return fmt.Errorf("prefix: %w", err)
	}
	now := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	transport := &captureTransport{}
	xids := []uint32{0x010203, 0x010204}
	client, err := pdclient.New(pdclient.Config{
		Resource:   *resource,
		Interface:  *ifname,
		ClientDUID: clientDUID,
		IAID:       1,
		Now:        func() time.Time { return now },
		Transaction: func() (uint32, error) {
			if len(xids) == 0 {
				return 0x010205, nil
			}
			xid := xids[0]
			xids = xids[1:]
			return xid, nil
		},
	}, transport)
	if err != nil {
		return err
	}
	if err := client.Start(context.Background()); err != nil {
		return err
	}
	advertise, err := pdclient.EncodeMessage(pdclient.Message{
		Type:          pdclient.MessageAdvertise,
		TransactionID: 0x010203,
		ClientDUID:    clientDUID,
		ServerDUID:    serverDUID,
		IAID:          1,
		T1:            7200,
		T2:            12600,
		Prefix:        prefix,
		Preferred:     14400,
		Valid:         14400,
	})
	if err != nil {
		return err
	}
	if err := client.Handle(context.Background(), advertise); err != nil {
		return err
	}
	reply, err := pdclient.EncodeMessage(pdclient.Message{
		Type:          pdclient.MessageReply,
		TransactionID: 0x010204,
		ClientDUID:    clientDUID,
		ServerDUID:    serverDUID,
		IAID:          1,
		T1:            7200,
		T2:            12600,
		Prefix:        prefix,
		Preferred:     14400,
		Valid:         14400,
	})
	if err != nil {
		return err
	}
	if err := client.Handle(context.Background(), reply); err != nil {
		return err
	}
	result := struct {
		Snapshot pdclient.Snapshot `json:"snapshot"`
		Sent     []sentPacket      `json:"sent"`
	}{
		Snapshot: client.Snapshot(),
		Sent:     transport.sent,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

type sentPacket struct {
	Interface     string `json:"interface"`
	MessageType   uint8  `json:"messageType"`
	TransactionID string `json:"transactionID"`
	Bytes         int    `json:"bytes"`
}

type captureTransport struct {
	sent []sentPacket
}

func (t *captureTransport) Send(_ context.Context, packet pdclient.OutboundPacket) error {
	t.sent = append(t.sent, sentPacket{
		Interface:     packet.Interface,
		MessageType:   packet.Message.Type,
		TransactionID: fmt.Sprintf("%06x", packet.Message.TransactionID),
		Bytes:         len(packet.Payload),
	})
	return nil
}

func parseHex(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	out, err := hex.DecodeString(value)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func duidLL(mac net.HardwareAddr) []byte {
	out := make([]byte, 4+len(mac))
	out[1] = 3
	out[3] = 1
	copy(out[4:], mac)
	return out
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerd-pdclient <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  selftest   run an in-process DHCPv6-PD state-machine handshake")
	fmt.Fprintln(w, "  run        send DHCPv6-PD packets on an interface and print the lease snapshot")
}
