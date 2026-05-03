package state

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DUIDTypeLinkLayer     = 3
	DUIDTypeLinkLayerTime = 1
)

type KAMEDUIDInfo struct {
	HasLengthPrefix bool
	Payload         []byte
	Type            uint16
}

func KAMEDHCPv6CDUIDLLFromMAC(mac string) ([]byte, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(mac)), ":")
	if len(parts) != 6 {
		return nil, fmt.Errorf("invalid MAC address %q", mac)
	}
	payload := []byte{0x00, 0x03, 0x00, 0x01}
	for _, part := range parts {
		if len(part) != 2 {
			return nil, fmt.Errorf("invalid MAC address %q", mac)
		}
		value, err := hex.DecodeString(part)
		if err != nil {
			return nil, fmt.Errorf("invalid MAC address %q", mac)
		}
		payload = append(payload, value[0])
	}
	data := make([]byte, 2, 2+len(payload))
	binary.LittleEndian.PutUint16(data, uint16(len(payload)))
	data = append(data, payload...)
	return data, nil
}

func KAMEDHCPv6CDUIDLLFromRawData(raw string) ([]byte, error) {
	cleaned := strings.ReplaceAll(strings.TrimSpace(raw), ":", "")
	payload, err := hex.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("invalid DUID raw data %q", raw)
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("invalid DUID raw data %q", raw)
	}
	duid := []byte{0x00, 0x03}
	duid = append(duid, payload...)
	data := make([]byte, 2, 2+len(duid))
	binary.LittleEndian.PutUint16(data, uint16(len(duid)))
	data = append(data, duid...)
	return data, nil
}

func ParseKAMEDHCPv6CDUID(data []byte) KAMEDUIDInfo {
	payload := data
	hasLength := false
	if len(data) >= 2 {
		lengthLE := int(binary.LittleEndian.Uint16(data[:2]))
		lengthBE := int(binary.BigEndian.Uint16(data[:2]))
		switch {
		case lengthLE == len(data)-2:
			payload = data[2:]
			hasLength = true
		case lengthBE == len(data)-2:
			payload = data[2:]
			hasLength = true
		}
	}
	var typ uint16
	if len(payload) >= 2 {
		typ = binary.BigEndian.Uint16(payload[:2])
	}
	return KAMEDUIDInfo{HasLengthPrefix: hasLength, Payload: payload, Type: typ}
}

func EnsureKAMEDHCPv6CDUIDLL(path, mac string, now time.Time) (changed bool, backupPath string, err error) {
	want, err := KAMEDHCPv6CDUIDLLFromMAC(mac)
	if err != nil {
		return false, "", err
	}
	return EnsureKAMEDHCPv6CDUID(path, want, now)
}

func EnsureKAMEDHCPv6CDUIDLLRaw(path, raw string, now time.Time) (changed bool, backupPath string, err error) {
	want, err := KAMEDHCPv6CDUIDLLFromRawData(raw)
	if err != nil {
		return false, "", err
	}
	return EnsureKAMEDHCPv6CDUID(path, want, now)
}

func EnsureKAMEDHCPv6CDUID(path string, want []byte, now time.Time) (changed bool, backupPath string, err error) {
	current, readErr := os.ReadFile(path)
	if readErr == nil {
		if string(current) == string(want) {
			return false, "", nil
		}
		backupPath = path + ".bak." + now.UTC().Format("20060102T150405Z")
		if err := os.Rename(path, backupPath); err != nil {
			return false, "", err
		}
	} else if !os.IsNotExist(readErr) {
		return false, "", readErr
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, backupPath, err
	}
	if err := os.WriteFile(path, want, 0600); err != nil {
		return false, backupPath, err
	}
	return true, backupPath, nil
}
