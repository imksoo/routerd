// SPDX-License-Identifier: BSD-3-Clause

package chain

func (s *dynamicRouteSAMStore) ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error) {
	return s.ObjectStatus(apiVersion, kind, name), nil
}

func (s *syncMapStore) ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error) {
	return s.ObjectStatus(apiVersion, kind, name), nil
}
