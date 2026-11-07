// SPDX-License-Identifier: BSD-3-Clause

package pdclient

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// These IA_NA bytes use RFC 9915 sections 21.4/21.6, independently of our
// encoder: IAID 7, T1 30, T2 60, 2001:db8::123 with lifetimes 90/120.
const addressAssociationWire = "000000070000001e0000003c" +
	"0005001820010db80000000000000000000001230000005a00000078"

func addressWireBytes(t *testing.T, raw string) []byte {
	t.Helper()
	b, err := hex.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func addressReplyWire(t *testing.T, associations ...[]byte) []byte {
	t.Helper()
	// The PD option must survive IA_NA validation independently.
	msg := Message{Type: MessageReply, TransactionID: 100, ClientDUID: testServer,
		IAPDs: []IAPD{{IAID: 7, Prefixes: []IAPrefix{prefix("2001:db8:10::/56", 90, 120)}}}}
	b := mustPayload(t, msg)
	for _, ia := range associations {
		b = appendOption(b, 3, ia)
	}
	return b
}

func reencodedAddressOptions(t *testing.T, payload []byte) [][]byte {
	t.Helper()
	msg, err := DecodeMessageForIAID(payload, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.IAPDs) != 1 || msg.IAPDs[0].IAID != 7 {
		t.Fatalf("IA_NA processing changed independent PD association: %+v", msg.IAPDs)
	}
	encoded := mustPayload(t, msg)
	var result [][]byte
	if err := walkOptions(encoded[4:], func(code uint16, data []byte) error {
		if code == 3 {
			result = append(result, data)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAddressAssociationWirePreservesAddressAndIndependentPD(t *testing.T) {
	ia := addressWireBytes(t, addressAssociationWire)
	got := reencodedAddressOptions(t, addressReplyWire(t, ia))
	if len(got) != 1 || !bytes.Equal(got[0], ia) {
		t.Fatalf("IA_NA address/lifetimes lost: got %x, want %x", got, ia)
	}
}

func TestAddressAssociationFiltersIAIDAndDuplicateWithoutDroppingPD(t *testing.T) {
	ia := addressWireBytes(t, addressAssociationWire)
	other := bytes.Clone(ia)
	other[3] = 8
	got := reencodedAddressOptions(t, addressReplyWire(t, other, ia, ia))
	if len(got) != 1 || !bytes.Equal(got[0], ia) {
		t.Fatalf("configured IA_NA not selected once: %x", got)
	}
}

func TestAddressAssociationDiscardInvalidTimersOnly(t *testing.T) {
	bad := addressWireBytes(t, addressAssociationWire)
	bad[7] = 61 // T1 > T2; discard the association, retain independent PD.
	if got := reencodedAddressOptions(t, addressReplyWire(t, bad)); len(got) != 0 {
		t.Fatalf("invalid IA_NA retained: %x", got)
	}
}

func TestAddressAssociationKeepsValidSiblingAndZeroLifetimeWithdrawal(t *testing.T) {
	ia := addressWireBytes(t, addressAssociationWire)
	bad := bytes.Clone(ia[12:])
	bad[19] = 0x24 // Different address, preferred lifetime greater than valid.
	bad[len(bad)-1] = 89
	withdraw := bytes.Clone(ia[12:])
	withdraw[19] = 0x25
	clear(withdraw[20:]) // Explicit zero lifetime must reach the lease owner.
	input := append(bytes.Clone(ia[:12]), bad...)
	input = append(input, ia[12:]...)
	input = append(input, withdraw...)
	want := append(bytes.Clone(ia), withdraw...)
	got := reencodedAddressOptions(t, addressReplyWire(t, input))
	if len(got) != 1 || !bytes.Equal(got[0], want) {
		t.Fatalf("usable address or withdrawal lost: got %x, want %x", got, want)
	}
}

func TestAddressAssociationRetainsNoAddrsAvailWithoutAddresses(t *testing.T) {
	ia := addressWireBytes(t, "000000070000000000000000000d00020002")
	got := reencodedAddressOptions(t, addressReplyWire(t, ia))
	if len(got) != 1 || !bytes.Equal(got[0], ia) {
		t.Fatalf("IA_NA failure status lost: got %x, want %x", got, ia)
	}
}
