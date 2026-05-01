package dhcp6recorder

const (
	bpfLD  = 0x00
	bpfH   = 0x08
	bpfB   = 0x10
	bpfABS = 0x20
	bpfJMP = 0x05
	bpfJEQ = 0x10
	bpfK   = 0x00
	bpfRET = 0x06

	ipProtocolICMPv6 uint8 = 58
	icmp6TypeRA      uint8 = 134
)

type ClassicBPFInsn struct {
	Code uint16
	Jt   uint8
	Jf   uint8
	K    uint32
}

func DHCP6AndRABPFProgram() []ClassicBPFInsn {
	return []ClassicBPFInsn{
		{Code: bpfLD | bpfH | bpfABS, K: 12},
		{Code: bpfJMP | bpfJEQ | bpfK, Jf: 13, K: uint32(etherTypeIPv6)},
		{Code: bpfLD | bpfB | bpfABS, K: 20},
		{Code: bpfJMP | bpfJEQ | bpfK, Jf: 6, K: uint32(ipProtocolUDP)},
		{Code: bpfLD | bpfH | bpfABS, K: 54},
		{Code: bpfJMP | bpfJEQ | bpfK, Jt: 8, K: uint32(dhcp6ClientPort)},
		{Code: bpfJMP | bpfJEQ | bpfK, Jt: 7, K: uint32(dhcp6ServerPort)},
		{Code: bpfLD | bpfH | bpfABS, K: 56},
		{Code: bpfJMP | bpfJEQ | bpfK, Jt: 5, K: uint32(dhcp6ClientPort)},
		{Code: bpfJMP | bpfJEQ | bpfK, Jt: 4, Jf: 5, K: uint32(dhcp6ServerPort)},
		{Code: bpfJMP | bpfJEQ | bpfK, Jf: 4, K: uint32(ipProtocolICMPv6)},
		{Code: bpfLD | bpfB | bpfABS, K: 54},
		{Code: bpfJMP | bpfJEQ | bpfK, Jt: 1, K: uint32(icmp6TypeRA)},
		{Code: bpfRET | bpfK, K: 0},
		{Code: bpfRET | bpfK, K: 0xffff},
		{Code: bpfRET | bpfK, K: 0},
	}
}
