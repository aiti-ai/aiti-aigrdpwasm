package nla_test

import (
	"testing"

	"github.com/nakagami/grdp/protocol/nla"
)

// TestEncodeDERTRequest locks the shape of the CredSSP TSRequest(negotiate):
// version=6 (CVE-2018-0886 hardening), a single NTLM negotiate negoToken, and
// a mandatory clientNonce[5]. The old assertion hardcoded version-2 bytes
// without a nonce; after hardening this is a structural check instead.
func TestEncodeDERTRequest(t *testing.T) {
	ntlm := nla.NewNTLMv2("", "", "")
	nonce := make([]byte, 32) // fixed 32 zero bytes for deterministic output
	result := nla.EncodeDERTRequest([]nla.Message{ntlm.GetNegotiateMessage()}, []byte(""), []byte(""), nonce)
	tsr, err := nla.DecodeDERTRequest(result)
	if err != nil {
		t.Fatalf("TSRequest parse failed: %v\nraw=%x", err, result)
	}
	if tsr.Version != 6 {
		t.Errorf("Version = %d, want 6 (hardened)", tsr.Version)
	}
	if len(tsr.NegoTokens) != 1 {
		t.Errorf("NegoTokens count = %d, want 1", len(tsr.NegoTokens))
	}
	if len(tsr.ClientNonce) != 32 {
		t.Errorf("ClientNonce length = %d, want 32", len(tsr.ClientNonce))
	}
}
