// SPDX-License-Identifier: BSD-3-Clause

package pdclient

import (
	"encoding/binary"
	"fmt"
	"net/netip"
)

func encodeIANA(ia IANA) ([]byte, error) {
	var header [12]byte
	binary.BigEndian.PutUint32(header[0:4], ia.IAID)
	binary.BigEndian.PutUint32(header[4:8], ia.T1)
	binary.BigEndian.PutUint32(header[8:12], ia.T2)
	data := appendStatus(header[:], ia.StatusCode)
	for _, entry := range ia.Addresses {
		if !entry.Address.Is6() || entry.Address.Is4In6() || entry.Address.Zone() != "" {
			return nil, fmt.Errorf("invalid IA_NA IPv6 address")
		}
		var address [24]byte
		raw := entry.Address.As16()
		copy(address[:16], raw[:])
		binary.BigEndian.PutUint32(address[16:20], entry.Preferred)
		binary.BigEndian.PutUint32(address[20:24], entry.Valid)
		data = appendOption(data, optionIAAddress, appendStatus(address[:], entry.StatusCode))
	}
	if len(data) > 65535 {
		return nil, fmt.Errorf("IA_NA exceeds wire length")
	}
	return data, nil
}

func decodeIANA(data []byte) (IANA, error) {
	if len(data) < 12 {
		return IANA{}, fmt.Errorf("IA_NA option too short")
	}
	ia := IANA{IAID: binary.BigEndian.Uint32(data[:4]), T1: binary.BigEndian.Uint32(data[4:8]), T2: binary.BigEndian.Uint32(data[8:12])}
	// RFC 9915 section 21.4: invalid timers discard only this association.
	if ia.T1 > 0 && ia.T2 > 0 && ia.T1 > ia.T2 {
		return IANA{}, fmt.Errorf("IA_NA T1 exceeds T2")
	}
	err := walkOptions(data[12:], func(code uint16, data []byte) error {
		if code == optionStatusCode && len(data) >= 2 {
			ia.StatusCode = binary.BigEndian.Uint16(data[:2])
			return nil
		}
		if code != optionIAAddress || len(data) < 24 {
			return nil
		}
		entry := IAAddress{Preferred: binary.BigEndian.Uint32(data[16:20]), Valid: binary.BigEndian.Uint32(data[20:24])}
		if entry.Preferred > entry.Valid {
			return nil
		}
		var raw [16]byte
		copy(raw[:], data[:16])
		entry.Address = netip.AddrFrom16(raw)
		if err := walkOptions(data[24:], func(code uint16, value []byte) error {
			if code == optionStatusCode && len(value) >= 2 {
				entry.StatusCode = binary.BigEndian.Uint16(value[:2])
			}
			return nil
		}); err != nil {
			return nil
		}
		for _, existing := range ia.Addresses {
			if existing.Address == entry.Address {
				return nil
			}
		}
		ia.Addresses = append(ia.Addresses, entry)
		return nil
	})
	return ia, err
}
