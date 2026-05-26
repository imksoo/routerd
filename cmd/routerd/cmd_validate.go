// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/eventlog"
)

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
	for _, warning := range config.Warnings(router) {
		fmt.Fprintf(stdout, "warning: %s\n", warning)
	}
	fmt.Fprintf(stdout, "config %s exists\n", *configPath)
	fmt.Fprintln(stdout, "config is valid")
	return nil
}

func checkCommand(args []string, stdout io.Writer) (err error) {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	statusFile := fs.String("status-file", "", "optional status file for the generated preflight result")
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
	logger, err := eventlog.New(router)
	if err != nil {
		return err
	}
	defer closeLogger(logger, "check", &err)
	opts := applyOptions{
		ConfigPath:          *configPath,
		StatusFile:          *statusFile,
		StatePath:           defaultStatePath,
		LedgerPath:          defaultLedgerPath,
		DryRun:              true,
		SkipServiceManager:  true,
		AnnounceDryRunToCLI: false,
	}
	result, err := runApplyOnce(router, opts, io.Discard, logger)
	if err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(stdout, "warning: %s\n", warning)
	}
	if *statusFile != "" {
		if err := writeResult(io.Discard, *statusFile, result); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "config %s passed preflight check\n", *configPath)
	return nil
}
