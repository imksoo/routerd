// SPDX-License-Identifier: BSD-3-Clause
//go:build !linux && !freebsd

package chain

import (
	"context"
	"errors"
	"syscall"
)

func checkNativeDnsmasqDescriptorView() error {
	return errors.New("native dnsmasq requires Linux or FreeBSD")
}

func readNativeDnsmasqProcess(context.Context, int) (dnsmasqNativeProcess, error) {
	return dnsmasqNativeProcess{}, errors.New("native dnsmasq process observation requires Linux or FreeBSD")
}
func listNativeDnsmasqProcesses(context.Context, dnsmasqProcessSpec) ([]dnsmasqNativeProcess, error) {
	return nil, errors.New("native dnsmasq process observation requires Linux or FreeBSD")
}
func signalNativeDnsmasqProcess(context.Context, dnsmasqNativeProcess, syscall.Signal) error {
	return errors.New("native dnsmasq process signals require Linux or FreeBSD")
}

func dnsmasqProcessScope(context.Context) (string, error) {
	return "", errors.New("native dnsmasq scope requires Linux or FreeBSD")
}

func nativeDnsmasqProcessExited(context.Context, dnsmasqNativeProcess) (bool, error) {
	return false, errors.New("native dnsmasq process observation requires Linux or FreeBSD")
}
