// SPDX-License-Identifier: BSD-3-Clause

package nixos

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestApplyUsesImportedGeneratedModule(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "configuration.nix")
	module := filepath.Join(dir, "routerd-generated.nix")
	wrapper := filepath.Join(dir, "routerd-wrapper.nix")
	if err := os.WriteFile(base, []byte("{ ... }: { imports = [ ./routerd-generated.nix ]; }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var gotName string
	var gotArgs []string
	result, err := Apply(context.Background(), testRouter(), Options{
		Mode:           "test",
		ModulePath:     module,
		WrapperPath:    wrapper,
		BaseConfigPath: base,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			gotName = name
			gotArgs = append([]string{}, args...)
			return []byte("ok"), nil
		},
		Readlink: func(path string) (string, error) {
			return "/nix/store/current", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "nixos-rebuild" || !reflect.DeepEqual(gotArgs, []string{"test"}) {
		t.Fatalf("command = %s %v", gotName, gotArgs)
	}
	if result.WrapperPath != "" {
		t.Fatalf("wrapper path = %q, want empty", result.WrapperPath)
	}
	if _, err := os.Stat(wrapper); !os.IsNotExist(err) {
		t.Fatalf("wrapper should not be written when base config imports generated module")
	}
}

func TestApplyWritesWrapperWhenGeneratedModuleIsNotImported(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "configuration.nix")
	module := filepath.Join(dir, "routerd-generated.nix")
	wrapper := filepath.Join(dir, "routerd-wrapper.nix")
	if err := os.WriteFile(base, []byte("{ ... }: { }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var gotArgs []string
	result, err := Apply(context.Background(), testRouter(), Options{
		Mode:           "switch",
		ModulePath:     module,
		WrapperPath:    wrapper,
		BaseConfigPath: base,
		Command: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			gotArgs = append([]string{}, args...)
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"switch", "-I", "nixos-config=" + wrapper}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
	data, err := os.ReadFile(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), base) || !strings.Contains(string(data), module) {
		t.Fatalf("wrapper did not import base and generated module:\n%s", data)
	}
	if result.WrapperPath != wrapper {
		t.Fatalf("wrapper path = %q, want %q", result.WrapperPath, wrapper)
	}
}

func TestApplyRejectsUnsupportedMode(t *testing.T) {
	_, err := Apply(context.Background(), testRouter(), Options{Mode: "dry-run"})
	if err == nil || !strings.Contains(err.Error(), "unsupported NixOS rebuild mode") {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyRollsBackFailedSwitch(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "configuration.nix")
	module := filepath.Join(dir, "routerd-generated.nix")
	wrapper := filepath.Join(dir, "routerd-wrapper.nix")
	if err := os.WriteFile(base, []byte("{ ... }: { }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var commands [][]string
	readlinkCalls := 0
	result, err := Apply(context.Background(), testRouter(), Options{
		Mode:           "switch",
		ModulePath:     module,
		WrapperPath:    wrapper,
		BaseConfigPath: base,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			if len(args) >= 1 && args[0] == "switch" && !contains(args, "--rollback") {
				return []byte("activation failed"), errors.New("switch failed")
			}
			return []byte("rolled back"), nil
		},
		Readlink: func(path string) (string, error) {
			readlinkCalls++
			if readlinkCalls == 1 {
				return "/nix/store/generation-old", nil
			}
			if readlinkCalls == 2 {
				return "/nix/store/generation-new", nil
			}
			return "/nix/store/generation-old", nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "activation failed") {
		t.Fatalf("err = %v", err)
	}
	if !result.RollbackAttempted {
		t.Fatal("rollback was not attempted")
	}
	wantRollback := []string{"nixos-rebuild", "switch", "--rollback", "-I", "nixos-config=" + wrapper}
	if !reflect.DeepEqual(result.RollbackCommand, wantRollback) {
		t.Fatalf("rollback command = %#v, want %#v", result.RollbackCommand, wantRollback)
	}
	if result.GenerationAfter != "/nix/store/generation-old" {
		t.Fatalf("generation after rollback = %q", result.GenerationAfter)
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %#v", commands)
	}
}

func TestNixosRebuildEnvPreservesExistingNIXPath(t *testing.T) {
	got := nixosRebuildEnv([]string{"PATH=/bin", "NIX_PATH=nixpkgs=/custom"})
	if !reflect.DeepEqual(got, []string{"PATH=/bin", "NIX_PATH=nixpkgs=/custom"}) {
		t.Fatalf("env = %#v", got)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func testRouter() *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
	}
}
