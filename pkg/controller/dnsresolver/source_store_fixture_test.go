// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

func (s mapStore) ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error) {
	return s.ObjectStatus(apiVersion, kind, name), nil
}
