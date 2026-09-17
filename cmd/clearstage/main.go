// clearstage: warm-cache staged decode of ONE ClearCodec frame.
// Replays an ordered capture (RDPGFX_DUMP_DIR layout) up to a target frame
// (building real session cache state), then decodes the target's ClearCodec
// payload in three stages — residual only, +bands, +subcodec — each onto the
// warm surface, dumping all three so a diff against FreeRDP ground truth
// isolates the diverging SECTION.
//
// usage: clearstage <dir> <outDir> <targetFrameNumber>
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
	target := 0
	fmt.Sscanf(os.Args[3], "%d", &target)
	seqOverride := -1
	if len(os.Args) > 4 {
		fmt.Sscanf(os.Args[4], "%d", &seqOverride)
	}
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

	// locate the target file index
	targetIdx := -1
	for i := range names {
		if i+1 == target {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		fmt.Println("target out of range")
		return
	}
	tName := names[targetIdx]
	fmt.Println("target:", tName)

	// replay everything BEFORE the target (full fidelity, like live)
	for i := 0; i < targetIdx; i++ {
		data, err := os.ReadFile(filepath.Join(dir, names[i]))
		if err != nil {
			continue
		}
		parts := strings.Split(strings.TrimSuffix(names[i], ".bin"), "-")
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
	}
	writeBMP(filepath.Join(outDir, "warm-before.bmp"), surf)

	// staged decode of the target
	tData, _ := os.ReadFile(filepath.Join(dir, tName))
	parts := strings.Split(strings.TrimSuffix(tName, ".bin"), "-")
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
	subcodecLen := int(binary.LittleEndian.Uint32(tData[off+8:]))
	fmt.Printf("target sections: residual=%d bands=%d subcodec=%d\n", residualLen, bandsLen, subcodecLen)

	// variant builder: same header, chosen sections
	variant := func(r, b, s int) []byte {
		out := make([]byte, 0, len(tData))
		hdrEnd := 2
		if tData[0]&0x01 != 0 {
			hdrEnd = 4
		}
		hdr := append([]byte(nil), tData[:hdrEnd]...)
		if seqOverride >= 0 {
			hdr[1] = byte(seqOverride)
			if hdr[0]&0x01 == 0 {
				// keep flags, just seq
			}
		}
		out = append(out, hdr...)
		var cnt [12]byte
		binary.LittleEndian.PutUint32(cnt[0:], uint32(r))
		binary.LittleEndian.PutUint32(cnt[4:], uint32(b))
		binary.LittleEndian.PutUint32(cnt[8:], uint32(s))
		out = append(out, cnt[:]...)
		out = append(out, tData[off:off+r]...)
		out = append(out, tData[off+r:off+r+b]...)
		out = append(out, tData[off+r+b:off+r+b+s]...)
		return out
	}

	// each successful decode advances the session seq by 1, so variants
	// after the first must carry seq+1, +2 …
	vseq := seqOverride
	variantAt := func(i int, r, b, s int) []byte {
		v := variant(r, b, s)
		if vseq >= 0 && i > 0 {
			v[1] = byte(vseq + i)
		}
		return v
	}
	surf1 := append([]byte(nil), surf...)
	sess.DecodeInto(variantAt(0, residualLen, 0, 0), w, h, surf1, W, H, left, top)
	writeBMP(filepath.Join(outDir, "stage1-res.bmp"), surf1)

	surf2 := append([]byte(nil), surf...)
	sess.DecodeInto(variantAt(1, residualLen, bandsLen, 0), w, h, surf2, W, H, left, top)
	writeBMP(filepath.Join(outDir, "stage2-bands.bmp"), surf2)

	surf3 := append([]byte(nil), surf...)
	sess.DecodeInto(variantAt(2, residualLen, bandsLen, subcodecLen), w, h, surf3, W, H, left, top)
	writeBMP(filepath.Join(outDir, "stage3-full.bmp"), surf3)

	fmt.Println("stages written: warm-before / stage1-res / stage2-bands / stage3-full")
}
