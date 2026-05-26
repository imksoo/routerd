// SPDX-License-Identifier: BSD-3-Clause

package nixos

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
)

const (
	DefaultModulePath     = "/etc/nixos/routerd-generated.nix"
	DefaultWrapperPath    = "/run/routerd/nixos/routerd-wrapper.nix"
	DefaultBaseConfigPath = "/etc/nixos/configuration.nix"
)

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type Options struct {
	Mode           string
	ModulePath     string
	WrapperPath    string
	BaseConfigPath string
	Command        CommandRunner
	Readlink       func(string) (string, error)
}

type Result struct {
	Mode              string
	ModulePath        string
	WrapperPath       string
	ChangedFiles      []string
	Command           []string
	CommandOutput     string
	GenerationBefore  string
	GenerationAfter   string
	RollbackAttempted bool
	RollbackCommand   []string
	RollbackOutput    string
	RollbackError     string
}

func Apply(ctx context.Context, router *api.Router, opts Options) (Result, error) {
	mode := defaultString(opts.Mode, "switch")
	if mode != "test" && mode != "switch" {
		return Result{}, fmt.Errorf("unsupported NixOS rebuild mode %q", mode)
	}
	modulePath := defaultString(opts.ModulePath, DefaultModulePath)
	wrapperPath := defaultString(opts.WrapperPath, DefaultWrapperPath)
	baseConfigPath := defaultString(opts.BaseConfigPath, DefaultBaseConfigPath)
	command := opts.Command
	if command == nil {
		command = runCommand
	}
	readlink := opts.Readlink
	if readlink == nil {
		readlink = os.Readlink
	}

	module, err := render.NixOSModule(router)
	if err != nil {
		return Result{}, err
	}
	var changed []string
	moduleChanged, err := writeFileIfChanged(modulePath, module, 0644)
	if err != nil {
		return Result{}, err
	}
	if moduleChanged {
		changed = append(changed, modulePath)
	}

	args := []string{mode}
	var wrapper string
	if !baseConfigImports(baseConfigPath, modulePath) {
		wrapper = wrapperPath
		data := []byte(nixosWrapperModule(baseConfigPath, modulePath))
		wrapperChanged, err := writeFileIfChanged(wrapperPath, data, 0644)
		if err != nil {
			return Result{}, err
		}
		if wrapperChanged {
			changed = append(changed, wrapperPath)
		}
		args = append(args, "-I", "nixos-config="+wrapperPath)
	}

	before, _ := readlink("/run/current-system")
	rebuild := nixosRebuildCommand()
	output, err := command(ctx, rebuild, args...)
	after, _ := readlink("/run/current-system")
	result := Result{
		Mode:             mode,
		ModulePath:       modulePath,
		WrapperPath:      wrapper,
		ChangedFiles:     changed,
		Command:          append([]string{rebuild}, args...),
		CommandOutput:    string(output),
		GenerationBefore: before,
		GenerationAfter:  after,
	}
	if err != nil {
		if mode == "switch" && before != "" {
			rollbackArgs := append([]string{"switch", "--rollback"}, args[1:]...)
			result.RollbackAttempted = true
			result.RollbackCommand = append([]string{rebuild}, rollbackArgs...)
			rollbackOutput, rollbackErr := command(ctx, rebuild, rollbackArgs...)
			result.RollbackOutput = string(rollbackOutput)
			if rollbackErr != nil {
				result.RollbackError = fmt.Sprintf("%v", rollbackErr)
				if len(rollbackOutput) > 0 {
					result.RollbackError += "\n" + string(bytes.TrimSpace(rollbackOutput))
				}
			}
			if rollbackAfter, readErr := readlink("/run/current-system"); readErr == nil {
				result.GenerationAfter = rollbackAfter
			}
		}
		if len(output) > 0 {
			return result, fmt.Errorf("nixos-rebuild %s failed: %w\n%s", mode, err, bytes.TrimSpace(output))
		}
		return result, fmt.Errorf("nixos-rebuild %s failed: %w", mode, err)
	}
	return result, nil
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = nixosRebuildEnv(os.Environ())
	return cmd.CombinedOutput()
}

func nixosRebuildCommand() string {
	for _, path := range []string{
		"/run/current-system/sw/bin/nixos-rebuild",
		"/nix/var/nix/profiles/system/sw/bin/nixos-rebuild",
	} {
		if st, err := os.Stat(path); err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			return path
		}
	}
	return "nixos-rebuild"
}

func nixosRebuildEnv(base []string) []string {
	for _, entry := range base {
		if strings.HasPrefix(entry, "NIX_PATH=") {
			return base
		}
	}
	if _, err := os.Stat("/nix/var/nix/profiles/per-user/root/channels/nixos"); err != nil {
		return base
	}
	nixPath := "nixpkgs=/nix/var/nix/profiles/per-user/root/channels/nixos:nixos-config=/etc/nixos/configuration.nix:/nix/var/nix/profiles/per-user/root/channels"
	return append(base, "NIX_PATH="+nixPath)
}

func writeFileIfChanged(path string, data []byte, mode os.FileMode) (bool, error) {
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, data) {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return false, err
	}
	return true, nil
}

func baseConfigImports(baseConfigPath, modulePath string) bool {
	data, err := os.ReadFile(baseConfigPath)
	if err != nil {
		return false
	}
	text := string(data)
	baseName := filepath.Base(modulePath)
	return strings.Contains(text, modulePath) || strings.Contains(text, "./"+baseName) || strings.Contains(text, baseName)
}

func nixosWrapperModule(baseConfigPath, modulePath string) string {
	return fmt.Sprintf(`# Generated by routerd. Do not edit by hand.
{ ... }:

{
  imports = [
    %s
    %s
  ];
}
`, baseConfigPath, modulePath)
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
