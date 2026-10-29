// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"
)

type httpTestCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	der  []byte
}

func newHTTPTestCA(t *testing.T, name string) httpTestCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return httpTestCA{cert: cert, key: key, der: der}
}

func (ca httpTestCA) pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	return pool
}

func (ca httpTestCA) issue(t *testing.T, client bool) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if client {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.der}, PrivateKey: key, Leaf: leaf}
}

func TestHTTPReloadRevalidatesOldTLSConnectionWithCurrentClientCA(t *testing.T) {
	serverCA, oldCA, newCA := newHTTPTestCA(t, "server"), newHTTPTestCA(t, "old clients"), newHTTPTestCA(t, "new clients")
	oldServer, newServer := serverCA.issue(t, false), serverCA.issue(t, false)
	oldClient, newClient := oldCA.issue(t, true), newCA.issue(t, true)
	owner := &ownedHTTPServer{}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := owner.close(ctx); err != nil {
			t.Error(err)
		}
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "authorized") })
	listen := "127.0.0.1:0"
	first, err := owner.prepare(listen, &servedHTTPConfig{Handler: handler, TLS: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{oldServer}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: oldCA.pool()}})
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()
	if err := first.publish(); err != nil {
		t.Fatal(err)
	}
	address := owner.current.listener.Addr().String()
	oldConnection, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", address, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: serverCA.pool(), ServerName: "localhost", Certificates: []tls.Certificate{oldClient}})
	if err != nil {
		t.Fatal(err)
	}
	defer oldConnection.Close()
	second, err := owner.prepare(listen, &servedHTTPConfig{Handler: handler, TLS: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{newServer}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: newCA.pool()}})
	if err != nil {
		t.Fatal(err)
	}
	defer second.close()
	if err := second.publish(); err != nil {
		t.Fatal(err)
	}
	if err := oldConnection.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(oldConnection, "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(oldConnection), nil)
	if err != nil {
		t.Fatalf("old verified handshake request: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old VerifiedChains retained authority after CA replacement: %d", response.StatusCode)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: serverCA.pool(), ServerName: "localhost", Certificates: []tls.Certificate{newClient}}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	response, err = client.Get("https://" + address + "/")
	if err != nil {
		t.Fatalf("new client certificate did not acquire authority: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || response.TLS.PeerCertificates[0].SerialNumber.Cmp(newServer.Leaf.SerialNumber) != 0 {
		t.Fatalf("new TLS authority/certificate inactive: status=%d state=%+v", response.StatusCode, response.TLS)
	}
}
