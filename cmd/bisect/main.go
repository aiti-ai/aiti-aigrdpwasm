package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nakagami/grdp/plugin/rdpgfx"
)

const W, H = 1280, 800

func writeBMP(path string, px []byte) {
	rowSize := (W*3 + 3) &^ 3
	imgSize := rowSize * H
	bmp := make([]byte, 54+imgSize)
	bmp[0], bmp[1] = 'B', 'M'
	put32 := func(off, v int) { bmp[off] = byte(v); bmp[off+1] = byte(v >> 8); bmp[off+2] = byte(v >> 16); bmp[off+3] = byte(v >> 24) }
	put16 := func(off, v int) { bmp[off] = byte(v); bmp[off+1] = byte(v >> 8) }
	put32(2, 54+imgSize)
	put32(10, 54)
	put32(14, 40)
	put16(26, 1)
	put16(28, 24)
	put32(18, W)
	put32(22, H)
	put32(34, imgSize)
	for y := 0; y < H; y++ {
		src := (H - 1 - y) * W * 4
		dst := 54 + y*rowSize
		for x := 0; x < W; x++ {
			bmp[dst+x*3] = px[src+x*4]
			bmp[dst+x*3+1] = px[src+x*4+1]
			bmp[dst+x*3+2] = px[src+x*4+2]
		}
	}
	os.WriteFile(path, bmp, 0644)
}

func main() {
	dir, outDir := os.Args[1], os.Args[2]
	from, to := 8, 20
	fmt.Sscanf(os.Args[3], "%d-%d", &from, &to)
	os.MkdirAll(outDir, 0755)
	entries, _ := os.ReadDir(dir)
	names := []string{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "gfx-") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	sess := rdpgfx.NewClearSession()
	prog := rdpgfx.NewProgSession()
	surf := make([]byte, W*H*4)
	for i := 3; i < len(surf); i += 4 {
		surf[i] = 0xFF
	}
	for i, n := range names {
		data, _ := os.ReadFile(filepath.Join(dir, n))
		parts := strings.Split(strings.TrimSuffix(n, ".bin"), "-")
		kind := parts[2]
		var left, top, w, h int
		fmt.Sscanf(parts[3], "%d", &left)
		fmt.Sscanf(parts[4], "%d", &top)
		fmt.Sscanf(parts[5], "%dx%d", &w, &h)
		switch kind {
		case "clear":
			sess.DecodeInto(data, w, h, surf, W, H, left, top)
		default:
			prog.DecodeInto(data, surf, W, H)
		}
		f := i + 1
		if f >= from && f <= to {
			writeBMP(filepath.Join(outDir, fmt.Sprintf("f%03d.bmp", f)), surf)
		}
		if from < 0 && kind == "clear" {
			writeBMP(filepath.Join(outDir, fmt.Sprintf("clear-%03d.bmp", f)), surf)
		}
	}
	fmt.Println("done")
}
