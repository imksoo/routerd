// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/imksoo/routerd/pkg/api"
)

type observedLeaseBody struct {
	io.Reader
	read chan struct{}
	once sync.Once
}

func (body *observedLeaseBody) Read(buffer []byte) (int, error) {
	n, err := body.Reader.Read(buffer)
	body.once.Do(func() { close(body.read) })
	return n, err
}

func TestLeaseCallbackWaitsForActualReloadHandoff(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted by selected table", true: "canceled while waiting"}[canceled], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "resolver.json")
			initial := testResolverConfig(nil)
			// No startup socket is needed: reload itself owns the new real DNS
			// listener, while the test holds its existing final commit boundary.
			initial.Spec.Listen = []api.DNSResolverListenSpec{{Port: 1053, AddressFrom: []api.StatusValueSourceSpec{{Resource: "Interface/waiting", Optional: true}}}}
			d := newTestDaemon(t, path, initial, false)
			t.Cleanup(d.shutdownDNSServers)
			port := freeTCPPort(t)
			updated := testResolverConfig([]int{port})
			writeRuntimeConfig(t, path, updated)
			address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
			d.queryHistory.operation.Lock()
			release := sync.OnceFunc(d.queryHistory.operation.Unlock)
			defer release()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reloadDone := make(chan error, 1)
			go func() {
				_, err := d.reload(ctx)
				reloadDone <- err
			}()
			// A response from the new listener proves actual reload has already
			// copied the old dynamic records, before its final table selection.
			assertDNSAnswer(t, address, "router.lab.example.", "192.0.2.1")
			body := &observedLeaseBody{Reader: strings.NewReader(`{"action":"added","ip":"192.0.2.55","hostname":"client"}`), read: make(chan struct{})}
			requestCtx, cancelRequest := context.WithCancel(t.Context())
			defer cancelRequest()
			request := httptest.NewRequest(http.MethodPost, "/v1/leases", body).WithContext(requestCtx)
			response := httptest.NewRecorder()
			leaseDone := make(chan struct{})
			go func() {
				d.routes().ServeHTTP(response, request)
				close(leaseDone)
			}()
			select {
			case <-body.read:
			case <-time.After(3 * time.Second):
				t.Fatal("lease request did not reach its actual decoder")
			}
			if canceled {
				cancelRequest()
			}
			select {
			case <-leaseDone:
				t.Fatal("lease request acknowledged a write while its table was being retired")
			case <-time.After(50 * time.Millisecond):
			}
			release()
			select {
			case err := <-reloadDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("actual reload did not finish its handoff")
			}
			select {
			case <-leaseDone:
			case <-time.After(3 * time.Second):
				t.Fatal("lease request did not finish after table selection")
			}
			answer := namespaceQuery(t, address, "client.lab.example.")
			if canceled {
				if response.Code != http.StatusRequestTimeout || len(answer.Answer) != 0 || answer.Rcode != dns.RcodeNameError {
					t.Fatalf("canceled lease callback mutated selected table: HTTP%d DNS%v", response.Code, answer)
				}
			} else if response.Code != http.StatusOK || len(answer.Answer) != 1 || answer.Answer[0].(*dns.A).A.String() != "192.0.2.55" {
				t.Fatalf("acknowledged lease was lost during actual reload: HTTP%d DNS%v", response.Code, answer)
			}
		})
	}
}
