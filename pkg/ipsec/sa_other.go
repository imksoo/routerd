// SPDX-License-Identifier: BSD-3-Clause
//go:build !linux

package ipsec

func defaultNativeSAs() nativeSAStore { return nil }
