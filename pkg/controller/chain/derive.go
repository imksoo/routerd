package chain

import (
	"fmt"
	"math/big"
	"net/netip"
	"strings"
)

func DeriveIPv6Address(prefixText, subnetID, suffixText string) (string, error) {
	prefix, err := netip.ParsePrefix(prefixText)
	if err != nil {
		return "", err
	}
	prefix = prefix.Masked()
	if !prefix.Addr().Is6() {
		return "", fmt.Errorf("prefix must be IPv6")
	}
	childBits := 64
	if prefix.Bits() > childBits {
		childBits = prefix.Bits()
	}
	base := intFromAddr(prefix.Addr())
	subnet := big.NewInt(0)
	if strings.TrimSpace(subnetID) != "" {
		if _, ok := subnet.SetString(strings.TrimPrefix(subnetID, "0x"), 16); !ok {
			return "", fmt.Errorf("invalid subnetID %q", subnetID)
		}
	}
	shift := uint(128 - childBits)
	subnet.Lsh(subnet, shift)
	base.Or(base, subnet)
	if suffixText != "" {
		suffix, err := netip.ParseAddr(suffixText)
		if err != nil {
			return "", err
		}
		suffixInt := intFromAddr(suffix)
		mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), shift), big.NewInt(1))
		suffixInt.And(suffixInt, mask)
		base.Or(base, suffixInt)
	}
	addr := addrFromInt(base)
	return netip.PrefixFrom(addr, childBits).String(), nil
}

func intFromAddr(addr netip.Addr) *big.Int {
	raw := addr.As16()
	return new(big.Int).SetBytes(raw[:])
}

func addrFromInt(value *big.Int) netip.Addr {
	var raw [16]byte
	bytes := value.Bytes()
	copy(raw[16-len(bytes):], bytes)
	return netip.AddrFrom16(raw)
}
