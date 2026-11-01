// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux && !freebsd

package ipsec

func providerGeneration(string) (string, error) {
	return "", nil
}
