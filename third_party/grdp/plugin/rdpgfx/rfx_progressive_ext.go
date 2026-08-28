package rdpgfx

// Progressive codec extensions: subband extrapolation ("DWT_REDUCE_EXTRAPOLATE"),
// progressive quantization math and context state — ported from FreeRDP
// libfreerdp/codec/progressive.c (MS-RDPRFX / MS-RDPEGFX 2.2.4).
//
// Extrapolated subband layout (offsets within the 4096-coefficient buffer):
//
//	Band   Offset  Dims     Count
//	HL1    0       31x33    1023
//	LH1    1023    33x31    1023
//	HH1    2046    31x31    961
//	HL2    3007    16x17    272
//	LH2    3279    17x16    272
//	HH2    3551    16x16    256
//	HL3    3807    8x9      72
//	LH3    3879    9x8      72
//	HH3    3951    8x8      64
//	LL3    4015    9x9      81
//
// The uncoded edge row/column of each band is extrapolated across levels —
// the decoder runs three strided lifting passes over the SAME buffer and the
// not-written cells simply retain their coded (parent-level) values.

import (
	"log/slog"
	"sync"
)

const (
	progFlagSubbandDiffing  = 0x01 // CONTEXT block flags
	progFlagTileDifference  = 0x01 // tile flags
	progFlagDWTReduceExtrap = 0x01 // REGION block flags
	progQualityFull         = 0xFF // tile quality sentinel → quantProgValFull
)

// progTempBuf: L/H band scratch for the strided extrapolated DWT
// (worst case level 1: L=33*64 + H=31*64 = 4096 int16).
type progTempBuf [4096]int16

var progTempPool = sync.Pool{New: func() any { return new(progTempBuf) }}

// rfxProgQuant is one RFX_PROGRESSIVE_CODEC_QUANT (16 bytes): quality byte +
// three component quant value sets (same nibble layout as the simple quants).
type rfxProgQuant struct {
	quality  byte
	y, cb, cr rfxQuant
}

// decodeProgQuant reads one 16-byte RFX_PROGRESSIVE_CODEC_QUANT.
func decodeProgQuant(data []byte) rfxProgQuant {
	return rfxProgQuant{
		quality: data[0],
		y:       parseRfxQuant(data[1:]),
		cb:      parseRfxQuant(data[6:]),
		cr:      parseRfxQuant(data[11:]),
	}
}

// progShift computes the effective per-band dequantization shifts for one
// component: shift = quant + progQuant - 1 (FreeRDP quant_add + quant_lsub(1)),
// clamped at zero (FreeRDP would wrap uint8; negative shifts are meaningless).
func progShift(quant rfxQuant, prog rfxQuant) (s rfxQuant) {
	s.LL3 = progShiftByte(quant.LL3, prog.LL3)
	s.LH3 = progShiftByte(quant.LH3, prog.LH3)
	s.HL3 = progShiftByte(quant.HL3, prog.HL3)
	s.HH3 = progShiftByte(quant.HH3, prog.HH3)
	s.LH2 = progShiftByte(quant.LH2, prog.LH2)
	s.HL2 = progShiftByte(quant.HL2, prog.HL2)
	s.HH2 = progShiftByte(quant.HH2, prog.HH2)
	s.LH1 = progShiftByte(quant.LH1, prog.LH1)
	s.HL1 = progShiftByte(quant.HL1, prog.HL1)
	s.HH1 = progShiftByte(quant.HH1, prog.HH1)
	return
}

func progShiftByte(a, b uint8) uint8 {
	s := int(a) + int(b) - 1
	if s < 0 {
		return 0
	}
	return uint8(s)
}

func progShiftBand(data []int16, shift uint8) {
	if shift <= 0 {
		return
	}
	for i := range data {
		data[i] <<= shift & 31
	}
}

// progDecodeComponent decodes one component of a progressive tile.
//
//	data       RLGR-compressed coefficient stream (may be empty → zeros)
//	shift      effective per-band dequant shifts (quant + progQuant - 1)
//	current    accumulated (dequantized) coefficients of the previous pass —
//	           also receives the new accumulated state (FreeRDP `current`)
//	sign       raw RLGR output of the latest pass (FreeRDP `sign`, the
//	           sign/refinement map consumed by TILE_UPGRADE passes)
//	coeffDiff  tile flags bit0: data is a delta on top of `current`
//	extrapolate region flags bit0: extrapolated subband layout + strided DWT
//
// Returns the 64x64 coefficient array ready for the YCbCr→RGB transform
// (same backing as `buffer`).
func progDecodeComponent(data []byte, shift rfxQuant, current *coeffArr, sign *coeffArr, coeffDiff, extrapolate bool) []int16 {
	const tilePixels = rfxTileSize * rfxTileSize

	buffer := coeffPool.Get().(*coeffArr)
	work := (*buffer)[:]

	if len(data) == 0 {
		clear(work)
	} else {
		// Progressive codec always uses RLGR mode 1.
		work = rlgr1Decode(data, tilePixels, work)
	}

	// FreeRDP keeps the raw RLGR output as the tile's `sign` map — the
	// per-coefficient magnitude/sign refinement indices consumed by
	// TILE_UPGRADE passes.
	copy((*sign)[:], work[:tilePixels])

	if extrapolate {
		// Extrapolated band layout — see the table above.
		progShiftBand(work[0:1023], shift.HL1)
		progShiftBand(work[1023:2046], shift.LH1)
		progShiftBand(work[2046:3007], shift.HH1)
		progShiftBand(work[3007:3279], shift.HL2)
		progShiftBand(work[3279:3551], shift.LH2)
		progShiftBand(work[3551:3807], shift.HH2)
		progShiftBand(work[3807:3879], shift.HL3)
		progShiftBand(work[3879:3951], shift.LH3)
		progShiftBand(work[3951:4015], shift.HH3)
		// LL3: 9x9=81 coefficients, plain DPCM then dequant.
		for i := 4016; i < 4096; i++ {
			work[i] += work[i-1]
		}
		progShiftBand(work[4015:4096], shift.LL3)
	} else {
		// Standard layout: LL3 (64 coeffs at 4032) DPCM + dequant, then the
		// remaining bands.
		for i := 4033; i < 4096; i++ {
			work[i] += work[i-1]
		}
		progShiftBand(work[0:1024], shift.HL1)
		progShiftBand(work[1024:2048], shift.LH1)
		progShiftBand(work[2048:3072], shift.HH1)
		progShiftBand(work[3072:3328], shift.HL2)
		progShiftBand(work[3328:3584], shift.LH2)
		progShiftBand(work[3584:3840], shift.HH2)
		progShiftBand(work[3840:3904], shift.HL3)
		progShiftBand(work[3904:3968], shift.LH3)
		progShiftBand(work[3968:4032], shift.HH3)
		progShiftBand(work[4032:4096], shift.LL3)
	}

	// Accumulate onto / seed the per-tile `current` state (FreeRDP
	// dwt_2d_decode coeffDiff + CopyMemory semantics).
	if coeffDiff {
		cur := (*current)[:]
		for i := 0; i < tilePixels; i++ {
			work[i] += cur[i]
		}
		copy(cur, work[:tilePixels])
	} else {
		copy((*current)[:], work[:tilePixels])
	}

	if extrapolate {
		progDWT2DLevel(work[3807:], 3)
		progDWT2DLevel(work[3007:], 2)
		progDWT2DLevel(work[0:], 1)
	} else {
		rfxInverseDWT2D(work)
	}
	return work
}

// progressive_rfx_get_band_l_count
func progBandLCount(level int) int { return (64 >> level) + 1 }

// progressive_rfx_get_band_h_count
func progBandHCount(level int) int {
	if level == 1 {
		return (64 >> 1) - 1
	}
	return (64 + (1 << (level - 1))) >> level
}

// progDWT2DLevel ports progressive_rfx_dwt_2d_decode_block: one strided
// in-place inverse DWT over an extrapolated block (buffer holds the four
// subbands of `level`; the 2^n x 2^n result lands at buffer[0] with stride
// nBandL+nBandH).
func progDWT2DLevel(buffer []int16, level int) {
	nBandL := progBandLCount(level)
	nBandH := progBandHCount(level)

	hlOff := 0
	lhOff := hlOff + nBandH*nBandL
	hhOff := lhOff + nBandL*nBandH
	llOff := hhOff + nBandH*nBandH

	HL := buffer[hlOff:]
	LH := buffer[lhOff:]
	HH := buffer[hhOff:]
	LL := buffer[llOff:]

	nDstStep := nBandL + nBandH
	temp := progTempPool.Get().(*progTempBuf)
	defer progTempPool.Put(temp)
	t := temp[:]

	L := t[0 : nBandL*nDstStep]
	H := t[nBandL*nDstStep : nBandL*nDstStep+nBandH*nDstStep]

	// horizontal (LL + HL -> L), (LH + HH -> H)
	progIDWTx(LL, nBandL, HL, nBandH, L, nDstStep, nBandL, nBandH, nBandL)
	progIDWTx(LH, nBandL, HH, nBandH, H, nDstStep, nBandL, nBandH, nBandH)
	// vertical (L + H -> result at buffer[0], strided)
	progIDWTy(L, nDstStep, H, nDstStep, buffer, nDstStep, nBandL, nBandH, nBandL+nBandH)
}

// progIDWTx ports progressive_rfx_idwt_x (horizontal 1-D lifting, edge-aware).
func progIDWTx(pLowBand []int16, nLowStep int, pHighBand []int16, nHighStep int,
	pDstBand []int16, nDstStep int, nLowCount, nHighCount, nDstCount int) {
	for i := 0; i < nDstCount; i++ {
		pL := pLowBand[i*nLowStep:]
		pH := pHighBand[i*nHighStep:]
		pX := pDstBand[i*nDstStep:]

		H0 := pH[0]
		L0 := pL[0]
		X0 := L0 - H0
		X2 := L0 - H0

		for j := 0; j < nHighCount-1; j++ {
			H1 := pH[j+1]
			L0 := pL[j+1]
			X2 = L0 - ((H0 + H1) / 2)
			X1 := ((X0 + X2) / 2) + (2 * H0)
			pX[2*j] = X0
			pX[2*j+1] = X1
			X0 = X2
			H0 = H1
		}

		switch {
		case nLowCount <= nHighCount+1 && nLowCount <= nHighCount:
			pX[2*(nHighCount-1)] = X2
			pX[2*(nHighCount-1)+1] = X2 + (2 * H0)
		case nLowCount <= nHighCount+1:
			k := nHighCount - 1
			L0 = pL[nHighCount]
			X0 = L0 - H0
			pX[2*k] = X2
			pX[2*k+1] = ((X0 + X2) / 2) + (2 * H0)
			pX[2*k+2] = X0
		default:
			k := nHighCount - 1
			L0 = pL[nHighCount]
			X0 = L0 - (H0 / 2)
			pX[2*k] = X2
			pX[2*k+1] = ((X0 + X2) / 2) + (2 * H0)
			pX[2*k+2] = X0
			L0 = pL[nHighCount+1]
			pX[2*k+3] = (X0 + L0) / 2
		}
	}
}

// progIDWTy ports progressive_rfx_idwt_y (vertical 1-D lifting, edge-aware).
// 全索引访问：C 版指针越过末尾不解引用是合法的，Go 切片会 panic，故不前进切片。
func progIDWTy(pLowBand []int16, nLowStep int, pHighBand []int16, nHighStep int,
	pDstBand []int16, nDstStep int, nLowCount, nHighCount, nDstCount int) {
	for i := 0; i < nDstCount; i++ {
		pL := i // 列基准：C 版 pLowBand++ 每列 +1
		pH := i
		pX := i

		H0 := pHighBand[pH]
		pH += nHighStep
		L0 := pLowBand[pL]
		pL += nLowStep
		X0 := L0 - H0
		X2 := L0 - H0

		for j := 0; j < nHighCount-1; j++ {
			if pH >= len(pHighBand) {
				// 防御：正常数据不应越界（越界即上游解析错位），跳过剩余
				// 系数避免拖垮整个会话。
				slog.Warn("RFX progressive: idwt_y high-band index out of range, truncating tile", "i", i, "j", j, "pH", pH, "len", len(pHighBand))
				return
			}
			H1 := pHighBand[pH]
			pH += nHighStep
			L0 = pLowBand[pL]
			pL += nLowStep
			X2 = L0 - ((H0 + H1) / 2)
			X1 := ((X0 + X2) / 2) + (2 * H0)
			pDstBand[pX] = X0
			pDstBand[pX+nDstStep] = X1
			pX += 2 * nDstStep
			X0 = X2
			H0 = H1
		}

		if nLowCount <= nHighCount+1 {
			if nLowCount <= nHighCount {
				pDstBand[pX] = X2
				pDstBand[pX+nDstStep] = X2 + (2 * H0)
			} else {
				L0 = pLowBand[pL]
				X0 = L0 - H0
				pDstBand[pX] = X2
				pDstBand[pX+nDstStep] = ((X0 + X2) / 2) + (2 * H0)
				pDstBand[pX+2*nDstStep] = X0
			}
		} else {
			L0 = pLowBand[pL]
			pL += nLowStep
			X0 = L0 - (H0 / 2)
			pDstBand[pX] = X2
			pDstBand[pX+nDstStep] = ((X0 + X2) / 2) + (2 * H0)
			pDstBand[pX+2*nDstStep] = X0
			pX += 3 * nDstStep
			L0 = pLowBand[pL]
			pDstBand[pX] = (X0 + L0) / 2
		}
	}
}

// ---- TILE_UPGRADE (progressive refinement passes) ----

// progUpgradeState ports RFX_PROGRESSIVE_UPGRADE_STATE + wBitStream
// (MSB-first bit consumption over the SRL/RAW segments).
type progUpgradeState struct {
	srl      []byte
	raw      []byte
	srlPos   int  // bits consumed from srl
	rawPos   int  // bits consumed from raw
	kp       int  // SRL zero-run parameter (scaled by 8)
	nz       int  // pending zero-run count
	mode     bool // false: zero-encoding next, true: unary next
}

func (st *progUpgradeState) srlBit() int {
	idx := st.srlPos >> 3
	if idx >= len(st.srl) {
		st.srlPos++
		return 0
	}
	b := (st.srl[idx] >> (7 - uint(st.srlPos&7))) & 1
	st.srlPos++
	return int(b)
}

func (st *progUpgradeState) rawBits(n int) int {
	v := 0
	for k := 0; k < n; k++ {
		idx := st.rawPos >> 3
		if idx >= len(st.raw) {
			st.rawPos++
			continue
		}
		b := (st.raw[idx] >> (7 - uint(st.rawPos&7))) & 1
		st.rawPos++
		v = (v << 1) | int(b)
	}
	return v
}

// progSRLRead ports progressive_rfx_srl_read.
func (st *progUpgradeState) srlRead(numBits int) int16 {
	if st.nz > 0 {
		st.nz--
		return 0
	}

	k := st.kp / 8

	if !st.mode {
		// zero encoding
		bit := st.srlBit()
		if bit == 0 {
			// '0' bit: nz >= (1 << k)
			st.nz = 1 << k
			st.kp += 4
			if st.kp > 80 {
				st.kp = 80
			}
			st.nz--
			return 0
		}
		// '1' bit: nz < (1 << k); nz = next k bits, unary encoding is next
		st.nz = 0
		st.mode = true
		if k > 0 {
			v := 0
			for i := 0; i < k; i++ {
				v = (v << 1) | st.srlBit()
			}
			st.nz = v
		}
		if st.nz > 0 {
			st.nz--
			return 0
		}
	}

	st.mode = false // zero encoding is next
	// unary encoding: read sign bit
	sign := st.srlBit()

	if st.kp < 6 {
		st.kp = 0
	} else {
		st.kp -= 6
	}

	if numBits == 1 {
		if sign != 0 {
			return -1
		}
		return 1
	}

	mag := 1
	max := (1 << numBits) - 1
	for mag < max {
		bit := st.srlBit()
		if bit != 0 {
			break
		}
		mag++
	}
	if sign != 0 {
		return int16(-mag)
	}
	return int16(mag)
}

// progUpgradeBlock ports progressive_rfx_upgrade_block: refines one subband
// in the accumulated `buffer` using the sign map + SRL/RAW bit streams.
func progUpgradeBlock(buffer, sign []int16, length int, shift, bitPos, numBits uint8, st *progUpgradeState, nonLL bool) {
	if numBits == 0 {
		return
	}
	if !nonLL {
		// LL3: raw-only refinement
		for idx := 0; idx < length; idx++ {
			input := st.rawBits(int(numBits))
			buffer[idx] += int16(input << (shift & 31))
		}
		return
	}
	for idx := 0; idx < length; idx++ {
		var input int16
		switch {
		case sign[idx] > 0:
			input = int16(st.rawBits(int(numBits)))
		case sign[idx] < 0:
			input = -int16(st.rawBits(int(numBits)))
		default:
			input = st.srlRead(int(numBits))
			sign[idx] = input
		}
		buffer[idx] += int16(input << (shift & 31))
	}
}

// progUpgradeComponent ports progressive_rfx_upgrade_component: refines all
// ten bands of one component in the accumulated coefficient buffer, then
// runs the (extrapolate or standard) inverse DWT.
func progUpgradeComponent(shift, bitPos, numBits rfxQuant, current, sign []int16,
	srlData []byte, rawData []byte, coeffDiff, subbandDiff, extrapolate bool) []int16 {

	st := progUpgradeState{kp: 8, srl: srlData, raw: rawData}

	// nonLL bands (extrapolated layout, consistent with the FIRST pass)
	progUpgradeBlock(current[0:1023], sign[0:1023], 1023, shift.HL1, bitPos.HL1, numBits.HL1, &st, true)
	progUpgradeBlock(current[1023:2046], sign[1023:2046], 1023, shift.LH1, bitPos.LH1, numBits.LH1, &st, true)
	progUpgradeBlock(current[2046:3007], sign[2046:3007], 961, shift.HH1, bitPos.HH1, numBits.HH1, &st, true)
	progUpgradeBlock(current[3007:3279], sign[3007:3279], 272, shift.HL2, bitPos.HL2, numBits.HL2, &st, true)
	progUpgradeBlock(current[3279:3551], sign[3279:3551], 272, shift.LH2, bitPos.LH2, numBits.LH2, &st, true)
	progUpgradeBlock(current[3551:3807], sign[3551:3807], 256, shift.HH2, bitPos.HH2, numBits.HH2, &st, true)
	progUpgradeBlock(current[3807:3879], sign[3807:3879], 72, shift.HL3, bitPos.HL3, numBits.HL3, &st, true)
	progUpgradeBlock(current[3879:3951], sign[3879:3951], 72, shift.LH3, bitPos.LH3, numBits.LH3, &st, true)
	progUpgradeBlock(current[3951:4015], sign[3951:4015], 64, shift.HH3, bitPos.HH3, numBits.HH3, &st, true)

	// LL3 (81 coefficients)
	progUpgradeBlock(current[4015:4096], sign[4015:4096], 81, shift.LL3, bitPos.LL3, numBits.LL3, &st, false)

	// FreeRDP verifies consumed bits vs declared lengths here; on mismatch the
	// whole frame is dropped. We accept the refined coefficients as-is.

	// reverse=TRUE dwt_2d_decode: buffer = current, then transform in place.
	out := coeffPool.Get().(*coeffArr)
	work := (*out)[:]
	if coeffDiff {
		// (tile difference on upgrades is not produced by current servers;
		// current already holds the accumulated coefficients)
		_ = coeffDiff
	}
	copy(work, current)
	if extrapolate {
		progDWT2DLevel(work[3807:], 3)
		progDWT2DLevel(work[3007:], 2)
		progDWT2DLevel(work[0:], 1)
	} else {
		rfxInverseDWT2D(work)
	}
	return work
}

// ProgSession is an exported sequential-decode handle for offline tools.
type ProgSession struct {
	d *rfxProgressiveDecoder
}

// NewProgSession creates a progressive decoder with persistent state.
func NewProgSession() *ProgSession { return &ProgSession{d: newRfxProgressiveDecoder()} }

// Decode decodes one progressive frame onto the surface buffer.
func (p *ProgSession) Decode(payload []byte, w, h int) []byte {
	out := make([]byte, w*h*4)
	for i := 3; i < len(out); i += 4 {
		out[i] = 0xFF
	}
	p.d.progressiveDecode(payload, out, w, h)
	return out
}
