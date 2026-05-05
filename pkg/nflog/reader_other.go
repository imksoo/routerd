//go:build !linux

package nflog

import (
	"context"
	"fmt"
	"time"
)

type Packet struct {
	Prefix      string
	Timestamp   time.Time
	SrcAddress  string
	SrcPort     int
	DstAddress  string
	DstPort     int
	Protocol    string
	L3Proto     string
	InIface     string
	OutIface    string
	PacketBytes int
	Payload     []byte
}

type Reader struct{}

func Open(group int) (*Reader, error) {
	return nil, fmt.Errorf("NFLOG is only supported on Linux")
}

func (r *Reader) Close() error {
	return nil
}

func (r *Reader) Read(ctx context.Context) (Packet, error) {
	return Packet{}, fmt.Errorf("NFLOG is only supported on Linux")
}
