// SPDX-License-Identifier: BSD-3-Clause

package dpi

import "encoding/binary"

func MinimalTLSClientHello(host string) []byte {
	hostBytes := []byte(host)
	serverName := make([]byte, 3+len(hostBytes))
	serverName[0] = 0
	binary.BigEndian.PutUint16(serverName[1:3], uint16(len(hostBytes)))
	copy(serverName[3:], hostBytes)

	sniData := make([]byte, 2+len(serverName))
	binary.BigEndian.PutUint16(sniData[0:2], uint16(len(serverName)))
	copy(sniData[2:], serverName)

	extension := make([]byte, 4+len(sniData))
	binary.BigEndian.PutUint16(extension[0:2], 0)
	binary.BigEndian.PutUint16(extension[2:4], uint16(len(sniData)))
	copy(extension[4:], sniData)

	body := []byte{0x03, 0x03}
	body = append(body, make([]byte, 32)...)
	body = append(body, 0)
	body = append(body, 0, 2, 0x13, 0x01)
	body = append(body, 1, 0)
	body = append(body, byte(len(extension)>>8), byte(len(extension)))
	body = append(body, extension...)

	handshake := []byte{0x01, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
	handshake = append(handshake, body...)

	record := []byte{0x16, 0x03, 0x01, byte(len(handshake) >> 8), byte(len(handshake))}
	record = append(record, handshake...)
	return record
}
