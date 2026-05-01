//go:build freebsd

package dhcp6control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

type AFPacketSender struct{}

type bpfIfreq struct {
	Name [unix.IFNAMSIZ]byte
	_    [16]byte
}

func (AFPacketSender) SendFrame(ctx context.Context, ifname string, frame []byte) error {
	if ifname == "" {
		return fmt.Errorf("interface name is required")
	}
	if len(frame) < 14 {
		return fmt.Errorf("frame too short to contain an Ethernet header (%d bytes)", len(frame))
	}
	fd, err := openBPFWriteDevice()
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var ifr bpfIfreq
	copy(ifr.Name[:], ifname)
	if err := ioctlPtr(fd, uintptr(unix.BIOCSETIF), unsafe.Pointer(&ifr)); err != nil {
		return fmt.Errorf("BIOCSETIF %s: %w", ifname, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	n, err := unix.Write(fd, frame)
	if err != nil {
		return fmt.Errorf("BPF write %s: %w", ifname, err)
	}
	if n != len(frame) {
		return fmt.Errorf("BPF write short: wrote %d of %d bytes", n, len(frame))
	}
	return nil
}

func openBPFWriteDevice() (int, error) {
	for i := 0; i < 256; i++ {
		fd, err := unix.Open(fmt.Sprintf("/dev/bpf%d", i), unix.O_RDWR|unix.O_CLOEXEC, 0)
		if err == nil {
			return fd, nil
		}
		if !errors.Is(err, unix.EBUSY) && !errors.Is(err, unix.ENOENT) {
			return -1, err
		}
	}
	fd, err := unix.Open("/dev/bpf", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err == nil {
		return fd, nil
	}
	if os.IsNotExist(err) {
		return -1, fmt.Errorf("no available BPF device for write")
	}
	return -1, err
}

func ioctlPtr(fd int, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}
