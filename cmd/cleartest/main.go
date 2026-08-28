// ClearCodec offline decoding verification: read the payload captured by repro, call decodeIntoBuf to decode, output BMP + structure score.
// Usage: cleartest /tmp/cleardump
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nakagami/grdp/plugin/rdpgfx"
)

// Minimal reimplementation of clearCodecCtx decoding (aligned with rdpgfx.go).
// Here directly reference the original implementation: can't import across packages (internal), so use exec to run a self-test program inside the grdp package? Simplify: copy the decode logic via go:generate won't work.
// A better approach: put this file in the rdpgfx package directory as an internal test. Switch to an external shell: use a small main inside the rdpgfx package.

func main() {
	dir := os.Args[1]
	entries, _ := os.ReadDir(dir)
	names := []string{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bin") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	shared := rdpgfx.NewClearSession() // 共享缓存态，模拟真实会话顺序解码
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		// Parse dimensions from the filename clear-wts2-NN_WxH.bin
		var w, h int
		base := strings.TrimSuffix(n, ".bin")
		idx := strings.LastIndex(base, "_")
		if idx > 0 {
			fmt.Sscanf(base[idx+1:], "%dx%d", &w, &h)
		}
		if w <= 0 || h <= 0 {
			continue
		}
		out := shared.Decode(data, w, h)
		if out == nil {
			fmt.Printf("%s: DECODE FAIL\n", n)
			continue
		}
		writeBMP(filepath.Join(dir, strings.TrimSuffix(n, ".bin")+".bmp"), out, w, h)
		fmt.Printf("%s: %s\n", n, structureScore(out, w, h))
	}
}

// structureScore: proportion of adjacent pixels (right neighbor/row below) that are identical. Normal UI blocks >0.7, noise <0.1.
func structureScore(px []byte, w, h int) string {
	total, same := 0, 0
	for y := 0; y < h; y++ {
		for x := 0; x < w-1; x++ {
			o := (y*w + x) * 4
			if px[o] == px[o+4] && px[o+1] == px[o+5] && px[o+2] == px[o+6] {
				same++
			}
			total++
		}
	}
	for y := 0; y < h-1; y++ {
		for x := 0; x < w; x++ {
			o := (y*w + x) * 4
			o2 := ((y+1)*w + x) * 4
			if px[o] == px[o2] && px[o+1] == px[o2+1] && px[o+2] == px[o2+2] {
				same++
			}
			total++
		}
	}
	if total == 0 {
		return "empty"
	}
	return fmt.Sprintf("structure=%.2f", float64(same)/float64(total))
}

func writeBMP(path string, px []byte, w, h int) {
	rowSize := (w*3 + 3) &^ 3
	imgSize := rowSize * h
	fileSize := 54 + imgSize
	b := make([]byte, fileSize)
	b[0], b[1] = 'B', 'M'
	binary.LittleEndian.PutUint32(b[2:], uint32(fileSize))
	binary.LittleEndian.PutUint32(b[10:], 54)
	binary.LittleEndian.PutUint32(b[14:], 40)
	binary.LittleEndian.PutUint32(b[18:], uint32(w))
	binary.LittleEndian.PutUint32(b[22:], uint32(h))
	binary.LittleEndian.PutUint16(b[26:], 1)
	binary.LittleEndian.PutUint16(b[28:], 24)
	binary.LittleEndian.PutUint32(b[34:], uint32(imgSize))
	for y := 0; y < h; y++ {
		srcY := y // BMP bottom-up: flip when writing
		dst := 54 + (h-1-y)*rowSize
		for x := 0; x < w; x++ {
			o := (srcY*w + x) * 4
			// BGRA in memory (B first); BMP needs BGR
			b[dst] = px[o]
			b[dst+1] = px[o+1]
			b[dst+2] = px[o+2]
			dst += 3
		}
	}
	os.WriteFile(path, b, 0644)
}
