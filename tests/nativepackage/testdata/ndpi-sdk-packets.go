// SPDX-License-Identifier: BSD-3-Clause

//go:build ignore

package main

import (
	"encoding/binary"
	"github.com/imksoo/routerd/pkg/dpi"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) != 2 {
		panic("usage: go run ndpi-sdk-packets.go OUTPUT_DIR")
	}
	if err := os.MkdirAll(os.Args[1], 0700); err != nil {
		panic(err)
	}
	for _, x := range []struct {
		name string
		port uint16
		data []byte
	}{{"tls", 443, dpi.MinimalTLSClientHello("routerd.example")}, {"http", 80, []byte("GET / HTTP/1.1\r\nHost: routerd.example\r\n\r\n")}} {
		p := append([]byte{0x45, 0, 0, 0, 0, 0, 0, 0, 64, 6, 0, 0, 192, 0, 2, 10, 198, 51, 100, 10, 0xcf, 0xb0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x50, 0x18, 0, 0, 0, 0, 0, 0}, x.data...)
		binary.BigEndian.PutUint16(p[2:4], uint16(len(p)))
		binary.BigEndian.PutUint16(p[22:24], x.port)
		if err := os.WriteFile(filepath.Join(os.Args[1], x.name+".packet"), p, 0644); err != nil {
			panic(err)
		}
	}
}
