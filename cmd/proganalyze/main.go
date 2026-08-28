// proganalyze: 解析 progressive dump 的块/flags 结构，确定需要实现的特性子集
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
)

const (
	wbtSync        = 0xCCC0
	wbtFrameBegin  = 0xCCC1
	wbtFrameEnd    = 0xCCC2
	wbtContext     = 0xCCC3
	wbtRegion      = 0xCCC4
	wbtTileSimple  = 0xCCC5
	wbtTileFirst   = 0xCCC6
	wbtTileUpgrade = 0xCCC7
)

func name(t uint16) string {
	switch t {
	case wbtSync:
		return "SYNC"
	case wbtFrameBegin:
		return "FRAME_BEGIN"
	case wbtFrameEnd:
		return "FRAME_END"
	case wbtContext:
		return "CONTEXT"
	case wbtRegion:
		return "REGION"
	case wbtTileSimple:
		return "TILE_SIMPLE"
	case wbtTileFirst:
		return "TILE_FIRST"
	case wbtTileUpgrade:
		return "TILE_UPGRADE"
	}
	return fmt.Sprintf("UNKNOWN_%04X", t)
}

func main() {
	stats := map[string]int{}
	flagsSeen := map[string]bool{}

	for _, f := range os.Args[1:] {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		fmt.Printf("=== %s (%d bytes) ===\n", filepath.Base(f), len(data))
		off := 0
		for off+6 <= len(data) {
			bt := binary.LittleEndian.Uint16(data[off:])
			bl := binary.LittleEndian.Uint32(data[off+2:])
			if bl < 6 || off+int(bl) > len(data) {
				fmt.Printf("  坏块 type=%04X len=%d at %d\n", bt, bl, off)
				break
			}
			body := data[off+6 : off+int(bl)]
			stats[name(bt)]++

			switch bt {
			case wbtRegion:
				// tileSize(1) numRects(2) numQuant(1) numProgQuant(1) flags(1) numTiles(2) tileDataSize(4)
				flags := body[5]
				numTiles := binary.LittleEndian.Uint16(body[6:])
				tileDataSize := binary.LittleEndian.Uint32(body[8:])
				numRects := binary.LittleEndian.Uint16(body[1:])
				numQuant := body[3]
				numProgQuant := body[4]
				extrap := flags & 0x01
				fmt.Printf("  REGION: rects=%d quant=%d progQuant=%d flags=%02X (extrapolate=%v) numTiles=%d tileDataSize=%d\n",
					numRects, numQuant, numProgQuant, flags, extrap != 0, numTiles, tileDataSize)
				if extrap != 0 {
					flagsSeen["EXTRAPOLATE"] = true
				}
				// 解析 tiles
				toff := 12 + int(numRects)*8 + int(numQuant)*5 + int(numProgQuant)*16
				tend := toff + int(tileDataSize)
				tcount := map[string]int{}
				qualities := map[byte]int{}
				tileFlags := map[byte]int{}
				for toff+6 <= tend && toff+6 <= len(data) {
					tt := binary.LittleEndian.Uint16(data[toff:])
					tl := binary.LittleEndian.Uint32(data[toff+2:])
					if tl < 6 || toff+int(tl) > len(data) {
						break
					}
					tb := data[toff+6 : toff+int(tl)]
					tcount[name(tt)]++
					if len(tb) >= 9 {
						q := tb[8]
						if tt == wbtTileSimple {
							q = 0xFF
						}
						qualities[q]++
						tileFlags[tb[7]&0x01]++
						stats[name(tt)]++
					}
					toff += int(tl)
				}
				fmt.Printf("    tiles: %v qualities: %v tileDiffFlag: %v\n", tcount, qualities, tileFlags)
			case wbtContext:
				cf := body[1] // context flags: numTileSets? 实际是 (1 byte id, 1 byte flags)?
				fmt.Printf("  CONTEXT: payload=% x\n", body[:min(4, len(body))])
				if cf&0x01 != 0 {
					flagsSeen["SUBBAND_DIFFING"] = true
				}
			case wbtFrameBegin:
				if len(body) >= 2 {
					fmt.Printf("  FRAME_BEGIN: frameIndex=%d\n", binary.LittleEndian.Uint32(body[0:4]))
				}
			}
			off += int(bl)
		}
	}
	fmt.Printf("\n=== 全量统计 ===\n")
	for k, v := range stats {
		fmt.Printf("%-14s %d\n", k, v)
	}
	fmt.Printf("flags: %v\n", flagsSeen)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
