// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"flag"
	"fmt"
	"strings"
)

// printSubcommandHelp renders a help message for a single subcommand FlagSet.
//
// Format:
//
//	Usage: routerctl <name> [flags]
//
//	<summary>
//
//	Flags:
//	  <fs.PrintDefaults output>
//
//	Examples:
//	  <examples>
//
// Output is written to fs.Output(), which the caller can redirect for tests
// via fs.SetOutput().
func printSubcommandHelp(fs *flag.FlagSet, summary, examples string) {
	w := fs.Output()
	fmt.Fprintf(w, "Usage: routerctl %s [flags]\n", fs.Name())
	if strings.TrimSpace(summary) != "" {
		fmt.Fprintln(w)
		for _, line := range strings.Split(strings.TrimRight(summary, "\n"), "\n") {
			fmt.Fprintln(w, line)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	// PrintDefaults already writes to fs.Output(). Each entry begins with
	// "  -flag" so it lines up under the "Flags:" heading.
	fs.PrintDefaults()
	if strings.TrimSpace(examples) != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Examples:")
		for _, line := range strings.Split(strings.TrimRight(examples, "\n"), "\n") {
			if strings.TrimSpace(line) == "" {
				fmt.Fprintln(w)
				continue
			}
			if strings.HasPrefix(line, "  ") {
				fmt.Fprintln(w, line)
				continue
			}
			fmt.Fprintln(w, "  "+line)
		}
	}
}
