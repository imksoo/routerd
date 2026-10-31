//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/samenrollment"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestSAMRRLeafProcessesAdmissionExpiryAndRevocation(t *testing.T) {
	if root := os.Getenv("ROUTERD_SAM_PROCESS_ROOT"); root != "" {
		err := run([]string{"serve", "--sandbox", "--root", root,
			"--config", filepath.Join(root, "router.yaml"),
			"--state-file", filepath.Join(root, "state.db"),
			"--ledger-file", filepath.Join(root, "ledger.db"),
			"--socket", filepath.Join(root, "control.sock"),
			"--status-socket", filepath.Join(root, "status.sock"),
			"--http-listen", "127.0.0.1:0",
			"--controllers", "sam-enrollment-client",
			"--apply-interval", "0", "--graceful-stop-timeout", "0"}, io.Discard, os.Stderr)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	// Short owned paths keep both real Unix HTTP endpoints below sun_path.
	dir, err := os.MkdirTemp("", "rdsam-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	var logs []string
	t.Cleanup(func() {
		if t.Failed() {
			for _, path := range logs {
				data, _ := os.ReadFile(path)
				t.Logf("%s:\n%s", filepath.Base(path), data)
			}
		}
	})
	rr, leaf := filepath.Join(dir, "rr"), filepath.Join(dir, "leaf")
	for _, root := range []string{rr, leaf} {
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig := func(root, data string) *api.Router {
		t.Helper()
		path := filepath.Join(root, "router.yaml")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		router, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := config.ValidateForOS(router, platform.OSLinux); err != nil {
			t.Fatalf("actual %s configuration admission: %v", filepath.Base(root), err)
		}
		return router
	}
	writeConfig(rr, samProcessRRConfig)
	leafConfig := func(nonce string) string {
		return fmt.Sprintf(samProcessLeafConfig, nonce, filepath.Join(rr, "control.sock"))
	}
	leafRouter := writeConfig(leaf, leafConfig("identity-one"))
	await := func(label string, check func() error) {
		t.Helper()
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		var last error
		for {
			if last = check(); last == nil {
				return
			}
			select {
			case <-ticker.C:
			case <-deadline.C:
				t.Fatalf("%s: %v", label, last)
			case <-ctx.Done():
				t.Fatalf("%s: %v; last=%v", label, ctx.Err(), last)
			}
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	start := func(role, root string) (*exec.Cmd, func()) {
		t.Helper()
		starts++
		logPath := filepath.Join(dir, fmt.Sprintf("%02d-%s.log", starts, role))
		log, err := os.Create(logPath)
		if err != nil {
			t.Fatal(err)
		}
		logs = append(logs, logPath)
		cmd := exec.Command(executable, "-test.run=^TestSAMRRLeafProcessesAdmissionExpiryAndRevocation$", "-test.timeout=90s")
		for _, value := range os.Environ() {
			key, _, _ := strings.Cut(value, "=")
			if !strings.HasPrefix(key, "OTEL_") && !strings.HasPrefix(key, "ROUTERD_") && key != "NOTIFY_SOCKET" {
				cmd.Env = append(cmd.Env, value)
			}
		}
		cmd.Env = append(cmd.Env, "ROUTERD_SAM_PROCESS_ROOT="+root)
		cmd.Stdout, cmd.Stderr = log, log
		if err := cmd.Start(); err != nil {
			log.Close()
			t.Fatal(err)
		}
		done := make(chan struct{})
		var waitErr error
		go func() { waitErr = cmd.Wait(); log.Close(); close(done) }()
		t.Cleanup(func() {
			select {
			case <-done:
				return
			default:
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Error(err)
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("owned SAM daemon did not exit after kill")
			}
		})
		stop := func() {
			t.Helper()
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
				if waitErr != nil {
					t.Fatalf("%s process stop: %v", role, waitErr)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("%s process did not acknowledge stop", role)
			}
			t.Logf("%s pid=%d reaped", role, cmd.Process.Pid)
		}
		client := controlapi.NewUnixClient(filepath.Join(root, "control.sock"))
		await(role+" actual control API", func() error {
			select {
			case <-done:
				return fmt.Errorf("process exited: %v", waitErr)
			default:
			}
			call, stop := context.WithTimeout(ctx, 200*time.Millisecond)
			defer stop()
			_, err := client.Status(call)
			return err
		})
		t.Logf("%s pid=%d actual control API ready", role, cmd.Process.Pid)
		return cmd, stop
	}
	snapshot := func(root, source string) (dynamicconfig.DynamicConfigPart, map[string]any, error) {
		reader, err := routerstate.OpenSQLiteReadOnly(filepath.Join(root, "state.db"))
		if err != nil {
			return dynamicconfig.DynamicConfigPart{}, nil, err
		}
		defer reader.Close()
		records, err := reader.GetDynamicConfigPartsBySource(source)
		if err != nil || len(records) != 1 {
			return dynamicconfig.DynamicConfigPart{}, nil, fmt.Errorf("source %s records=%d: %v", source, len(records), err)
		}
		part, err := codec.Decode(records[0])
		status, statusErr := reader.ReadObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "leaf-a")
		return part, status, errors.Join(err, statusErr)
	}
	liveLeaf := func() error {
		part, status, err := snapshot(leaf, "SAMRRSet/rrs")
		if err != nil {
			return err
		}
		if status["phase"] != "Ready" || !part.Spec.ExpiresAt.After(time.Now()) || len(part.Spec.Resources) != 1 {
			return fmt.Errorf("leaf has no current RR result: status=%v expires=%v resources=%d", status, part.Spec.ExpiresAt, len(part.Spec.Resources))
		}
		rrSet, err := part.Spec.Resources[0].SAMRRSetSpec()
		if err != nil || len(rrSet.Nodes) != 1 || rrSet.Nodes[0].NodeRef != "rr-a" || !rrSet.Nodes[0].RouteReflector {
			return fmt.Errorf("wrong actual topology: %+v %v", rrSet, err)
		}
		return nil
	}
	_, stopRR := start("rr", rr)
	firstLeaf, stopLeaf := start("leaf", leaf)
	await("admitted leaf result", liveLeaf)
	accepted, _, err := snapshot(rr, "SAMEnrollmentClaim/leaf-a")
	if err != nil || len(accepted.Spec.Resources) != 1 {
		t.Fatalf("RR did not persist the received claim: %+v %v", accepted, err)
	}
	claim, err := accepted.Spec.Resources[0].SAMEnrollmentClaimSpec()
	if err != nil || claim.LeafID != "leaf-a" || claim.JoinNonce != "identity-one" || claim.Revoked {
		t.Fatalf("accepted remote claim: %+v %v", claim, err)
	}
	stopLeaf()
	secondLeaf, stopLeaf := start("leaf-reopen", leaf)
	if secondLeaf.Process.Pid == firstLeaf.Process.Pid {
		t.Fatal("leaf did not reopen in an independent process")
	}
	await("leaf reopened actual SQLite", liveLeaf)
	stopRR()
	lease, _, err := snapshot(leaf, "SAMRRSet/rrs")
	if err != nil {
		t.Fatal(err)
	}
	await("real lease expiry while RR is stopped", func() error {
		part, status, err := snapshot(leaf, "SAMRRSet/rrs")
		if err != nil {
			return err
		}
		if !part.Spec.ExpiresAt.Equal(lease.Spec.ExpiresAt) || part.Spec.ExpiresAt.After(time.Now()) || status["phase"] == "Ready" {
			return fmt.Errorf("unavailable RR retained current admission: status=%v expires=%v", status, part.Spec.ExpiresAt)
		}
		return nil
	})
	_, stopRR = start("rr-reopen", rr)
	await("RR return renews actual admission", liveLeaf)
	rrClient := controlapi.NewUnixClient(filepath.Join(rr, "control.sock"))
	result, err := rrClient.RevokeSAMEnrollmentClaim(ctx, controlapi.SAMEnrollmentClaimRevokeRequest{Name: "leaf-a", Reason: "private process qualification"})
	if err != nil || !result.Revoked {
		t.Fatalf("actual RR revoke: %+v %v", result, err)
	}
	original := leafRouter.Spec.Resources[0]
	if _, err := rrClient.SubmitSAMEnrollmentClaim(ctx, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: original}); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("RR accepted revoked client identity: %v", err)
	}
	await("revoked leaf cannot keep a live admission", func() error {
		part, status, err := snapshot(leaf, "SAMRRSet/rrs")
		if err != nil {
			return err
		}
		if part.Spec.ExpiresAt.After(time.Now()) || status["phase"] == "Ready" {
			return fmt.Errorf("revoked admission is still current: %v %v", status, part.Spec.ExpiresAt)
		}
		return nil
	})
	stopRR()
	_, stopRR = start("rr-revocation-reopen", rr)
	if _, err := rrClient.SubmitSAMEnrollmentClaim(ctx, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: original}); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("RR reopen forgot permanent identity revocation: %v", err)
	}
	stopLeaf()
	writeConfig(leaf, leafConfig("identity-two"))
	_, stopLeaf = start("leaf-new-identity", leaf)
	await("explicit new client identity is admitted", liveLeaf)
	reaccepted, _, err := snapshot(rr, "SAMEnrollmentClaim/leaf-a")
	if err != nil || len(reaccepted.Spec.Resources) != 1 {
		t.Fatalf("new admission is absent: %v", err)
	}
	newClaim, err := reaccepted.Spec.Resources[0].SAMEnrollmentClaimSpec()
	if err != nil || newClaim.JoinNonce != "identity-two" || samenrollment.ClientIdentityDigest(newClaim) == samenrollment.ClientIdentityDigest(claim) {
		t.Fatalf("wrong re-admitted identity: %+v %v", newClaim, err)
	}
	stopLeaf()
	stopRR()
	for _, root := range []string{rr, leaf} {
		for _, name := range []string{"control.sock", "status.sock"} {
			if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
				t.Fatalf("owned endpoint remains after acknowledged process exit: %s %v", name, err)
			}
		}
	}
	t.Log("two real serve processes: admission, leaf reopen, RR outage/expiry/recovery, revoke, durable revocation and explicit identity rotation; all owned processes reaped")
}

const samProcessRRConfig = `apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: rr-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: {name: underlay}
      spec: {ifname: lo, managed: false}
    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPRouter
      metadata: {name: core}
      spec: {asn: 64512, routerID: 198.18.128.1}
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMNodeSet
      metadata: {name: rrnodes}
      spec:
        nodes:
          - {nodeRef: rr-a, role: cloud, routeReflector: true, samEndpoint: 198.18.129.1}
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMTransportProfile
      metadata: {name: rr}
      spec:
        selfNodeRef: rr-a
        mode: ipip
        encryption: none
        addressingMode: pair-stable
        innerPrefix: 198.18.128.0/24
        underlayInterface: underlay
        localEndpoint: 198.18.129.1
        peersFrom: [{resource: SAMEnrollmentPolicy/leaves}]
        bgp: {routerRef: BGPRouter/core, peerASN: 64512, generatePeers: false}
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMEnrollmentPolicy
      metadata: {name: leaves}
      spec:
        transportProfileRef: SAMTransportProfile/rr
        rrSetRef: SAMRRSet/rrs
        rrNodeSetRef: SAMNodeSet/rrnodes
        tunnelAddressPrefixes: [198.18.128.0/24]
        endpointPrefixes: [198.18.129.0/24]
        ttl: 8s
`

const samProcessLeafConfig = `apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: leaf-a}
spec:
  resources:
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMEnrollmentClaim
      metadata: {name: leaf-a}
      spec:
        policyRef: SAMEnrollmentPolicy/leaves
        rrSetRef: SAMRRSet/rrs
        leafID: leaf-a
        joinNonce: %s
        tunnelAddress: 198.18.128.2/32
        endpoint: 198.18.129.2
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMEnrollmentClient
      metadata: {name: leaf-a}
      spec:
        claimRef: SAMEnrollmentClaim/leaf-a
        rrSocket: %q
        stateTTLRefreshBefore: 2s
        retryBackoff: {min: 2s, max: 2s}
`
