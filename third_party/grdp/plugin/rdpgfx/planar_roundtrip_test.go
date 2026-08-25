package rdpgfx

import (
	"bytes"
	"math/rand"
	"testing"
)

// ---- Faithful port of xrdp's xrdp_bitmap32_compress.c (encoder side) ----
// so we can round-trip against the real wire format.

// xrdpDelta ports fdelta(): vertical (column-wise) delta encoding with the
// sign-carrying byte transform: first row verbatim, later rows encode
//   delta = in[y*w+x] - in[(y-1)*w+x]  (signed 8-bit)
// as  even => +d/2, odd => -((d>>1)+1).
func xrdpDelta(plane []byte, w, h int) []byte {
	out := make([]byte, len(plane))
	copy(out, plane)
	for i := 0; i < w*(h-1); i++ {
		delta := int8(plane[i+w]) - int8(plane[i])
		isNeg := (delta >> 7) & 1
		var v int8
		if isNeg == 0 {
			v = delta << 1
		} else {
			v = (-delta << 1) - 1
		}
		out[i+w] = byte(v)
	}
	return out
}

// xrdpFout ports fout(): emit one control chunk (literals + repeat).
func xrdpFout(s []byte, line []byte, colptr, collen, replen int) []byte {
	cont := collen > 13
	for cont {
		lcollen := collen
		if lcollen > 15 {
			lcollen = 15
		}
		s = append(s, byte(lcollen<<4))
		s = append(s, line[colptr:colptr+lcollen]...)
		colptr += lcollen
		collen -= lcollen
		cont = collen > 13
	}
	cont = collen > 0 || replen > 0
	for cont {
		lreplen := replen
		if collen == 0 && lreplen > 15 {
			if lreplen > 47 {
				lreplen = 47
			}
			replen -= lreplen
			code := ((lreplen & 0xF) << 4) | ((lreplen & 0xF0) >> 4)
			s = append(s, byte(code))
			colptr += lreplen
		} else {
			if lreplen > 15 {
				lreplen = 15
			}
			replen -= lreplen
			if lreplen < 3 {
				collen += lreplen
				lreplen = 0
			}
			code := (collen << 4) | lreplen
			s = append(s, byte(code))
			s = append(s, line[colptr:colptr+collen]...)
			colptr += collen + lreplen
			collen = 0
		}
		cont = replen > 0
	}
	return s
}

// xrdpFpack ports fpack(): RLE a single plane line by line.
func xrdpFpack(plane []byte, w, h int) []byte {
	var s []byte
	for y := 0; y < h; y++ {
		line := plane[y*w : (y+1)*w]
		colptr := 0
		collen, replen := 0, 0
		if line[0] == 0 {
			replen = 1
		} else {
			collen = 1
		}
		ptr := 0
		for ptr < w-1 {
			if line[ptr] == line[ptr+1] {
				replen++
			} else {
				if replen > 0 {
					if replen < 3 {
						collen += replen + 1
						replen = 0
					} else {
						s = xrdpFout(s, line, colptr, collen, replen)
						colptr = ptr + 1
						replen = 0
						collen = 1
					}
				} else {
					collen++
				}
			}
			ptr++
		}
		s = xrdpFout(s, line, colptr, collen, replen)
	}
	return s
}

// xrdpCompressPlanar builds a full planar frame exactly like
// xrdp_mm_egfx_send_planar_bitmap (header 0x10 = RLE+alpha, or 0x30 with
// noAlpha), planes in stream order A,R,G,B (or R,G,B when noAlpha).
func xrdpCompressPlanar(r, g, b, a []byte, w, h int, noAlpha bool) []byte {
	header := byte(0x10)
	if noAlpha {
		header |= 0x20
	}
	var s []byte
	s = append(s, header)
	dr, dg, db, da := xrdpDelta(r, w, h), xrdpDelta(g, w, h), xrdpDelta(b, w, h), xrdpDelta(a, w, h)
	if noAlpha {
		s = append(s, xrdpFpack(dr, w, h)...)
		s = append(s, xrdpFpack(dg, w, h)...)
		s = append(s, xrdpFpack(db, w, h)...)
	} else {
		s = append(s, xrdpFpack(da, w, h)...)
		s = append(s, xrdpFpack(dr, w, h)...)
		s = append(s, xrdpFpack(dg, w, h)...)
		s = append(s, xrdpFpack(db, w, h)...)
	}
	return s
}

func roundTrip(t *testing.T, r, g, b, a []byte, w, h int, noAlpha bool) {
	t.Helper()
	wire := xrdpCompressPlanar(r, g, b, a, w, h, noAlpha)
	// ARGB wire format: honor the alpha plane.
	got := decodePlanar(wire, w, h, pixelFormatARGB8888)
	if len(got) != w*h*4 {
		t.Fatalf("bad output length %d", len(got))
	}
	for i := 0; i < w*h; i++ {
		// decodePlanar emits BGRA
		wantB, wantG, wantR, wantA := b[i], g[i], r[i], byte(0xFF)
		if !noAlpha {
			wantA = a[i]
		}
		gotB, gotG, gotR, gotA := got[i*4], got[i*4+1], got[i*4+2], got[i*4+3]
		if gotB != wantB || gotG != wantG || gotR != wantR || gotA != wantA {
			t.Fatalf("pixel %d mismatch: got BGRA=%d,%d,%d,%d want %d,%d,%d,%d (wire len %d)",
				i, gotB, gotG, gotR, gotA, wantB, wantG, wantR, wantA, len(wire))
		}
	}
}

func makePlanes(w, h int, seed int64) (r, g, b, a []byte) {
	rnd := rand.New(rand.NewSource(seed))
	n := w * h
	r, g, b, a = make([]byte, n), make([]byte, n), make([]byte, n), make([]byte, n)
	for i := 0; i < n; i++ {
		r[i] = byte(rnd.Intn(256))
		g[i] = byte(rnd.Intn(256))
		b[i] = byte(rnd.Intn(256))
		a[i] = byte(rnd.Intn(256))
	}
	return
}

func TestPlanarRoundTripRandom(t *testing.T) {
	sizes := [][2]int{{64, 64}, {32, 16}, {128, 32}, {16, 16}, {7, 5}, {1, 1}}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		r, g, b, a := makePlanes(w, h, int64(w*1000+h))
		roundTrip(t, r, g, b, a, w, h, false)
		roundTrip(t, r, g, b, a, w, h, true)
	}
}

func TestPlanarRoundTripFlat(t *testing.T) {
	// Solid color planes (maximum run compression), plus a vertical gradient
	// (heavy delta use) and horizontal gradient.
	w, h := 64, 64
	for _, v := range []byte{0, 1, 127, 128, 255} {
		r := bytes.Repeat([]byte{v}, w*h)
		g := bytes.Repeat([]byte{0xFF - v}, w*h)
		b := bytes.Repeat([]byte{0x80}, w*h)
		a := bytes.Repeat([]byte{0xE0}, w*h)
		roundTrip(t, r, g, b, a, w, h, false)
		roundTrip(t, r, g, b, a, w, h, true)
	}
	// vertical gradient in R
	r := make([]byte, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r[y*w+x] = byte(y * 4)
		}
	}
	g := bytes.Repeat([]byte{0}, w*h)
	b := bytes.Repeat([]byte{0}, w*h)
	a := bytes.Repeat([]byte{0xFF}, w*h)
	roundTrip(t, r, g, b, a, w, h, false)
	roundTrip(t, r, g, b, a, w, h, true)
}

func TestPlanarRoundTripLongRuns(t *testing.T) {
	// Rows that are single long runs > 47 to exercise the big-run (nibble
	// swap) path.
	w, h := 64, 16
	r := make([]byte, w*h)
	g := make([]byte, w*h)
	b := make([]byte, w*h)
	a := make([]byte, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r[y*w+x] = byte(y)
			g[y*w+x] = byte(64 - x)
			b[y*w+x] = 0x33
			a[y*w+x] = 0xFF
		}
	}
	roundTrip(t, r, g, b, a, w, h, false)
	roundTrip(t, r, g, b, a, w, h, true)
}

func TestPlanarXRGBForcesOpaque(t *testing.T) {
	// xrdp sends XRGB_8888 with an all-zero alpha plane; decodePlanar must
	// ignore the (transparent) alpha plane and produce opaque pixels.
	w, h := 64, 64
	r := bytes.Repeat([]byte{0x10}, w*h)
	g := bytes.Repeat([]byte{0x20}, w*h)
	b := bytes.Repeat([]byte{0x30}, w*h)
	a := bytes.Repeat([]byte{0x00}, w*h) // all-zero alpha, like xrdp
	wire := xrdpCompressPlanar(r, g, b, a, w, h, false)
	got := decodePlanar(wire, w, h, pixelFormatXRGB8888)
	for i := 0; i < w*h; i++ {
		if got[i*4] != 0x30 || got[i*4+1] != 0x20 || got[i*4+2] != 0x10 || got[i*4+3] != 0xFF {
			t.Fatalf("pixel %d: got BGRA=%d,%d,%d,%d want 48,32,16,255",
				i, got[i*4], got[i*4+1], got[i*4+2], got[i*4+3])
		}
	}
}

func TestPlanarARGBHonorsAlpha(t *testing.T) {
	// With ARGB wire format, a varying alpha plane must be honored.
	w, h := 32, 32
	r := bytes.Repeat([]byte{0xAA}, w*h)
	g := bytes.Repeat([]byte{0xBB}, w*h)
	b := bytes.Repeat([]byte{0xCC}, w*h)
	a := make([]byte, w*h)
	for i := range a {
		a[i] = byte(i % 256)
	}
	wire := xrdpCompressPlanar(r, g, b, a, w, h, false)
	got := decodePlanar(wire, w, h, pixelFormatARGB8888)
	for i := 0; i < w*h; i++ {
		if got[i*4+3] != a[i] {
			t.Fatalf("pixel %d alpha: got %d want %d", i, got[i*4+3], a[i])
		}
	}
}
