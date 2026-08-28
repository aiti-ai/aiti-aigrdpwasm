// progtest: 离线解码 progressive payload → PNG + 结构分数
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nakagami/grdp/plugin/rdpgfx"
)

func main() {
	dir := os.Args[1]
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".bin") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		out := rdpgfx.DecodeProgressiveFrame(data, 1280, 800)
		writePNG(filepath.Join(dir, strings.TrimSuffix(e.Name(), ".bin")+".png"), out, 1280, 800)
		// 统计
		colors := map[uint32]int{}
		for i := 0; i+3 < len(out); i += 16 {
			colors[uint32(out[i])<<16|uint32(out[i+1])<<8|uint32(out[i+2])]++
		}
		top := ""
		for c, n := range colors {
			if n > len(out)/16/3 { // 占比 > 1/3
				top += fmt.Sprintf(" #%06x=%d", c, n)
			}
		}
		fmt.Printf("%s: %d bytes → %d distinct colors%s\n", e.Name(), len(data), len(colors), top)
	}
}

func writePNG(path string, px []byte, w, h int) {
	f, _ := os.Create(path)
	defer f.Close()
	// 手写 PNG 太繁琐，改用 BMP
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
	f.Write(b)
}
