// SPDX-License-Identifier: BSD-3-Clause

package manageddaemon

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

type Spec struct {
	Name              string
	Binary            string
	UnitName          string
	SocketPath        string
	ControlSocketPath string
	StatePath         string
}

func (s Spec) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("managed daemon name is empty")
	}
	if strings.TrimSpace(s.Binary) == "" {
		return fmt.Errorf("managed daemon %s binary is empty", s.Name)
	}
	if strings.TrimSpace(s.SocketPath) == "" {
		return fmt.Errorf("managed daemon %s socket path is empty", s.Name)
	}
	return nil
}

func (s Spec) UnixTarget() string {
	return "unix://" + strings.TrimSpace(s.SocketPath)
}

func (s Spec) ControlSocket() string {
	return strings.TrimSpace(s.ControlSocketPath)
}

func (s Spec) StateFile() string {
	return strings.TrimSpace(s.StatePath)
}

func (s Spec) SocketReady(ctx context.Context, timeout time.Duration) error {
	if err := s.Validate(); err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "unix", s.SocketPath)
	if err != nil {
		return fmt.Errorf("%s socket unavailable at %s: %w", s.Name, s.SocketPath, err)
	}
	_ = conn.Close()
	return nil
}
