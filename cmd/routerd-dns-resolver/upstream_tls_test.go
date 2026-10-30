// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestDoHUsesConfiguredTLSNameForIPServer(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "resolver.test"},
		DNSNames: []string{"resolver.test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true,
	}
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
	var requests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.TLS.ServerName != "resolver.test" {
			t.Errorf("TLS SNI=%q", r.TLS.ServerName)
		}
		if r.URL.Query().Has("serverName") {
			t.Error("local TLS name leaked as DoH HTTP query option")
		}
		if r.URL.Query().Get("endpoint") != "primary" {
			t.Error("DoH endpoint query was lost")
		}
		wire, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var query dns.Msg
		if err := query.Unpack(wire); err != nil {
			t.Error(err)
			return
		}
		failoverWriteDNS(t, w, failoverSuccessfulReply(&query))
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}}
	server.StartTLS()
	defer server.Close()
	for _, name := range []string{"resolver.test", "wrong.test", ""} {
		t.Run("name="+name, func(t *testing.T) {
			values := url.Values{"endpoint": {"primary"}}
			if name != "" {
				values.Set("serverName", name)
			}
			pool, err := newUpstreamPool([]string{server.URL + "/dns-query?" + values.Encode()}, upstreamPoolConfig{QueryTimeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			transport := pool.upstreams[0].client.Transport.(*http.Transport)
			transport.TLSClientConfig.RootCAs = roots
			defer transport.CloseIdleConnections()
			response, err := pool.Exchange(context.Background(), dnsProbeQuery(), nil)
			if name == "resolver.test" {
				if err != nil || len(response) == 0 {
					t.Fatalf("configured TLS name exchange=%x error=%v", response, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "certificate") {
				t.Fatalf("incorrect TLS name accepted or failed before verification: %v", err)
			}
		})
	}
	if requests.Load() != 1 {
		t.Fatalf("verified HTTP requests=%d, want only configured name", requests.Load())
	}
}
