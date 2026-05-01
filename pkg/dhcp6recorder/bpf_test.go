package dhcp6recorder

import "testing"

func TestDHCP6AndRABPFProgramShape(t *testing.T) {
	program := DHCP6AndRABPFProgram()
	if len(program) != 16 {
		t.Fatalf("len = %d, want 16", len(program))
	}
	if program[0].K != 12 || program[1].K != uint32(etherTypeIPv6) {
		t.Fatalf("ethernet prefix = %+v %+v", program[0], program[1])
	}
	if program[3].K != uint32(ipProtocolUDP) || program[10].K != uint32(ipProtocolICMPv6) {
		t.Fatalf("next-header checks = %+v %+v", program[3], program[10])
	}
	if program[5].K != uint32(dhcp6ClientPort) || program[6].K != uint32(dhcp6ServerPort) ||
		program[8].K != uint32(dhcp6ClientPort) || program[9].K != uint32(dhcp6ServerPort) {
		t.Fatalf("dhcpv6 port checks = %+v %+v %+v %+v", program[5], program[6], program[8], program[9])
	}
	if program[12].K != uint32(icmp6TypeRA) {
		t.Fatalf("ra check = %+v", program[12])
	}
	if program[14].K == 0 || program[13].K != 0 || program[15].K != 0 {
		t.Fatalf("return path = %+v %+v %+v", program[13], program[14], program[15])
	}
}
