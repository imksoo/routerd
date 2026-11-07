// SPDX-License-Identifier: BSD-3-Clause

//go:build linux || freebsd

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/platform"
)

type interfaceReceipt struct {
	Name  string `json:"name"`
	Index int    `json:"index"`
	MAC   string `json:"mac"`
	Scope string `json:"scope"`
	Token string `json:"token"`
}

type epairReceipt struct {
	A interfaceReceipt `json:"a"`
	B interfaceReceipt `json:"b"`
}

func observeInterface(ctx context.Context, name string) (interfaceReceipt, bool, error) {
	var found interfaceReceipt
	scope, err := platform.KernelNetworkScope(ctx)
	if err != nil {
		return found, false, err
	}
	links, err := net.Interfaces()
	if err != nil {
		return found, false, err
	}
	for _, link := range links {
		if link.Name != name {
			continue
		}
		found = interfaceReceipt{Name: name, Index: link.Index, MAC: link.HardwareAddr.String(), Scope: scope}
		out, err := exec.CommandContext(ctx, "ifconfig", name).CombinedOutput()
		if err != nil {
			return found, false, fmt.Errorf("ifconfig observation: %w: %s", err, out)
		}
		for _, line := range strings.Split(string(out), "\n") {
			if value, ok := strings.CutPrefix(strings.TrimSpace(line), "description: "); ok {
				found.Token = value
			}
		}
		return found, true, nil
	}
	return found, false, nil
}

// Only the disposable epair created and acknowledged by this harness can be
// captured. Destruction requires its boot/jail, native index, MAC and private
// description to still match; a missing or replaced receipt never adopts a link.
func interfaceCommand(args []string) error {
	if runtime.GOOS != "freebsd" {
		return errors.New("epair ownership check requires native FreeBSD")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if len(args) == 4 && args[0] == "capture" {
		name, token, path := args[1], args[2], args[3]
		if !strings.HasPrefix(name, "epair") || !strings.HasSuffix(name, "a") || !strings.HasPrefix(token, "routerd-lifecycle-") {
			return errors.New("invalid private epair identity")
		}
		before, exists, err := observeInterface(ctx, name)
		if err != nil || !exists || before.Index < 1 || before.MAC == "" || before.Token != "" {
			return errors.Join(err, errors.New("new epair identity is unavailable or already described"))
		}
		peerName := strings.TrimSuffix(name, "a") + "b"
		peer, peerExists, err := observeInterface(ctx, peerName)
		if err != nil || !peerExists || peer.Index < 1 || peer.MAC == "" || peer.Token != "" || peer.Scope != before.Scope {
			return errors.Join(err, errors.New("new epair peer identity is unavailable or already described"))
		}
		for _, endpoint := range []string{name, peerName} {
			if out, err := exec.CommandContext(ctx, "ifconfig", endpoint, "description", token).CombinedOutput(); err != nil {
				return fmt.Errorf("mark new epair: %w: %s", err, out)
			}
		}
		after, exists, err := observeInterface(ctx, name)
		before.Token = token
		if err != nil || !exists || after != before {
			return errors.Join(err, errors.New("epair identity changed before capture"))
		}
		peerAfter, peerExists, err := observeInterface(ctx, peerName)
		peer.Token = token
		if err != nil || !peerExists || peerAfter != peer {
			return errors.Join(err, errors.New("epair peer identity changed before capture"))
		}
		data, err := json.Marshal(epairReceipt{A: after, B: peerAfter})
		if err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(append(data, '\n'))
		if err == nil {
			err = f.Sync()
		}
		return errors.Join(err, f.Close())
	}
	if len(args) == 2 && args[0] == "destroy" {
		data, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		var receipt epairReceipt
		if err := json.Unmarshal(data, &receipt); err != nil {
			return err
		}
		if !strings.HasPrefix(receipt.A.Name, "epair") || !strings.HasSuffix(receipt.A.Name, "a") || receipt.B.Name != strings.TrimSuffix(receipt.A.Name, "a")+"b" {
			return errors.New("invalid epair receipt names")
		}
		endpoints := []interfaceReceipt{receipt.A, receipt.B}
		absent := 0
		for _, expected := range endpoints {
			if expected.Index < 1 || expected.Scope == "" || expected.MAC == "" || expected.Token == "" {
				return errors.New("epair receipt is incomplete")
			}
			actual, exists, err := observeInterface(ctx, expected.Name)
			if err != nil {
				return err
			}
			if !exists {
				absent++
				continue
			}
			if actual != expected {
				return errors.New("epair endpoint was replaced; preserve native links and evidence")
			}
		}
		if absent == 2 {
			return nil
		}
		if absent != 0 {
			return errors.New("epair endpoint is absent but its peer remains unknown")
		}
		if out, err := exec.CommandContext(ctx, "ifconfig", receipt.A.Name, "destroy").CombinedOutput(); err != nil {
			return fmt.Errorf("destroy owned epair: %w: %s", err, out)
		}
		for _, expected := range endpoints {
			_, exists, err := observeInterface(ctx, expected.Name)
			if err != nil {
				return err
			}
			if exists {
				return errors.New("epair destruction was not acknowledged")
			}
		}
		return nil
	}
	return errors.New("expected interface capture NAME TOKEN RECEIPT or destroy RECEIPT")
}
