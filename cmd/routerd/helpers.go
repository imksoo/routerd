// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/eventlog"
	statuswriter "github.com/imksoo/routerd/pkg/status"
)

func filepathDir(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx <= 0 {
		return "."
	}
	return path[:idx]
}

func writeFileIfChanged(path string, data []byte, perm os.FileMode) (bool, error) {
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, data) {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return false, statErr
		}
		if info.Mode().Perm() != perm {
			if err := os.Chmod(path, perm); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return false, err
	}
	if err := os.Chmod(path, perm); err != nil {
		return false, err
	}
	return true, nil
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func runLogged(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w: %s", name, args, err, string(out))
	}
	return nil
}

func closeLogger(logger *eventlog.Logger, command string, errp *error) {
	if logger == nil {
		return
	}
	if *errp != nil {
		logger.Emit(eventlog.LevelError, command, "routerd command failed", map[string]string{"error": (*errp).Error()})
	} else {
		logger.Emit(eventlog.LevelInfo, command, "routerd command completed", nil)
	}
	if err := logger.Close(); err != nil && *errp == nil {
		*errp = err
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

func defaultStatusFile() string {
	return platformDefaults.StatusFile()
}

func defaultSocketPath() string {
	return platformDefaults.SocketFile()
}

func defaultStatusSocketPath() string {
	return platformDefaults.StatusSocketFile()
}

func writeResult(stdout io.Writer, statusFile string, result *apply.Result) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if n, err := fmt.Fprintln(stdout, string(data)); err != nil {
		return err
	} else if n != len(data)+1 {
		return io.ErrShortWrite
	}
	if statusFile != "" {
		if err := statuswriter.Write(statusFile, result); err != nil {
			return err
		}
	}
	return nil
}
