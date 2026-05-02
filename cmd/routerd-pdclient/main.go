package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"time"

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
	case "help", "-h", "--help":
		usage(stdout)
		return nil
	default:
		usage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
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

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerd-pdclient <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  selftest   run an in-process DHCPv6-PD state-machine handshake")
}
