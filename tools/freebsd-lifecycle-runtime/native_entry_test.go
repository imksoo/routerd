// SPDX-License-Identifier: BSD-3-Clause

//go:build linux || freebsd

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeEntryOwnsLifecyclePrerequisites(t *testing.T) {
	source, err := os.ReadFile("../../scripts/freebsd-native-vm-smoke.sh")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(source), "prepare_native_descriptors() {\n")
	end := strings.Index(string(source), "\ncleanup_started=0\n")
	if start < 0 || end <= start {
		t.Fatal("native lifecycle prerequisite boundary is missing")
	}
	functions := string(source[start:end])
	for _, scenario := range []struct {
		name string
		body string
	}{
		{"created", `
prepare_native_descriptors
prepare_lifecycle_relay "$relay_binary"
[ -x "$lifecycle_relay" ] && cmp -s "$relay_binary" "$lifecycle_relay"
[ "$native_own_fdescfs" -eq 1 ]
restore_lifecycle_relay
restore_native_descriptors
[ ! -e "$lifecycle_relay" ] && [ ! -d "$lifecycle_relay_parent" ]
[ ! -e "$fixture/mounted" ]
[ "$(cat "$fixture/effects")" = "mount
umount" ]
`},
		{"existing-matching", `
mkdir -p "$lifecycle_relay_dir"
cp "$relay_binary" "$lifecycle_relay"
touch "$fixture/mounted" "$native_fd_dir/9"
prepare_native_descriptors
prepare_lifecycle_relay "$relay_binary"
[ "$native_own_fdescfs" -eq 0 ]
restore_lifecycle_relay
restore_native_descriptors
cmp -s "$relay_binary" "$lifecycle_relay"
[ -e "$fixture/mounted" ] && [ ! -s "$fixture/effects" ]
`},
		{"foreign-relay", `
mkdir -p "$lifecycle_relay_dir"
printf 'foreign executable\n' >"$lifecycle_relay"
cp "$lifecycle_relay" "$fixture/foreign"
if prepare_lifecycle_relay "$relay_binary"; then exit 1; fi
restore_lifecycle_relay
cmp -s "$fixture/foreign" "$lifecycle_relay"
[ ! -e "$fixture/mounted" ] && [ ! -s "$fixture/effects" ]
`},
		{"incomplete-descriptors", `
touch "$fixture/mounted"
if prepare_native_descriptors; then exit 1; fi
restore_native_descriptors
[ ! -e "$lifecycle_relay" ] && [ ! -d "$lifecycle_relay_parent" ]
[ -e "$fixture/mounted" ] && [ ! -s "$fixture/effects" ]
`},
		{"replacement-during-test", `
prepare_native_descriptors
prepare_lifecycle_relay "$relay_binary"
rm "$lifecycle_relay"
printf 'replacement executable\n' >"$lifecycle_relay"
cp "$lifecycle_relay" "$fixture/replacement"
if restore_lifecycle_relay; then exit 1; fi
restore_native_descriptors
cmp -s "$fixture/replacement" "$lifecycle_relay"
[ ! -e "$fixture/mounted" ]
`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			directory := t.TempDir()
			binary := filepath.Join(directory, "relay-binary")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
				t.Fatal(err)
			}
			// Only the OS mount boundary is replaced. File creation, exclusive
			// publication, identity checks and cleanup use the actual script.
			fixture := `
fixture=$1
relay_binary=$2
native_fd_dir=$fixture/fd
lifecycle_relay_parent=$fixture/libexec
lifecycle_relay_dir=$lifecycle_relay_parent/routerd
lifecycle_relay=$lifecycle_relay_dir/dhcp-event-relay
native_own_fdescfs=0
lifecycle_own_relay_parent=0
lifecycle_own_relay_dir=0
lifecycle_relay_stage=
mkdir "$native_fd_dir"
mount() {
  if [ "$1" = -p ]; then
    if [ -e "$fixture/mounted" ]; then
      printf 'fdesc %s fdescfs rw 0 0\n' "$native_fd_dir"
    fi
  else
    printf 'mount\n' >>"$fixture/effects"
    touch "$fixture/mounted" "$native_fd_dir/9"
  fi
}

umount() {
  printf 'umount\n' >>"$fixture/effects"
  rm "$fixture/mounted" "$native_fd_dir/9"
}
`
			command := exec.Command("sh", "-c", "set -eu\n"+functions+fixture+scenario.body, "native-prerequisites", directory, binary)
			if out, err := command.CombinedOutput(); err != nil {
				t.Fatalf("native lifecycle prerequisite ownership failed: %v: %s", err, out)
			}
		})
	}
}

func TestNativeEntryPreparesDescriptorsBeforeDefaultGoGate(t *testing.T) {
	source, err := os.ReadFile("../../scripts/freebsd-native-vm-smoke.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(source)
	functionStart := strings.Index(script, "prepare_native_descriptors() {\n")
	functionEnd := strings.Index(script, "\ncleanup_started=0\n")
	entryStart := strings.Index(script, "trap cleanup EXIT\n")
	entryEnd := strings.Index(script, "\ncat >\"$config\" <<'EOF'\n")
	if functionStart < 0 || functionEnd <= functionStart || entryStart < functionEnd || entryEnd <= entryStart {
		t.Fatal("native Go gate or prerequisite boundary is missing")
	}
	for _, failGate := range []bool{false, true} {
		t.Run(map[bool]string{false: "successful-gate", true: "failed-gate"}[failGate], func(t *testing.T) {
			directory := t.TempDir()
			fixture := `
fixture=$1
fail_gate=$2
native_fd_dir=$fixture/fd
native_own_fdescfs=0
ROUTERD_FREEBSD_LIFECYCLE_RUNTIME=false
export ROUTERD_FREEBSD_LIFECYCLE_RUNTIME
mkdir "$native_fd_dir"
mount() {
  if [ "$1" = -p ]; then
    if [ -e "$fixture/mounted" ]; then printf 'fdesc %s fdescfs rw 0 0\n' "$native_fd_dir"; fi
  else
    printf 'mount\n' >>"$fixture/effects"
    touch "$fixture/mounted" "$native_fd_dir/9"
  fi
}
umount() {
  printf 'umount\n' >>"$fixture/effects"
  rm "$fixture/mounted" "$native_fd_dir/9"
}
cleanup() {
  gate_rc=$?
  restore_native_descriptors || exit 8
  printf 'cleanup\n' >>"$fixture/effects"
  exit "$gate_rc"
}
go() {
  [ -e "$native_fd_dir/9" ] && [ "$native_own_fdescfs" -eq 1 ] || return 9
  printf 'go %s\n' "$*" >>"$fixture/effects"
  [ "$fail_gate" = false ] || return 7
}
`
			flag := "false"
			if failGate {
				flag = "true"
			}
			command := exec.Command("sh", "-c", "set -eu\n"+script[functionStart:functionEnd]+fixture+script[entryStart:entryEnd], "native-entry-order", directory, flag)
			out, err := command.CombinedOutput()
			if failGate {
				if exited, ok := err.(*exec.ExitError); !ok || exited.ExitCode() != 7 {
					t.Fatalf("native Go failure did not reach the established cleanup trap: %v: %s", err, out)
				}
			} else if err != nil {
				t.Fatalf("default Go gate ran without descriptor preparation: %v: %s", err, out)
			}
			effects, err := os.ReadFile(filepath.Join(directory, "effects"))
			if err != nil || !strings.HasPrefix(string(effects), "mount\ngo test ./...\n") || !strings.HasSuffix(string(effects), "umount\ncleanup\n") {
				t.Fatalf("default Go gate lost prerequisite or cleanup ordering: %s %v", effects, err)
			}
			if _, err := os.Stat(filepath.Join(directory, "mounted")); !os.IsNotExist(err) {
				t.Fatalf("default Go gate retained its descriptor mount: %v", err)
			}
		})
	}
}
