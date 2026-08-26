package nla

import (
	"encoding/asn1"
	"testing"
)

// TestTSCredentialsDERShapeInternal locks the DER shape of TSCredentials
// (internal test, touches unexported fields): the credentials [1] field must be
// an EXPLICIT OCTET STRING wrapping the DER of the TSPasswordCreds SEQUENCE.
// A past change mistakenly used a bare SEQUENCE, making Windows fail to parse
// authInfo (TLS alert internal_error).
func TestTSCredentialsDERShapeInternal(t *testing.T) {
	got := EncodeDERTCredentials([]byte{0, 0}, []byte("t\x00"), []byte("p\x00"))
	// expected: 30 <len> a0 03 02 01 01  a1 <len> 04 <len> 30 <tpas SEQUENCE...>
	var tcre TSCredentials
	if _, err := asn1.Unmarshal(got, &tcre); err != nil {
		t.Fatalf("TSCredentials parse failed (wrong DER shape; Windows refuses too): %v\nraw=%x", err, got)
	}
	if tcre.CredType != 1 {
		t.Errorf("CredType = %d, want 1", tcre.CredType)
	}
	if len(tcre.Credentials) < 2 || tcre.Credentials[0] != 0x30 {
		t.Errorf("credentials content is not a TSPasswordCreds SEQUENCE(0x30): %x", tcre.Credentials)
	}
	// [1] EXPLICIT followed by OCTET STRING(0x04), then SEQUENCE(0x30)
	a1 := indexByte(got, 0xa1)
	if a1 < 0 {
		t.Fatalf("[1] tag not found: %x", got)
	}
	if got[a1+2] != 0x04 {
		t.Errorf("credentials is not [1] EXPLICIT OCTET STRING(0x04): %x", got[a1:])
	}
	if got[a1+4] != 0x30 {
		t.Errorf("OCTET STRING content is not a SEQUENCE(0x30): %x", got[a1:])
	}
}

func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}
