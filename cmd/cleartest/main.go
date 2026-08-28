// cleartest: 离线解码 ClearCodec payload → PNG + 颜色统计
// usage: cleartest <dir>
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nakagami/grdp/plugin/rdpgfx"
)

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
	for _, n := range names {
		// 文件名 clear-wts1-N_WxH.bin
		var w, h int
		base := strings.TrimSuffix(n, ".bin")
		und := strings.LastIndex(base, "_")
		dot := strings.LastIndex(base[:und], "_")
		fmt.Sscanf(base[und+1:], "%dx%d", &w, &h)
		_ = dot
		if w <= 0 || h <= 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		out := rdpgfx.DecodeClearFrame(data, w, h)
		writeBMP(filepath.Join(dir, strings.TrimSuffix(n, ".bin")+".bmp"), out, w, h)
		colors := map[uint32]int{}
		for i := 0; i+3 < len(out); i += 8 {
			colors[uint32(out[i])<<16|uint32(out[i+1])<<8|uint32(out[i+2])]++
		}
		fmt.Printf("%s (%dx%d): %d colors, top: ", n, w, h, len(colors))
		c2 := 0
		for c, cnt := range colors {
			fmt.Printf("#%06x=%d ", c, cnt)
			if c2++; c2 >= 4 {
				break
			}
		}
		fmt.Println()
	}
}

func writeBMP(path string, px []byte, w, h int) {
	rowSize := (w*3 + 3) &^ 3
	imgSize := rowSize * h
	fileSize := 54 + imgSize
	b := make([]byte, fileSize)
	b[0], b[1] = 'B', 'M'
	b[2] = byte(fileSize)
	b[3] = byte(fileSize >> 8)
	b[10] = 54
	b[14] = 40
	b[18] = byte(w)
	b[19] = byte(w >> 8)
	b[22] = byte(h)
	b[23] = byte(h >> 8)
	b[26] = 1
	b[28] = 24
	b[34] = byte(imgSize)
	b[35] = byte(imgSize >> 8)
	for y := 0; y < h; y++ {
		dst := 54 + (h-1-y)*rowSize
		for x := 0; x < w; x++ {
			o := (y*w + x) * 4
			b[dst] = px[o]
			b[dst+1] = px[o+1]
			b[dst+2] = px[o+2]
			dst += 3
		}
	}
	os.WriteFile(path, b, 0644)
}
