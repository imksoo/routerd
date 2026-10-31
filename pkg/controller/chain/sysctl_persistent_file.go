// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type persistentSysctlSnapshot struct {
	data []byte
	info os.FileInfo
}

func observePersistentSysctl(path string) (persistentSysctlSnapshot, error) {
	before, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return persistentSysctlSnapshot{}, nil
	}
	if err != nil {
		return persistentSysctlSnapshot{}, err
	}
	if !before.Mode().IsRegular() {
		return persistentSysctlSnapshot{}, errors.New("persistent sysctl path is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return persistentSysctlSnapshot{}, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return persistentSysctlSnapshot{}, err
	}
	if !os.SameFile(before, opened) {
		return persistentSysctlSnapshot{}, errors.New("persistent sysctl file was replaced while opening")
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return persistentSysctlSnapshot{}, err
	}
	after, err := os.Lstat(path)
	if err != nil {
		return persistentSysctlSnapshot{}, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return persistentSysctlSnapshot{}, errors.New("persistent sysctl file changed while reading")
	}
	return persistentSysctlSnapshot{data, after}, nil
}

func samePersistentSysctlSnapshot(a, b persistentSysctlSnapshot) bool {
	if (a.info == nil) != (b.info == nil) {
		return false
	}
	if !bytes.Equal(a.data, b.data) {
		return false
	}
	return a.info == nil || (os.SameFile(a.info, b.info) && a.info.Mode() == b.info.Mode() && sameSysctlFileOwner(a.info, b.info))
}

func findPersistentSysctlBlock(data []byte, owner sysctlPersistentOwner) (string, int, int, error) {
	begin := "# BEGIN routerd sysctl " + owner.Kind + "/" + owner.Name + " token "
	end := "# END routerd sysctl " + owner.Kind + "/" + owner.Name + " token "
	start, finish, offset, count := -1, -1, 0, 0
	var endLine string
	for _, raw := range strings.SplitAfter(string(data), "\n") {
		line := strings.TrimSuffix(raw, "\n")
		if strings.HasPrefix(line, begin) {
			count++
			if count > 1 {
				return "", 0, 0, errors.New("duplicate persistent sysctl owner blocks")
			}
			start, endLine = offset, end+strings.TrimPrefix(line, begin)
		}
		if strings.HasPrefix(line, end) {
			if start < 0 || line != endLine || finish >= 0 {
				return "", 0, 0, errors.New("malformed persistent sysctl owner block")
			}
			finish = offset + len(raw)
		}
		offset += len(raw)
	}
	if start < 0 {
		return "", 0, 0, nil
	}
	if finish < 0 {
		return "", 0, 0, errors.New("unterminated persistent sysctl owner block")
	}
	return string(data[start:finish]), start, finish, nil
}

func replacePersistentSysctlBlock(data []byte, owner sysctlPersistentOwner, expected, desired string) ([]byte, error) {
	current, start, end, err := findPersistentSysctlBlock(data, owner)
	if err != nil {
		return nil, err
	}
	if current != "" && current != expected && !(owner.Pending && current == owner.Block) {
		return nil, errors.New("persistent sysctl block is foreign or edited")
	}
	remaining := data
	if current != "" {
		remaining = append(append([]byte{}, data[:start]...), data[end:]...)
	}
	if desired != "" {
		for _, line := range strings.Split(string(remaining), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if ok && strings.TrimSpace(key) == owner.Key {
				_, desiredValue, _ := strings.Cut(strings.Split(desired, "\n")[1], " = ")
				if normalizeSysctlRuntimeValue(value) != normalizeSysctlRuntimeValue(desiredValue) {
					return nil, fmt.Errorf("persistent sysctl %s conflicts with a setting outside its owned block", owner.Key)
				}
			}
		}
	}
	if current == "" {
		return append([]byte(desired), data...), nil
	}
	next := append(append([]byte{}, data[:start]...), []byte(desired)...)
	return append(next, data[end:]...), nil
}

func (c SysctlController) writePersistentSysctl(ctx context.Context, owner sysctlPersistentOwner, before persistentSysctlSnapshot, next []byte) error {
	if bytes.Equal(before.data, next) {
		if err := c.checkPersistentSysctlOwner(ctx, owner); err != nil {
			return err
		}
		current, err := observePersistentSysctl(owner.Path)
		if err != nil {
			return err
		}
		if !samePersistentSysctlSnapshot(before, current) {
			if err := c.checkPersistentSysctlOwner(ctx, owner); err != nil {
				return err
			}
			return errors.New("persistent sysctl file changed after receipt observation")
		}
		if before.info != nil && owner.Pending {
			directory, err := os.Open(filepath.Dir(owner.Path))
			if err != nil {
				return err
			}
			defer directory.Close()
			return directory.Sync()
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(owner.Path), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(owner.Path), ".routerd-sysctl-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	defer file.Close()
	mode := os.FileMode(0644)
	if before.info != nil {
		mode = before.info.Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
		if err := copySysctlFileOwner(file, before.info); err != nil {
			return err
		}
	}
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if _, err := file.Write(next); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// Receipt/database and temporary-file I/O may wait. Recheck the actual
	// receipt and then the file immediately before the visible replacement.
	if err := c.checkPersistentSysctlOwner(ctx, owner); err != nil {
		return err
	}
	current, err := observePersistentSysctl(owner.Path)
	if err != nil {
		return err
	}
	if !samePersistentSysctlSnapshot(before, current) {
		if err := c.checkPersistentSysctlOwner(ctx, owner); err != nil {
			return err
		}
		return errors.New("persistent sysctl file changed before replacement")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if before.info == nil {
		// A new pathname must be created exclusively; never replace a file
		// introduced by another owner while the intent was being persisted.
		if err := os.Link(temporary, owner.Path); err != nil {
			return err
		}
		if err := os.Remove(temporary); err != nil {
			return err
		}
	} else if err := os.Rename(temporary, owner.Path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(owner.Path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
