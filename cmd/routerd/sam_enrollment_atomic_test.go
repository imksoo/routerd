// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/samenrollment"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func enrollmentHTTPServer(t *testing.T, router *api.Router, store *routerstate.SQLiteStore, now time.Time) (*httptest.Server, *controlapi.Client) {
	t.Helper()
	server := httptest.NewServer(controlapi.Handler{
		SubmitSAMEnrollmentClaim: func(r *http.Request, req controlapi.SAMEnrollmentClaimSubmitRequest) (*controlapi.SAMEnrollmentClaimSubmitResult, error) {
			return submitSAMEnrollmentClaim(r.Context(), router, store, req, now)
		},
		RevokeSAMEnrollmentClaim: func(r *http.Request, req controlapi.SAMEnrollmentClaimRevokeRequest) (*controlapi.SAMEnrollmentClaimRevokeResult, error) {
			return revokeSAMEnrollmentClaim(r.Context(), router, store, req, now.Add(time.Minute))
		},
		GetSAMEnrollmentTopology: func(r *http.Request, req controlapi.SAMEnrollmentTopologyGetRequest) (*controlapi.SAMEnrollmentTopologyGetResult, error) {
			return getSAMEnrollmentTopologyForAcceptedClaim(r.Context(), router, store, req, now.Add(time.Minute))
		},
	})
	return server, controlapi.NewHTTPClient(server.URL)
}

func enrollmentSignedClaim(t *testing.T, router *api.Router) api.Resource {
	t.Helper()
	secretFile := filepath.Join(t.TempDir(), "join-token")
	if err := os.WriteFile(secretFile, []byte("test-join-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	setSubmitTestJoinToken(t, router, "pve-wg-leaves", secretFile)
	claim := loadSubmitTestClaim(t, "pve-leaf-a")
	spec, err := claim.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatal(err)
	}
	spec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), spec)
	claim.Spec = spec
	return claim
}

func TestSAMEnrollmentHTTPRevocationIsAtomicOnEitherSQLFailureAndRestart(t *testing.T) {
	for _, target := range []string{"ledger", "claim"} {
		t.Run(target, func(t *testing.T) {
			now := time.Date(2026, 6, 28, 0, 1, 0, 0, time.UTC)
			router := loadSubmitTestRouter(t)
			claim := enrollmentSignedClaim(t, router)
			path := filepath.Join(t.TempDir(), "state.db")
			store, err := routerstate.OpenSQLite(path)
			if err != nil {
				t.Fatal(err)
			}
			server, client := enrollmentHTTPServer(t, router, store, now)
			t.Cleanup(func() { server.Close(); _ = store.Close() })
			reopen := func() {
				t.Helper()
				server.Close()
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				store, err = routerstate.OpenSQLite(path)
				if err != nil {
					t.Fatal(err)
				}
				server, client = enrollmentHTTPServer(t, router, store, now)
			}
			if _, err := client.SubmitSAMEnrollmentClaim(t.Context(), controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}); err != nil {
				t.Fatal(err)
			}
			before, err := store.ReadSAMEnrollmentSnapshot(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			faultDB, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer faultDB.Close()
			trigger := `CREATE TRIGGER reject_revoke BEFORE INSERT ON sam_enrollment_revoked_identities BEGIN SELECT RAISE(ABORT,'ledger unavailable'); END`
			if target == "claim" {
				trigger = `CREATE TRIGGER reject_revoke BEFORE UPDATE ON dynamic_config_parts BEGIN SELECT RAISE(ABORT,'claim update unavailable'); END`
			}
			if _, err := faultDB.ExecContext(t.Context(), trigger); err != nil {
				t.Fatal(err)
			}
			_, err = client.RevokeSAMEnrollmentClaim(t.Context(), controlapi.SAMEnrollmentClaimRevokeRequest{Name: claim.Metadata.Name})
			var apiError *controlapi.APIError
			if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusInternalServerError {
				t.Fatalf("SQL failure reported success: %v", err)
			}
			reopen()
			after, err := store.ReadSAMEnrollmentSnapshot(t.Context())
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("failed revoke changed durable state: before=%+v after=%+v err=%v", before, after, err)
			}
			if _, err := client.GetSAMEnrollmentTopology(t.Context(), submitTestTopologyRequest(t, claim)); err != nil {
				t.Fatalf("failed revoke lost accepted claim: %v", err)
			}
			if _, err := faultDB.ExecContext(t.Context(), `DROP TRIGGER reject_revoke`); err != nil {
				t.Fatal(err)
			}
			if result, err := client.RevokeSAMEnrollmentClaim(t.Context(), controlapi.SAMEnrollmentClaimRevokeRequest{Name: claim.Metadata.Name}); err != nil || !result.Revoked {
				t.Fatalf("retry revoke=%+v %v", result, err)
			}
			reopen()
			if _, err := client.GetSAMEnrollmentTopology(t.Context(), submitTestTopologyRequest(t, claim)); err == nil || !strings.Contains(err.Error(), "is revoked") {
				t.Fatalf("reopened revoke lost authority: %v", err)
			}
			rotated := claim
			spec, _ := rotated.SAMEnrollmentClaimSpec()
			spec.JoinNonce += "-rotated"
			spec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), spec)
			rotated.Spec = spec
			if _, err := client.SubmitSAMEnrollmentClaim(t.Context(), controlapi.SAMEnrollmentClaimSubmitRequest{Claim: rotated}); err != nil {
				t.Fatalf("rotated claim rejected: %v", err)
			}
			reopen()
			if _, err := client.GetSAMEnrollmentTopology(t.Context(), submitTestTopologyRequest(t, rotated)); err != nil {
				t.Fatalf("current rotated claim: %v", err)
			}
			replay := claim
			replay.Metadata.Name += "-replay"
			if _, err := client.SubmitSAMEnrollmentClaim(t.Context(), controlapi.SAMEnrollmentClaimSubmitRequest{Claim: replay}); err == nil || !strings.Contains(err.Error(), "was revoked") {
				t.Fatalf("renamed replay after restart=%v", err)
			}
		})
	}
}

func TestSAMEnrollmentTopologyHTTPIsReadOnlyAndRequiresCurrentIdentity(t *testing.T) {
	now := time.Date(2026, 6, 28, 0, 1, 0, 0, time.UTC)
	router := loadSubmitTestRouter(t)
	claim := enrollmentSignedClaim(t, router)
	path := filepath.Join(t.TempDir(), "state.db")
	writer, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := submitSAMEnrollmentClaim(t.Context(), router, writer, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, now); err != nil {
		t.Fatal(err)
	}
	reader, err := routerstate.OpenSQLiteReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	server, client := enrollmentHTTPServer(t, router, reader, now)
	defer server.Close()
	before, err := writer.ReadSAMEnrollmentSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	request := submitTestTopologyRequest(t, claim)
	for range 2 {
		if _, err := client.GetSAMEnrollmentTopology(t.Context(), request); err != nil {
			t.Fatalf("GET needs writable SQLite: %v", err)
		}
	}
	for _, field := range []string{"claim", "identity"} {
		missing := request
		if field == "claim" {
			missing.ClaimDigest = ""
		} else {
			missing.ClaimIdentityDigest = ""
		}
		if _, err := client.GetSAMEnrollmentTopology(t.Context(), missing); err == nil || !strings.Contains(err.Error(), "are required") {
			t.Fatalf("missing %s digest accepted: %v", field, err)
		}
	}
	wrong := request
	wrong.ClaimDigest = "sha256:previous-claim"
	if _, err := client.GetSAMEnrollmentTopology(t.Context(), wrong); err == nil || !strings.Contains(err.Error(), controlapi.SAMEnrollmentTopologyIdentityMismatchMessage) {
		t.Fatalf("stale claim digest returned topology: %v", err)
	}
	after, err := writer.ReadSAMEnrollmentSnapshot(t.Context())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("GET mutated state: %+v %v", after, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = client.GetSAMEnrollmentTopology(t.Context(), request)
	var apiError *controlapi.APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusInternalServerError {
		t.Fatalf("closed SQL reader became an admission observation: %v", err)
	}
}

func TestSAMEnrollmentHTTPRejectsSignatureWhichOmitsDirectMeshFalse(t *testing.T) {
	now := time.Date(2026, 6, 28, 0, 1, 0, 0, time.UTC)
	router := loadSubmitTestRouter(t)
	claim := enrollmentSignedClaim(t, router)
	spec, _ := claim.SAMEnrollmentClaimSpec()
	priorPayload := strings.Replace(samenrollment.JoinCanonicalPayload(spec), "\ndirectMesh=false", "", 1)
	mac := hmac.New(sha256.New, []byte("test-join-token"))
	_, _ = mac.Write([]byte(priorPayload))
	spec.JoinHMAC = hex.EncodeToString(mac.Sum(nil))
	claim.Spec = spec
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server, client := enrollmentHTTPServer(t, router, store, now)
	defer server.Close()
	if _, err := client.SubmitSAMEnrollmentClaim(t.Context(), controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}); err == nil || !strings.Contains(err.Error(), "joinHMAC does not match") {
		t.Fatalf("prior signature payload accepted: %v", err)
	}
	snapshot, err := store.ReadSAMEnrollmentSnapshot(t.Context())
	if err != nil || len(snapshot.Parts) != 0 || len(snapshot.RevokedIdentities) != 0 {
		t.Fatalf("invalid signature wrote admission state: %+v %v", snapshot, err)
	}
}

func TestSAMEnrollmentSQLPublicationRechecksCurrentRowAndGlobalRevocation(t *testing.T) {
	now := time.Date(2026, 6, 28, 0, 1, 0, 0, time.UTC)
	router := loadSubmitTestRouter(t)
	claim := enrollmentSignedClaim(t, router)
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := submitSAMEnrollmentClaim(t.Context(), router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, now); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ReadSAMEnrollmentSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	old := snapshot.Parts[0]
	spec, _ := claim.SAMEnrollmentClaimSpec()
	identity := samenrollment.ClientIdentityDigest(spec)
	newer := old
	newer.Digest = "sha256:newer-validated-observation"
	if err := store.AcceptSAMEnrollmentClaim(t.Context(), newer, identity, old.Digest); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeSAMEnrollmentClaim(t.Context(), old, identity, old.Digest); !errors.Is(err, routerstate.ErrSAMEnrollmentClaimChanged) {
		t.Fatalf("stale revoke overwrote newer row: %v", err)
	}
	if _, err := revokeSAMEnrollmentClaim(t.Context(), router, store, controlapi.SAMEnrollmentClaimRevokeRequest{Name: claim.Metadata.Name}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	renamed := old
	renamed.Source += "-replay"
	if err := store.AcceptSAMEnrollmentClaim(t.Context(), renamed, identity, ""); !errors.Is(err, routerstate.ErrSAMEnrollmentIdentityRevoked) {
		t.Fatalf("stale snapshot bypassed global revocation: %v", err)
	}
	after, err := store.ReadSAMEnrollmentSnapshot(t.Context())
	if err != nil || len(after.Parts) != 1 || !slices.Contains(after.RevokedIdentities, identity) {
		t.Fatalf("SQL replay changed admission: %+v %v", after, err)
	}
}
