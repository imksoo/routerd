// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/quic-go/quic-go"
)

func TestDoQWireIDAndFINOverAuthenticatedQUIC(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"doq.test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	listener, err := quic.ListenAddr("127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}, NextProtos: []string{"doq"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	peerDone := make(chan error, 1)
	go func() {
		connection, err := listener.Accept(ctx)
		if err != nil {
			peerDone <- err
			return
		}
		stream, err := connection.AcceptStream(ctx)
		if err != nil {
			peerDone <- err
			return
		}
		_ = stream.SetDeadline(time.Now().Add(2 * time.Second))
		// A conforming peer may wait for FIN. io.ReadAll cannot finish while
		// the client keeps its request direction open.
		frame, err := io.ReadAll(io.LimitReader(stream, maxDNSMessageSize+3))
		if err != nil {
			peerDone <- err
			return
		}
		if len(frame) < 14 || int(binary.BigEndian.Uint16(frame)) != len(frame)-2 || frame[2] != 0 || frame[3] != 0 {
			peerDone <- fmt.Errorf("invalid DoQ request frame %x", frame)
			return
		}
		var query dns.Msg
		if err = query.Unpack(frame[2:]); err != nil {
			peerDone <- err
			return
		}
		response := new(dns.Msg)
		response.SetReply(&query)
		wire, err := response.Pack()
		if err == nil {
			err = writeDNSMessage(stream, wire)
		}
		if err == nil {
			err = stream.Close()
		}
		peerDone <- err
	}()
	connection, err := quic.DialAddr(ctx, listener.Addr().String(), &tls.Config{RootCAs: roots, ServerName: "doq.test", NextProtos: []string{"doq"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseWithError(0, "")
	stream, err := connection.OpenStreamSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	query := new(dns.Msg)
	query.SetQuestion("example.test.", dns.TypeA)
	query.Id = 0x1234
	wire, err := query.Pack()
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Clone(wire)
	answer, err := exchangeDoQMessage(ctx, stream, wire)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wire, want) {
		t.Fatal("DoQ transport mutated caller query")
	}
	var response dns.Msg
	if err := response.Unpack(answer); err != nil {
		t.Fatal(err)
	}
	if response.Id != query.Id || !response.Response {
		t.Fatalf("client response identity=%+v", response)
	}
	if err := <-peerDone; err != nil {
		t.Fatal(err)
	}
}
