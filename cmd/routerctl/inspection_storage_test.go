// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const emptyInspectionConfig = "apiVersion: routerd.net/v1alpha1\nkind: Router\nmetadata:\n  name: test\nspec:\n  resources: []\n"

func TestDoctorInspectionDoesNotCreateMissingState(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	if err := os.WriteFile(configPath, []byte(emptyInspectionConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "missing.db")
	var output bytes.Buffer
	if err := run([]string{"doctor", "dns", "--no-host", "--config", configPath, "--state-file", statePath, "-o", "json"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("doctor initialized persistent state during inspection: %v", err)
	}
}

func TestDoctorInspectionReadsLiveWALWithoutWriteAccess(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	if err := os.WriteFile(configPath, []byte(emptyInspectionConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "state.db")
	writer, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := writer.SaveObjectStatus(api.NetAPIVersion, "DNSResolver", "lan", map[string]any{"phase": "Ready"}); err != nil {
		t.Fatal(err)
	}
	_, reader, err := loadDiagnoseInputs(diagnoseOptions{ConfigPath: configPath, StatePath: statePath})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.(io.Closer).Close()
	statuses, err := reader.(routerstate.ObjectStatusLister).ListObjectStatuses()
	if err != nil || len(statuses) != 1 || statuses[0].Status["phase"] != "Ready" {
		t.Fatalf("read-only inspection missed committed live WAL status: %+v, %v", statuses, err)
	}
	if _, err := reader.Set("inspection-must-not-write", "true", "doctor"); err == nil {
		t.Fatal("doctor inspection handle allowed state mutation")
	}
}

func TestAdoptionCandidatesDoNotCreateOwnershipLedger(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	if err := os.WriteFile(configPath, []byte(emptyInspectionConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The real adoption path runs its read-only host inventory. Test-owned
	// empty command observations keep this regression independent of the host.
	for _, name := range []string{"ip", "nft", "systemctl", "sysctl", "hostname", "ifconfig", "pfctl", "service"} {
		writeTestCommand(t, filepath.Join(binDir, name), "#!/bin/sh\nfor arg do\n if [ \"$arg\" = -j ]; then printf '[]'; exit 0; fi\ndone\nexit 0\n")
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ledgerPath := filepath.Join(dir, "missing-ledger.db")
	var output bytes.Buffer
	if err := run([]string{"adopt", "--candidates", "--config", configPath, "--ledger-file", ledgerPath}, &output, io.Discard); err != nil {
		t.Fatalf("read-only adoption inspection: %v", err)
	}
	if _, err := os.Stat(ledgerPath); !os.IsNotExist(err) {
		t.Fatalf("candidate inspection initialized persistent ownership state: %v", err)
	}
}
