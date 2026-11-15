// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
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
		current, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read SSH host key: %w", err)
		}
		info, err := os.Stat(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat SSH host key: %w", err)
		}
		pub, err := os.ReadFile(path + ".pub")
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read SSH host public key: %w", err)
		}
		changed := !bytes.Equal(current, private) || !bytes.Equal(pub, public) || info == nil || info.Mode().Perm() != 0600
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
