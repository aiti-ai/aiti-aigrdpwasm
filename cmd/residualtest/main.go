package main

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/nakagami/grdp/plugin/rdpgfx"
)

func main() {
	data, _ := os.ReadFile(os.Args[1])
	residualLen := int(binary.LittleEndian.Uint32(data[2:]))
	seq := 6
	if len(os.Args) > 2 {
		fmt.Sscanf(os.Args[2], "%d", &seq)
	}
	payload := make([]byte, 14+residualLen)
	payload[0] = 0
	payload[1] = byte(seq)
	copy(payload[2:14], data[2:14])
	binary.LittleEndian.PutUint32(payload[2:6], uint32(residualLen))
	copy(payload[14:], data[14:14+residualLen])

	for _, wh := range [][2]int{{1024, 640}, {1280, 800}} {
		px := rdpgfx.DecodeClearFrame(payload, wh[0], wh[1])
		o := (180*wh[0] + 50) * 4
		fmt.Printf("DecodeClearFrame %dx%d row180: %v\n", wh[0], wh[1], px[o:o+9])
	}
}
