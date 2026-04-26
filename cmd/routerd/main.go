package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

	"routerd/pkg/config"
	"routerd/pkg/reconcile"
	statuswriter "routerd/pkg/status"
)

const (
	defaultConfigPath = "/usr/local/etc/routerd/router.yaml"
	defaultPluginDir  = "/usr/local/libexec/routerd/plugins"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return errors.New("missing command")
	}

	switch args[0] {
	case "validate":
		return validateCommand(args[1:], stdout)
	case "observe":
		return configCommand(args[1:], stdout, "observe")
	case "plan":
		return configCommand(args[1:], stdout, "plan")
	case "reconcile":
		return reconcileCommand(args[1:], stdout)
	case "run":
		return configCommand(args[1:], stdout, "run")
	case "status":
		return statusCommand(args[1:], stdout)
	case "plugin":
		return pluginCommand(args[1:], stdout)
	case "help", "-h", "--help":
		usage(stdout)
		return nil
	default:
		usage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func validateCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireExistingFile(*configPath); err != nil {
		return err
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if err := config.Validate(router); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "config %s exists\n", *configPath)
	fmt.Fprintln(stdout, "config is valid")
	return nil
}

func configCommand(args []string, stdout io.Writer, name string) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	statusFile := fs.String("status-file", defaultStatusFile(), "status file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	engine := reconcile.New()
	switch name {
	case "observe":
		result, err := engine.Observe(router)
		if err != nil {
			return err
		}
		return writeResult(stdout, *statusFile, result)
	case "plan":
		result, err := engine.Plan(router)
		if err != nil {
			return err
		}
		return writeResult(stdout, *statusFile, result)
	case "run":
		return errors.New("run is not implemented yet")
	default:
		return fmt.Errorf("unknown config command %s", name)
	}
}

func reconcileCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	statusFile := fs.String("status-file", defaultStatusFile(), "status file")
	once := fs.Bool("once", false, "run one reconcile loop")
	dryRun := fs.Bool("dry-run", false, "plan without applying changes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*once {
		return errors.New("reconcile currently requires --once")
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	engine := reconcile.New()
	result, err := engine.Plan(router)
	if err != nil {
		return err
	}
	if !*dryRun {
		return errors.New("non-dry-run reconcile is not implemented yet")
	}
	fmt.Fprintf(stdout, "dry-run reconcile plan for %s\n", *configPath)
	return writeResult(stdout, *statusFile, result)
}

func statusCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	statusFile := fs.String("status-file", defaultStatusFile(), "status file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "status file: %s\n", *statusFile)
	return nil
}

func pluginCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("missing plugin subcommand")
	}

	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("plugin list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		pluginDir := fs.String("plugin-dir", defaultPluginDir, "plugin directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "plugin listing is not implemented yet for %s\n", *pluginDir)
		return nil
	case "inspect":
		fs := flag.NewFlagSet("plugin inspect", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		pluginDir := fs.String("plugin-dir", defaultPluginDir, "plugin directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("plugin inspect requires a plugin name")
		}
		fmt.Fprintf(stdout, "plugin inspect is not implemented yet for %s in %s\n", fs.Arg(0), *pluginDir)
		return nil
	default:
		return fmt.Errorf("unknown plugin subcommand %q", args[0])
	}
}

func requireExistingFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	return nil
}

func defaultRuntimeDir() string {
	if runtime.GOOS == "freebsd" {
		return "/var/run/routerd"
	}
	return "/run/routerd"
}

func defaultStateDir() string {
	if runtime.GOOS == "freebsd" {
		return "/var/db/routerd"
	}
	return "/var/lib/routerd"
}

func defaultStatusFile() string {
	return defaultRuntimeDir() + "/status.json"
}

func writeResult(stdout io.Writer, statusFile string, result *reconcile.Result) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, string(data))
	if statusFile != "" {
		if err := statuswriter.Write(statusFile, result); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote status %s\n", statusFile)
	}
	return nil
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerd <command> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  validate --config <path>")
	fmt.Fprintln(w, "  observe --config <path>")
	fmt.Fprintln(w, "  plan --config <path>")
	fmt.Fprintln(w, "  reconcile --config <path> --once [--dry-run]")
	fmt.Fprintln(w, "  run --config <path>")
	fmt.Fprintln(w, "  status [--status-file <path>]")
	fmt.Fprintln(w, "  plugin list --plugin-dir <path>")
	fmt.Fprintln(w, "  plugin inspect <plugin-name> --plugin-dir <path>")
}
