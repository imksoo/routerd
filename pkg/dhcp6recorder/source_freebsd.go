//go:build freebsd

package dhcp6recorder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

type AFPacketSource struct {
	fd      int
	ifname  string
	buf     []byte
	pending [][]byte
}

type bpfIfreq struct {
	Name [unix.IFNAMSIZ]byte
	_    [16]byte
}

func NewAFPacketSource(ifname string) (*AFPacketSource, error) {
	if ifname == "" {
		return nil, fmt.Errorf("interface name is required")
	}
	fd, err := openBPFDevice()
	if err != nil {
		return nil, err
	}
	if err := configureBPF(fd, ifname); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	size, err := unix.IoctlGetInt(fd, unix.BIOCGBLEN)
	if err != nil || size <= 0 {
		size = 4096
	}
	return &AFPacketSource{fd: fd, ifname: ifname, buf: make([]byte, size)}, nil
}

func openBPFDevice() (int, error) {
	for i := 0; i < 256; i++ {
		fd, err := unix.Open(fmt.Sprintf("/dev/bpf%d", i), unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err == nil {
			return fd, nil
		}
		if !errors.Is(err, unix.EBUSY) && !errors.Is(err, unix.ENOENT) {
			return -1, err
		}
	}
	fd, err := unix.Open("/dev/bpf", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err == nil {
		return fd, nil
	}
	if os.IsNotExist(err) {
		return -1, fmt.Errorf("no available BPF device")
	}
	return -1, err
}

func configureBPF(fd int, ifname string) error {
	if err := unix.IoctlSetInt(fd, unix.BIOCIMMEDIATE, 1); err != nil {
		return err
	}
	var ifr bpfIfreq
	copy(ifr.Name[:], ifname)
	if err := ioctlPtr(fd, uintptr(unix.BIOCSETIF), unsafe.Pointer(&ifr)); err != nil {
		return err
	}
	program := DHCP6AndRABPFProgram()
	insns := make([]unix.BpfInsn, len(program))
	for i, insn := range program {
		insns[i] = unix.BpfInsn{Code: insn.Code, Jt: insn.Jt, Jf: insn.Jf, K: insn.K}
	}
	bpfProgram := unix.BpfProgram{Len: uint32(len(insns)), Insns: &insns[0]}
	return ioctlPtr(fd, uintptr(unix.BIOCSETF), unsafe.Pointer(&bpfProgram))
}

func (s *AFPacketSource) ReadFrame(ctx context.Context) ([]byte, error) {
	for {
		if len(s.pending) > 0 {
			frame := s.pending[0]
			s.pending = s.pending[1:]
			return frame, nil
		}
		n, err := unix.Read(s.fd, s.buf)
		if err == nil {
			s.pending = appendBPFFrames(s.pending, s.buf[:n])
			continue
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EINTR) {
			continue
		}
		return nil, err
	}
}

func appendBPFFrames(out [][]byte, data []byte) [][]byte {
	headerSize := int(unsafe.Sizeof(unix.BpfHdr{}))
	for offset := 0; offset+headerSize <= len(data); {
		header := (*unix.BpfHdr)(unsafe.Pointer(&data[offset]))
		start := offset + int(header.Hdrlen)
		end := start + int(header.Caplen)
		if header.Hdrlen == 0 || header.Caplen == 0 || start < offset || end > len(data) {
			break
		}
		out = append(out, append([]byte(nil), data[start:end]...))
		offset += bpfWordAlign(int(header.Hdrlen) + int(header.Caplen))
	}
	return out
}

func bpfWordAlign(n int) int {
	align := int(unix.BPF_ALIGNMENT)
	return (n + align - 1) &^ (align - 1)
}

func (s *AFPacketSource) Close() error {
	if s == nil || s.fd < 0 {
		return nil
	}
	err := unix.Close(s.fd)
	s.fd = -1
	return err
}

func ioctlPtr(fd int, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}
