// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/imksoo/routerd/pkg/config"
)

func migrateCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	inputPath := fs.String("config", defaultConfigPath, "legacy config path")
	outputPath := fs.String("out", "", "new config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *outputPath == "" {
		return fmt.Errorf("migrate requires --out")
	}
	if filepath.Clean(*inputPath) == filepath.Clean(*outputPath) {
		return fmt.Errorf("migrate refuses to overwrite --config; choose a distinct --out path")
	}
	data, err := os.ReadFile(*inputPath)
	if err != nil {
		return fmt.Errorf("read legacy config %s: %w", *inputPath, err)
	}
	migrated, changes, err := config.MigrateLegacyYAML(data, *inputPath)
	if err != nil {
		return err
	}
	router, err := config.LoadBytes(migrated, *outputPath)
	if err != nil {
		return fmt.Errorf("migrated config is not admitted: %w", err)
	}
	if err := config.Validate(router); err != nil {
		return fmt.Errorf("migrated config is not valid: %w", err)
	}
	if err := os.WriteFile(*outputPath, migrated, 0o600); err != nil {
		return fmt.Errorf("write migrated config %s: %w", *outputPath, err)
	}
	for _, change := range changes {
		fmt.Fprintf(stdout, "migrated %s %s: %s\n", change.Resource, change.Field, change.Reason)
	}
	fmt.Fprintf(stdout, "wrote validated migrated config %s (%d changes)\n", *outputPath, len(changes))
	return nil
}
