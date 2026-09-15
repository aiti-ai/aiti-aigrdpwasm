// gfxreplay: replay an ordered WTS1+WTS2 capture (RDPGFX_DUMP_DIR layout) onto
// a persistent 1280x800 surface, dumping periodic snapshots as BMPs.
// usage: gfxreplay <dir> <outDir> [snapshotEvery]
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nakagami/grdp/plugin/rdpgfx"
)

const W, H = 1280, 800

func writeBMP(path string, px []byte) {
	rowSize := (W*3 + 3) &^ 3
	imgSize := rowSize * H
	bmp := make([]byte, 54+imgSize)
	bmp[0], bmp[1] = 'B', 'M'
	put32 := func(off, v int) { binary.LittleEndian.PutUint32(bmp[off:], uint32(v)) }
	put16 := func(off, v int) { binary.LittleEndian.PutUint16(bmp[off:], uint16(v)) }
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
	every := 40
	if len(os.Args) > 3 {
		every, _ = strconv.Atoi(os.Args[3])
	}
	os.MkdirAll(outDir, 0755)
	entries, _ := os.ReadDir(dir)
	names := []string{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "gfx-") && strings.HasSuffix(e.Name(), ".bin") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	surf := make([]byte, W*H*4)
	for i := 3; i < len(surf); i += 4 {
		surf[i] = 0xFF
	}
	clearSess := rdpgfx.NewClearSession()
	progSess := rdpgfx.NewProgSession()

	// watch regions: mean color of each 64x64 tile after every file
	type watch struct{ x, y int }
	watches := []watch{{6, 11}, {8, 11}, {0, 12}, {17, 11}, {18, 12}, {8, 12}, {5, 10}, {2, 10}}
	prev := map[watch][3]int{}
	for _, w := range watches {
		prev[w] = [3]int{-1, -1, -1}
	}
	sample := func(x, y int) [3]int {
		// mean of 64x64 at tile coords
		var r, g, b int64
		n := int64(0)
		for yy := 0; yy < 64 && y*64+yy < H; yy++ {
			for xx := 0; xx < 64 && x*64+xx < W; xx++ {
				off := ((y*64+yy)*W + x*64 + xx) * 4
				n++
				r += int64(surf[off])
				g += int64(surf[off+1])
				b += int64(surf[off+2])
			}
		}
		if n == 0 {
			return [3]int{0, 0, 0}
		}
		return [3]int{int(r / n), int(g / n), int(b / n)}
	}

	for i, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		// gfx-%06d-<kind>-<left>-<top>-<w>x<h>.bin
		parts := strings.Split(strings.TrimSuffix(n, ".bin"), "-")
		kind := parts[2]
		left, _ := strconv.Atoi(parts[3])
		top, _ := strconv.Atoi(parts[4])
		wh := strings.Split(parts[5], "x")
		w, _ := strconv.Atoi(wh[0])
		h, _ := strconv.Atoi(wh[1])
		switch kind {
		case "clear":
			clearSess.DecodeInto(data, surf, W, H, left, top)
		case "prog":
			progSess.DecodeInto(data, surf, W, H)
		case "wts2":
			// WTS2: per-codec dispatch — progressive is codecId 9; probe the
			// first tile block type heuristically? The live WTS2 stream for
			// Windows desktop is progressive (codec 9); uncompressed/planar
			// (full-frame) are rare. Treat wts2 as progressive.
			progSess.DecodeInto(data, surf, W, H)
		}
		for _, wt := range watches {
			cur := sample(wt.x, wt.y)
			p := prev[wt]
			if cur != p {
				fmt.Printf("tile(%d,%d) %s -> #%02x%02x%02x  [%s]\n", wt.x, wt.y, fmt.Sprintf("#%02x%02x%02x", p[0], p[1], p[2]), cur[0], cur[1], cur[2], n)
				prev[wt] = cur
			}
		}
		if every > 0 && (i+1)%every == 0 {
			writeBMP(filepath.Join(outDir, fmt.Sprintf("snap-%06d.bmp", i+1)), surf)
		}
		_ = w
		_ = h
	}
	writeBMP(filepath.Join(outDir, "final.bmp"), surf)
	fmt.Printf("replayed %d frames -> %s\n", len(names), outDir)
}
