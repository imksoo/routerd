// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/servicemgr"
	"golang.org/x/crypto/ssh"
)

type SSHHostKeyController struct {
	Router *api.Router
	DryRun bool
}

func (c SSHHostKeyController) Reconcile(ctx context.Context) error {
	if c.Router == nil || len(c.Router.Spec.SSHHostKeys) == 0 {
		return nil
	}
	const pending = "/var/run/routerd/ssh-host-key-reload"
	_, err := os.Stat(pending)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat SSH reload marker: %w", err)
	}
	needsReload := err == nil
	seen := map[string]bool{}
	for _, spec := range c.Router.Spec.SSHHostKeys {
		kind, key, err := config.SSHHostKey(spec.PrivateKey)
		if err != nil {
			return err
		}
		if seen[kind] {
			return fmt.Errorf("duplicate %s SSH host key", kind)
		}
		seen[kind] = true
		path := "/etc/ssh/ssh_host_" + kind + "_key"
		private := []byte(spec.PrivateKey)
		public := ssh.MarshalAuthorizedKey(key.PublicKey())
		changed, err := sshHostKeyNeedsUpdate(path, private, key.PublicKey())
		if err != nil {
			return err
		}
		if !changed || c.DryRun {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(pending), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(pending, nil, 0600); err != nil {
			return fmt.Errorf("mark SSH reload pending: %w", err)
		}
		if err := writeSSHKey(path, private, 0600); err != nil {
			return err
		}
		if err := writeSSHKey(path+".pub", public, 0644); err != nil {
			return err
		}
		needsReload = true
	}
	if c.DryRun || !needsReload {
		return nil
	}
	out, err := exec.CommandContext(ctx, "sshd", "-t").CombinedOutput()
	if err != nil {
		return fmt.Errorf("validate sshd configuration: %w: %s", err, strings.TrimSpace(string(out)))
	}
	_, features := platform.Current()
	command := servicemgr.ForPlatform(features).Command(servicemgr.OperationReload, servicemgr.Service{SystemdName: "ssh.service", RCDName: "sshd"})
	out, err = exec.CommandContext(ctx, command.Name, command.Args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("reload SSH service: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := os.Remove(pending); err != nil {
		return fmt.Errorf("clear SSH reload marker: %w", err)
	}
	return nil
}

func sshHostKeyNeedsUpdate(path string, desired []byte, desiredPublic ssh.PublicKey) (bool, error) {
	private, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read SSH host key: %w", err)
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat SSH host key: %w", err)
	}
	if info.Mode().Perm() != 0600 {
		return true, nil
	}
	currentKey, err := ssh.ParsePrivateKey(private)
	if err != nil || !bytes.Equal(currentKey.PublicKey().Marshal(), desiredPublic.Marshal()) {
		return true, nil
	}
	currentDER, err := sshPrivateKeyDER(private)
	if err != nil {
		return true, nil
	}
	desiredDER, err := sshPrivateKeyDER(desired)
	if err != nil {
		return false, fmt.Errorf("encode configured SSH host key: %w", err)
	}
	if !bytes.Equal(currentDER, desiredDER) {
		return true, nil
	}
	public, err := os.ReadFile(path + ".pub")
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read SSH host public key: %w", err)
	}
	pubInfo, err := os.Stat(path + ".pub")
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat SSH host public key: %w", err)
	}
	if pubInfo.Mode().Perm()&0022 != 0 || pubInfo.Mode().Perm()&0400 == 0 {
		return true, nil
	}
	key, _, options, rest, err := ssh.ParseAuthorizedKey(public)
	return err != nil || len(options) != 0 || len(bytes.TrimSpace(rest)) != 0 || !bytes.Equal(key.Marshal(), desiredPublic.Marshal()), nil
}

func sshPrivateKeyDER(data []byte) ([]byte, error) {
	key, err := ssh.ParseRawPrivateKey(data)
	if err != nil {
		return nil, err
	}
	if ed, ok := key.(*ed25519.PrivateKey); ok {
		key = *ed
	}
	return x509.MarshalPKCS8PrivateKey(key)
}

func writeSSHKey(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".routerd-ssh-key-*")
	if err != nil {
		return fmt.Errorf("create SSH host key: %w", err)
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write SSH host key: %w", err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("install SSH host key: %w", err)
	}
	return nil
}
