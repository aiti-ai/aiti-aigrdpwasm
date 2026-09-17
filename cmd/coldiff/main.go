// coldiff: per-column divergence analysis for one ClearCodec frame.
// Replays frames 1..N-1 warm, decodes frame N's bands section, diffs each
// column against the FreeRDP ground-truth warm frame, and reports the
// band/vBar case (fullHit/shortMiss/shortHit) for every diverging column —
// pinpointing WHICH decode path returns wrong pixels.
//
// usage: coldiff <dir> <freeRdpWarmPng> <targetFrameNumber>
package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nakagami/grdp/plugin/rdpgfx"
)

const W, H = 1280, 800

var ref *image.RGBA
var beforeImg *image.RGBA

func prBefore(x, y int) color.RGBA { return beforeImg.RGBAAt(x, y) }

func main() {
	dir := os.Args[1]
	refPath := os.Args[2]
	target := 0
	fmt.Sscanf(os.Args[3], "%d", &target)

	refFile, _ := os.Open(refPath)
	refImg, _ := png.Decode(refFile)
	refFile.Close()
	ref = refImg.(*image.RGBA)
	beforePath := os.Args[4]
	bf, _ := os.Open(beforePath)
	bimg, _ := png.Decode(bf)
	bf.Close()
	beforeImg = bimg.(*image.RGBA)

	entries, _ := os.ReadDir(dir)
	names := []string{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "gfx-") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	targetIdx := target - 1

	sess := rdpgfx.NewClearSession()
	prog := rdpgfx.NewProgSession()
	surf := make([]byte, W*H*4)
	for i := 3; i < len(surf); i += 4 {
		surf[i] = 0xFF
	}
	for i := 0; i < targetIdx; i++ {
		data, err := os.ReadFile(filepath.Join(dir, names[i]))
		if err != nil {
			continue
		}
		parts := strings.Split(strings.TrimSuffix(names[i], ".bin"), "-")
		var left, top, w, h int
		fmt.Sscanf(parts[3], "%d", &left)
		fmt.Sscanf(parts[4], "%d", &top)
		fmt.Sscanf(parts[5], "%dx%d", &w, &h)
		if parts[2] == "clear" {
			sess.DecodeInto(data, w, h, surf, W, H, left, top)
		} else {
			prog.DecodeInto(data, surf, W, H)
		}
	}

	tData, _ := os.ReadFile(filepath.Join(dir, names[targetIdx]))
	parts := strings.Split(strings.TrimSuffix(names[targetIdx], ".bin"), "-")
	var left, top, w, h int
	fmt.Sscanf(parts[3], "%d", &left)
	fmt.Sscanf(parts[4], "%d", &top)
	fmt.Sscanf(parts[5], "%dx%d", &w, &h)
	off := 2
	if tData[0]&0x01 != 0 {
		off += 2
	}
	residualLen := int(binary.LittleEndian.Uint32(tData[off:]))
	bandsLen := int(binary.LittleEndian.Uint32(tData[off+4:]))
	bandData := tData[off+12+residualLen : off+12+residualLen+bandsLen]

	// per-column divergence from ground truth (compare only rows this tile covers)
	// fork-after-frame10 (full payload)
	surfAfter := append([]byte(nil), surf...)
	sess.DecodeInto(tData, w, h, surfAfter, W, H, left, top)

	forkPainted := map[int]bool{}
	freePainted := map[int]bool{}
	badCol := map[int]bool{}
	for y := top; y < top+640 && y < H; y++ {
		for x := left; x < left+1024 && x < W; x++ {
			o := (y*W + x) * 4
			fBefore := [3]int{int(surf[o]), int(surf[o+1]), int(surf[o+2])}
			fAfter := [3]int{int(surfAfter[o]), int(surfAfter[o+1]), int(surfAfter[o+2])}
			pRef := ref.RGBAAt(x, y)
			pBefore := [3]int{int(beforeImg.RGBAAt(x, y).R), int(beforeImg.RGBAAt(x, y).G), int(beforeImg.RGBAAt(x, y).B)}
			if fBefore != fAfter {
				forkPainted[x] = true
			}
			if pBefore != fAfter {
				freePainted[x] = true
			}
			if fAfter != [3]int{int(pRef.R), int(pRef.G), int(pRef.B)} && abs(fAfter[0]-int(pRef.R))+abs(fAfter[1]-int(pRef.G))+abs(fAfter[2]-int(pRef.B)) > 30 {
				badCol[x] = true
			}
		}
	}
	onlyFork := 0
	onlyFree := 0
	both := 0
	neither := 0
	for x := left; x < left+1024; x++ {
		switch {
		case forkPainted[x] && freePainted[x]:
			both++
		case forkPainted[x]:
			onlyFork++
		case freePainted[x]:
			onlyFree++
		default:
			neither++
		}
	}
	fmt.Printf("columns: both=%d onlyFork=%d onlyFree=%d neither=%d\n", both, onlyFork, onlyFree, neither)
	fmt.Printf("fork painted cols=%d free painted cols=%d\n", len(forkPainted), len(freePainted))
	// list gaps: free painted but fork not (max 30)
	gaps := 0
	for x := left; x < left+1024 && gaps < 30; x++ {
		if freePainted[x] && !forkPainted[x] {
			fmt.Printf("  free-only col %d\n", x)
			gaps++
		}
	}
	os.Exit(0)
	// walk bands, classify each vBar column
	pos := 0
	band := 0
	for pos+11 <= len(bandData) {
		xStart := int(binary.LittleEndian.Uint16(bandData[pos:]))
		xEnd := int(binary.LittleEndian.Uint16(bandData[pos+2:]))
		yStart := int(binary.LittleEndian.Uint16(bandData[pos+4:]))
		yEnd := int(binary.LittleEndian.Uint16(bandData[pos+6:]))
		pos += 11
		vBars := xEnd - xStart + 1
		bad := 0
		cases := map[string]int{}
		badCases := map[string]int{}
		for i := 0; i < vBars; i++ {
			hdr := binary.LittleEndian.Uint16(bandData[pos:])
			pos += 2
			kind := "fullHit"
			switch {
			case hdr&0xC000 == 0x4000:
				kind = "shortHit"
				pos++
			case hdr&0xC000 == 0x0000:
				kind = "shortMiss"
				n := int((hdr>>8)&0x3F) - int(hdr&0xFF)
				if n < 0 {
					fmt.Printf("band %d col %d: NEGATIVE advance hdr=0x%04x, abort walk\n", band, xStart+i, hdr)
					os.Exit(2)
				}
				pos += 3 * n
			case hdr&0x8000 == 0x8000:
			default:
				kind = "invalid"
			}
			cases[kind]++
			if badCol[xStart+i] {
				bad++
				badCases[kind]++
			}
		}
		badTotal := 0
		for x := xStart; x <= xEnd; x++ {
			if badCol[x] {
				badTotal++
			}
		}
		fmt.Printf("band %d x=%d..%d y=%d..%d vBars=%d badCols=%d cases=%v badCases=%v\n",
			band, xStart, xEnd, yStart, yEnd, vBars, badTotal, cases, badCases)
		band++
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
