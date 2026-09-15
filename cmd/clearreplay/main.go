// clearreplay: decode a captured ClearCodec frame sequence with SHARED cache
// state (mimicking a live session) and dump selected frames as BMPs.
// usage: clearreplay <dir> <outDir> [bandMinBytes]
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
	os.WriteFile(path, bmp, 0644)
}

func main() {
	dir, outDir := os.Args[1], os.Args[2]
	bandMin := 0
	if len(os.Args) > 3 {
		fmt.Sscanf(os.Args[3], "%d", &bandMin)
	}
	os.MkdirAll(outDir, 0755)
	entries, _ := os.ReadDir(dir)
	names := []string{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bin") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	sess := rdpgfx.NewClearSession()
	dumped := 0
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		base := strings.TrimSuffix(n, ".bin")
		und := strings.LastIndex(base, "_")
		var w, h int
		fmt.Sscanf(base[und+1:], "%dx%d", &w, &h)
		if w <= 0 || h <= 0 {
			continue
		}
		off := 2
		if data[0]&0x01 != 0 {
			off += 2
		}
		bandsLen := int(binary.LittleEndian.Uint32(data[off+4:]))
		px := sess.Decode(data, w, h)
		if bandsLen >= bandMin && dumped < 40 {
			writeBMP(filepath.Join(outDir, fmt.Sprintf("%02d-%s.bmp", dumped, base)), px, w, h)
			dumped++
			fmt.Printf("frame %s bands=%d -> dumped\n", base, bandsLen)
		}
	}
	fmt.Printf("total frames=%d dumped=%d\n", len(names), dumped)
}
