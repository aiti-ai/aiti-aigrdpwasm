package nla

import (
	"bytes"
	"encoding/hex"
	"github.com/nakagami/grdp/core"
	"testing"
)

// TestAppendClientAvPairs locks the client av-pair appending in the CredSSP
// NTLMv2 blob: server targetInfo (EOL stripped) + MsvAvFlags(0x2) +
// MsvAvChannelBindings(16 zeroes) + MsvAvTargetName("TERMSRV/h") + EOL.
// Without these av-pairs Windows AUTHENTICATE fails (SEC_E_INTERNAL_ERROR).
func TestAppendClientAvPairs(t *testing.T) {
	// simulated server targetInfo: one MsvAvNbComputerName + EOL
	serverInfo := bytes.Buffer{}
	// MsvAvNbComputerName(0x0001) len 4 value "AB"
	serverInfo.Write([]byte{0x01, 0x00, 0x04, 0x00, 'A', 'B'})
	serverInfo.Write([]byte{0x00, 0x00, 0x00, 0x00}) // EOL

	got := appendClientAvPairs(serverInfo.Bytes(), "h")

	// must contain MsvAvFlags(0x0006)=0x02000000
	flags := []byte{0x06, 0x00, 0x04, 0x00, 0x02, 0x00, 0x00, 0x00}
	if !bytes.Contains(got, flags) {
		t.Errorf("missing MsvAvFlags=0x2: %s", hex.EncodeToString(got))
	}
	// MsvAvChannelBindings(0x000A) 16 zero bytes
	cbt := append([]byte{0x0A, 0x00, 0x10, 0x00}, make([]byte, 16)...)
	if !bytes.Contains(got, cbt) {
		t.Errorf("missing MsvAvChannelBindings(16 zeroes): %s", hex.EncodeToString(got))
	}
	// MsvAvTargetName(0x0009) "TERMSRV/h" UTF-16LE
	tn := append([]byte{0x09, 0x00, 0x12, 0x00}, core.UnicodeEncode("TERMSRV/h")...)
	if !bytes.Contains(got, tn) {
		t.Errorf("missing MsvAvTargetName=TERMSRV/h: %s", hex.EncodeToString(got))
	}
	// must end with EOL
	if !bytes.HasSuffix(got, []byte{0x00, 0x00, 0x00, 0x00}) {
		t.Errorf("does not end with EOL: %s", hex.EncodeToString(got))
	}
}
