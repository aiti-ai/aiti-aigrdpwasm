package main

import (
	"encoding/binary"
	"fmt"
	"os"
)

func main() {
	data, _ := os.ReadFile(os.Args[1])
	off := 0
	for off+6 <= len(data) {
		bt := binary.LittleEndian.Uint16(data[off:])
		bl := int(binary.LittleEndian.Uint32(data[off+2:]))
		if bl < 6 || off+bl > len(data) {
			fmt.Printf("BAD BLOCK at %d: type=0x%04x len=%d (remain %d)\n", off, bt, bl, len(data)-off)
			break
		}
		bd := data[off+6 : off+bl]
		fmt.Printf("block @%d type=0x%04x len=%d\n", off, bt, bl)
		if bt == 0xCCC4 { // REGION
			if len(bd) >= 12 {
				numRects := int(binary.LittleEndian.Uint16(bd[1:]))
				numQuant := int(bd[3])
				numProgQuant := int(bd[4])
				regionFlags := bd[5]
				numTiles := int(binary.LittleEndian.Uint16(bd[6:]))
				tileDataSize := int(binary.LittleEndian.Uint32(bd[8:]))
				fmt.Printf("  region: numRects=%d numQuant=%d numProgQuant=%d flags=0x%02x numTiles=%d tileDataSize=%d bodyLen=%d\n",
					numRects, numQuant, numProgQuant, regionFlags, numTiles, tileDataSize, len(bd))
				toff := 12 + numRects*8 + numQuant*5 + numProgQuant*16
				parsed := 0
				for toff+6 <= len(bd) && parsed < numTiles+3 {
					tt := binary.LittleEndian.Uint16(bd[toff:])
					tl := int(binary.LittleEndian.Uint32(bd[toff+2:]))
					extra := ""
					if tl >= 10 && toff+10 <= len(bd) {
						extra = fmt.Sprintf(" xIdx=%d yIdx=%d", binary.LittleEndian.Uint16(bd[toff+6:]), binary.LittleEndian.Uint16(bd[toff+8:]))
					}
					fmt.Printf("  tile[%d] @%d type=0x%04x len=%d%s\n", parsed, toff, tt, tl, extra)
					if tl < 6 || toff+tl > len(bd) {
						fmt.Printf("  LOOP BREAK (tl<6 or overflow)\n")
						break
					}
					toff += tl
					parsed++
					if tt != 0xCCC5 && tt != 0xCCC6 && tt != 0xCCC7 {
						fmt.Printf("  (unknown type continues)\n")
					}
				}
				fmt.Printf("  parsed=%d/%d tiles, toff=%d tileEnd=%d\n", parsed, numTiles, toff, 12+tileDataSize)
			}
		}
		off += bl
	}
}
