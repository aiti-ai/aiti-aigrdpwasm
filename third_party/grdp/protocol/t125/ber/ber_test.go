package ber

import (
	"bytes"
	"testing"
)

// TestWriteIntegerSignEncoding locks the DER INTEGER sign-bit encoding:
// 0xFFFF must encode as 02 03 00 FF FF (positive), not 02 02 FF FF (which
// parses as -1). It was once emitted as a negative number, breaking MCS domain
// parameter parsing on Windows -> immediate disconnect.
func TestWriteIntegerSignEncoding(t *testing.T) {
	cases := []struct {
		n    int
		want []byte
	}{
		{0x7F, []byte{0x02, 0x01, 0x7F}},
		{0x80, []byte{0x02, 0x02, 0x00, 0x80}},
		{0xFF, []byte{0x02, 0x02, 0x00, 0xFF}},
		{0x100, []byte{0x02, 0x02, 0x01, 0x00}},
		{0x7FFF, []byte{0x02, 0x02, 0x7F, 0xFF}},
		{0xFFFF, []byte{0x02, 0x03, 0x00, 0xFF, 0xFF}},
		{0xFC17, []byte{0x02, 0x03, 0x00, 0xFC, 0x17}},
	}
	for _, c := range cases {
		buf := &bytes.Buffer{}
		WriteInteger(c.n, buf)
		if !bytes.Equal(buf.Bytes(), c.want) {
			t.Errorf("WriteInteger(0x%X) = %s, want %s", c.n,
				bytesToHex(buf.Bytes()), bytesToHex(c.want))
		}
	}
}

func bytesToHex(b []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[2*i] = hex[v>>4]
		out[2*i+1] = hex[v&0xF]
	}
	return string(out)
}
