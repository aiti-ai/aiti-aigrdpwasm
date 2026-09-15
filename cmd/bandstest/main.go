// bandstest: staged visual decode + FreeRDP-literal bands re-implementation diff.
// usage: bandstest <payload.bin> <width>x<height>
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nakagami/grdp/plugin/rdpgfx"
)

func writeBMP(path string, px []byte, w, h int) {
	rowSize := (w*3 + 3) &^ 3
	imgSize := rowSize * h
	bmp := make([]byte, 54+imgSize)
	bmp[0], bmp[1] = 'B', 'M'
	put32 := func(off, v int) { binary.LittleEndian.PutUint32(bmp[off:], uint32(v)) }
	put16 := func(off, v int) { binary.LittleEndian.PutUint16(bmp[off:], uint16(v)) }
	put32(2, 54+imgSize)
	put32(10, 54)
	put32(14, 40)
	put16(26, 1)
	put16(28, 24)
	put32(18, w)
	put32(22, h)
	put32(34, imgSize)
	for y := 0; y < h; y++ {
		src := (h - 1 - y) * w * 4
		dst := 54 + y*rowSize
		for x := 0; x < w; x++ {
			bmp[dst+x*3] = px[src+x*4]
			bmp[dst+x*3+1] = px[src+x*4+1]
			bmp[dst+x*3+2] = px[src+x*4+2]
		}
	}
	if err := os.WriteFile(path, bmp, 0644); err != nil {
		panic(err)
	}
}

func readBMP(path string) ([]byte, int, int) {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	w := int(binary.LittleEndian.Uint32(data[18:]))
	h := int(binary.LittleEndian.Uint32(data[22:]))
	rowSize := (w*3 + 3) &^ 3
	px := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		dst := (h - 1 - y) * w * 4
		src := 54 + y*rowSize
		for x := 0; x < w; x++ {
			px[dst+x*4] = data[src+x*3]
			px[dst+x*4+1] = data[src+x*3+1]
			px[dst+x*4+2] = data[src+x*3+2]
			px[dst+x*4+3] = 0xFF
		}
	}
	return px, w, h
}

// paintBandsFreeRDP re-implements FreeRDP clear_decompress_bands_data literally
// (2.11.7), painting onto px (w*h*4 BGRA). Returns error position or -1.
func paintBandsFreeRDP(data []byte, w, h int, px []byte) int {
	pos, end := 0, len(data)
	band := 0
	var vBarStorage [32768][]byte  // composed full entries (BGR, count=len/3)
	var vBarCount_ [32768]int
	var shortStorage [16384][]byte // short entries
	shortCursor, fullCursor := 0, 0
	for pos+11 <= end {
		xStart := int(binary.LittleEndian.Uint16(data[pos:]))
		xEnd := int(binary.LittleEndian.Uint16(data[pos+2:]))
		yStart := int(binary.LittleEndian.Uint16(data[pos+4:]))
		yEnd := int(binary.LittleEndian.Uint16(data[pos+6:]))
		bb, bg, br := data[pos+8], data[pos+9], data[pos+10]
		pos += 11
		vBarHeight := yEnd - yStart + 1
		if vBarHeight > 52 {
			return pos - 11
		}
		vBarCount := xEnd - xStart + 1
		for i := 0; i < vBarCount; i++ {
			if pos+2 > end {
				return pos
			}
			hdr := binary.LittleEndian.Uint16(data[pos:])
			pos += 2
			yOn := 0
			var shortEntry []byte
			shortCount := 0
			update := false
			switch {
			case hdr&0xC000 == 0x4000:
				idx := int(hdr & 0x3FFF)
				shortEntry = shortStorage[idx]
				shortCount = len(shortEntry) / 3
				yOn = int(data[pos])
				pos++
				update = true
			case hdr&0xC000 == 0x0000:
				yOn = int(hdr & 0xFF)
				yOff := int((hdr >> 8) & 0x3F)
				if yOff < yOn || yOff-yOn > 52 {
					return pos
				}
				shortCount = yOff - yOn
				buf := make([]byte, shortCount*3)
				copy(buf, data[pos:pos+shortCount*3])
				shortStorage[shortCursor] = buf
				shortCursor = (shortCursor + 1) % len(shortStorage)
				shortEntry = buf
				pos += shortCount * 3
				update = true
			case hdr&0x8000 == 0x8000:
				idx := int(hdr & 0x7FFF)
				if len(vBarStorage[idx]) == 0 {
					buf := make([]byte, vBarHeight*3)
					for y := 0; y < vBarHeight; y++ {
						buf[y*3], buf[y*3+1], buf[y*3+2] = bb, bg, br
					}
					vBarStorage[idx] = buf
					vBarCount_[idx] = vBarHeight
				}
			default:
				return pos
			}
			if update {
				full := make([]byte, vBarHeight*3)
				for y := 0; y < vBarHeight; y++ {
					var b2, g2, r2 byte
					switch {
					case y < yOn:
						b2, g2, r2 = bb, bg, br
					case y < yOn+shortCount:
						b2, g2, r2 = shortEntry[(y-yOn)*3], shortEntry[(y-yOn)*3+1], shortEntry[(y-yOn)*3+2]
					default:
						b2, g2, r2 = bb, bg, br
					}
					full[y*3], full[y*3+1], full[y*3+2] = b2, g2, r2
				}
				vBarStorage[fullCursor] = full
				vBarCount_[fullCursor] = vBarHeight
				fullCursor = (fullCursor + 1) % len(vBarStorage)
				// paint column
				x := xStart + i
				for y := 0; y < vBarHeight && yStart+y < h; y++ {
					if x >= w {
						break
					}
					idx2 := ((yStart+y)*w + x) * 4
					px[idx2] = full[y*3]
					px[idx2+1] = full[y*3+1]
					px[idx2+2] = full[y*3+2]
					px[idx2+3] = 0xFF
				}
			} else {
				// full hit: paint cached entry (count rows)
				idx := int(hdr & 0x7FFF)
				entry := vBarStorage[idx]
				cnt := vBarCount_[idx]
				x := xStart + i
				for y := 0; y < cnt && yStart+y < h; y++ {
					if x >= w {
						break
					}
					idx2 := ((yStart+y)*w + x) * 4
					px[idx2] = entry[y*3]
					px[idx2+1] = entry[y*3+1]
					px[idx2+2] = entry[y*3+2]
					px[idx2+3] = 0xFF
				}
			}
		}
		band++
	}
	_ = band
	fmt.Printf("freerdp-bands: shortCursor=%d fullCursor=%d\n", shortCursor, fullCursor)
	return -1
}

func main() {
	file := os.Args[1]
	var w, h int
	fmt.Sscanf(os.Args[2], "%dx%d", &w, &h)
	data, err := os.ReadFile(file)
	if err != nil {
		panic(err)
	}
	off := 2
	if data[0]&0x01 != 0 {
		off += 2
	}
	residualLen := int(binary.LittleEndian.Uint32(data[off:]))
	bandsLen := int(binary.LittleEndian.Uint32(data[off+4:]))
	_ = int(binary.LittleEndian.Uint32(data[off+8:]))
	off += 12
	base := strings.TrimSuffix(filepath.Base(file), ".bin")
	dir := filepath.Dir(file)

	// 1) fork output: residual+bands (fresh session)
	sess := rdpgfx.NewClearSession()
	forkPx := sess.Decode(append(append(append([]byte{}, data[:off-12]...), zeroCounts(residualLen, bandsLen, 0)...), data[off:off+residualLen+bandsLen]...), w, h)
	writeBMP(filepath.Join(dir, base+".fork.bands.bmp"), forkPx, w, h)

	// 2) FreeRDP-literal: start from fork's residual-only image, paint bands myself
	resPx, _, _ := readBMP(filepath.Join(dir, base+".res.bmp"))
	bandData := data[off+residualLen : off+residualLen+bandsLen]
	if badPos := paintBandsFreeRDP(bandData, w, h, resPx); badPos >= 0 {
		fmt.Printf("freerdp-literal parse STOPPED at %d\n", badPos)
	}
	writeBMP(filepath.Join(dir, base+".freerdp.bands.bmp"), resPx, w, h)

	// 3) diff
	diff := 0
	first := -1
	for i := 0; i+3 < len(forkPx) && i < len(resPx); i += 4 {
		if forkPx[i] != resPx[i] || forkPx[i+1] != resPx[i+1] || forkPx[i+2] != resPx[i+2] {
			if diff < 12 {
				pix := i / 4
				fmt.Printf("DIFF pix(%d,%d): fork=#%02x%02x%02x freerdp=#%02x%02x%02x\n",
					pix%w, pix/w, forkPx[i], forkPx[i+1], forkPx[i+2], resPx[i], resPx[i+1], resPx[i+2])
			}
			diff++
			if first < 0 {
				x, y := pix2xy(i/4, w)
				first = pix2band(bandData, x, y)
			}
		}
	}
	fmt.Printf("TOTAL DIFF PIXELS fork-vs-freerdp: %d (%.2f%%)\n", diff, 100*float64(diff)/float64(w*h))
	if first >= 0 {
		fmt.Printf("first diff falls in band #%d\n", first)
	}
}

func pix2xy(pix, w int) (int, int) { return pix % w, pix / w }

func pix2band(bandData []byte, x, y int) int {
	pos, band := 0, 0
	for pos+11 <= len(bandData) {
		xStart := int(binary.LittleEndian.Uint16(bandData[pos:]))
		xEnd := int(binary.LittleEndian.Uint16(bandData[pos+2:]))
		yStart := int(binary.LittleEndian.Uint16(bandData[pos+4:]))
		yEnd := int(binary.LittleEndian.Uint16(bandData[pos+6:]))
		pos += 11
		if x >= xStart && x <= xEnd && y >= yStart && y <= yEnd {
			return band
		}
		vBarCount := xEnd - xStart + 1
		for i := 0; i < vBarCount; i++ {
			hdr := binary.LittleEndian.Uint16(bandData[pos:])
			pos += 2
			switch {
			case hdr&0xC000 == 0x4000:
				pos++
			case hdr&0xC000 == 0x0000:
				n := int((hdr >> 8) & 0x3F) - int(hdr&0xFF)
				pos += 3 * n
			}
		}
		band++
	}
	return -1
}

func zeroCounts(r, b, s int) []byte {
	out := make([]byte, 12)
	binary.LittleEndian.PutUint32(out[0:], uint32(r))
	binary.LittleEndian.PutUint32(out[4:], uint32(b))
	binary.LittleEndian.PutUint32(out[8:], uint32(s))
	return out
}
