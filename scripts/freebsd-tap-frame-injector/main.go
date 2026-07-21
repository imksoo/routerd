// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package main

import (
	"encoding/binary"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: freebsd-tap-frame-injector INTERFACE")
		os.Exit(2)
	}
	device := "/dev/" + os.Args[1]
	fd, err := unix.Open(device, unix.O_RDWR, 0)
	if err != nil {
		panic(fmt.Errorf("open %s: %w", device, err))
	}
	defer unix.Close(fd)

	frames := [][]byte{arpRequest(), routerAdvertisement()}
	for i := 0; i < 4; i++ {
		for _, frame := range frames {
			if _, err := unix.Write(fd, frame); err != nil {
				panic(err)
			}
		}
	}
}

func arpRequest() []byte {
	frame := make([]byte, 42)
	copy(frame[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	copy(frame[6:12], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x02})
	binary.BigEndian.PutUint16(frame[12:14], 0x0806)
	arp := frame[14:]
	binary.BigEndian.PutUint16(arp[0:2], 1)
	binary.BigEndian.PutUint16(arp[2:4], 0x0800)
	arp[4], arp[5] = 6, 4
	binary.BigEndian.PutUint16(arp[6:8], 1)
	copy(arp[8:14], frame[6:12])
	copy(arp[14:18], []byte{192, 0, 2, 2})
	copy(arp[24:28], []byte{192, 0, 2, 1})
	return frame
}

func routerAdvertisement() []byte {
	return []byte{
		0x33, 0x33, 0x00, 0x00, 0x00, 0x01,
		0x02, 0x00, 0x00, 0x00, 0x00, 0x02,
		0x86, 0xdd,
		0x60, 0x00, 0x00, 0x00, 0x00, 0x10, 0x3a, 0xff,
		0xfe, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0xff, 0xfe, 0x00, 0x00, 0x02,
		0xff, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		134, 0, 0, 0, 64, 0x00, 0x00, 0xb4,
		0, 0, 0, 0, 0, 0, 0, 0,
	}
}
