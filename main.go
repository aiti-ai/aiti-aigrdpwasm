//go:build js && wasm

package main

import (
	"sort"
	"strings"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"syscall/js"
	"time"

	"github.com/nakagami/grdp"
	"github.com/nakagami/grdp/plugin/rdpsnd"
)

var (
	rdpClient      *grdp.RdpClient
	clientMu       sync.Mutex
	connectEpoch   int64 // connection generation: stale async callbacks from old connections must not affect the new session
	canvas         js.Value
	ctx2d          js.Value
	localClipboard string
	clipMu         sync.Mutex
	swapAltMeta    bool
)

// consoleWriter routes log output to the browser console (slog in WASM has
// no stderr; without this every Warn/Error is silently swallowed).
type consoleWriter struct{}

func (consoleWriter) Write(p []byte) (int, error) {
	js.Global().Get("console").Call("log", "[grdp] "+string(p))
	return len(p), nil
}

func main() {
	if h := js.Global().Get("console"); !h.IsUndefined() {
		slog.SetDefault(slog.New(slog.NewTextHandler(consoleWriter{}, &slog.HandlerOptions{Level: slog.LevelWarn})))
	}
	js.Global().Set("rdpConnect", js.FuncOf(jsConnect))
	js.Global().Set("rdpDisconnect", js.FuncOf(jsDisconnect))
	js.Global().Set("rdpMouseMove", js.FuncOf(jsMouseMove))
	js.Global().Set("rdpMouseDown", js.FuncOf(jsMouseDown))
	js.Global().Set("rdpMouseUp", js.FuncOf(jsMouseUp))
	js.Global().Set("rdpMouseWheel", js.FuncOf(jsMouseWheel))
	js.Global().Set("rdpKeyDown", js.FuncOf(jsKeyDown))
	js.Global().Set("rdpKeyUp", js.FuncOf(jsKeyUp))
	js.Global().Set("rdpClipboardChanged", js.FuncOf(jsClipboardChanged))
	js.Global().Set("rdpUnlock", js.FuncOf(jsUnlock))
	js.Global().Set("rdpStats", js.FuncOf(func(_ js.Value, _ []js.Value) any {
		clientMu.Lock()
		c := rdpClient
		clientMu.Unlock()
		if c == nil {
			return ""
		}
		counts := c.CodecCounts()
		parts := make([]string, 0, len(counts))
		for id, n := range counts {
			parts = append(parts, fmt.Sprintf("%d:%d", id, n))
		}
		parts = append(parts, fmt.Sprintf("65534:%d", renderedBitmaps))
		parts = append(parts, fmt.Sprintf("65533:%d", renderedIconRegion))
		sort.Strings(parts)
		return strings.Join(parts, ",")
	}))

	// Stall watchdog: after lock/logon-screen transitions Windows Server
	// sometimes stops serving a connection entirely — no frames, input
	// ignored — while TCP/WS stay open ("zombie": page shows Connected but
	// nothing ever updates).  A second connection to the same session works
	// fine, so recovery = detect the stall and reconnect.  Signal: no frames
	// for 15s → nudge with a mouse move; two consecutive nudges that produce
	// zero frames → reconnect.  At most one auto-reconnect per 90s.
	go func() {
		nudges := 0
		lastReconnect := time.Now().Add(-90 * time.Second)
		hadClient := false
		for {
			time.Sleep(5 * time.Second)
			clientMu.Lock()
			c := rdpClient
			clientMu.Unlock()
			if c == nil {
				// Connection object gone: either never connected, or the WS
				// closed (rdpOnClose → jsDisconnect).  Reconnect after a
				// previous session existed — covers server-side closes that
				// the fixed-timer flow would otherwise leave dead.
				if hadClient && len(lastConnectArgs) >= 8 && time.Since(lastReconnect) > 90*time.Second {
					slog.Warn("watchdog: connection closed, reconnecting")
					setStatusText("Reconnecting…")
					lastReconnect = time.Now()
					hadClient = false
					go jsConnect(js.Undefined(), lastConnectArgs)
				}
				nudges = 0
				continue
			}
			hadClient = true
			silent := time.Since(time.UnixMilli(lastFrameAtMs.Load()))
			if silent < 15*time.Second {
				nudges = 0
				continue
			}
			c.MouseMove(640, 400)
			nudges++
			time.Sleep(9 * time.Second)
			if time.Since(time.UnixMilli(lastFrameAtMs.Load())) < 10*time.Second {
				nudges = 0 // nudge produced frames — alive
				continue
			}
			if nudges >= 2 && time.Since(lastReconnect) > 90*time.Second {
				slog.Warn("watchdog: connection stalled (no frames), reconnecting")
				setStatusText("Stalled — reconnecting…")
				jsDisconnect(js.Undefined(), nil)
				lastReconnect = time.Now()
				nudges = 0
				if len(lastConnectArgs) >= 8 {
					go jsConnect(js.Undefined(), lastConnectArgs)
				}
			}
		}
	}()

	// Block forever — JS callbacks keep things alive.
	select {}
}

// jsConnect is called from JS: rdpConnect(proxyWsURL, host, port, domain, user, password, width, height, swapAltMeta[, noGfx])
var lastConnectArgs []js.Value

var lastPassword string

// domCodeForChar maps a printable ASCII char to its DOM code for jsCodeToRDP.
func domCodeForChar(ch byte) string {
	switch {
	case ch >= 'a' && ch <= 'z':
		return "Key" + string(ch-32)
	case ch >= 'A' && ch <= 'Z':
		return "Key" + string(ch)
	case ch >= '0' && ch <= '9':
		return "Digit" + string(ch)
	}
	switch ch {
	case ' ':
		return "Space"
	case '.':
		return "Period"
	case ',':
		return "Comma"
	case '-':
		return "Minus"
	case '_':
		return "Underscore"
	case '/':
		return "Slash"
	case ';':
		return "Semicolon"
	case '=':
		return "Equal"
	case '\\':
		return "Backslash"
	case '`':
		return "Backquote"
	case '[':
		return "BracketLeft"
	case ']':
		return "BracketRight"
	case '\'':
		return "Quote"
	}
	return ""
}

// jsUnlock is called from JS: rdpUnlock() — one-click locked-session
// recovery: Ctrl+Alt+Del (SAS brings up the password field), then the
// stored password, then Enter.  User-gated (toolbar 🔓 button), so it
// never fires blind against an unlocked desktop.
func jsUnlock(_ js.Value, _ []js.Value) any {
	clientMu.Lock()
	c := rdpClient
	clientMu.Unlock()
	if c == nil {
		return "not connected"
	}
	const lctrl, lalt, del = 0x1D, 0x38, 0xE053
	go func() {
		c.KeyDown(lctrl)
		c.KeyDown(lalt)
		c.KeyDown(del)
		time.Sleep(120 * time.Millisecond)
		c.KeyUp(del)
		c.KeyUp(lalt)
		c.KeyUp(lctrl)
		time.Sleep(2500 * time.Millisecond) // secure-screen transition
		// type the stored password (scan via keymap) then Enter
		for _, ch := range lastPassword {
			code := jsCodeToRDP(domCodeForChar(byte(ch)), false)
			if code == 0 {
				continue
			}
			c.KeyDown(code)
			time.Sleep(60 * time.Millisecond)
			c.KeyUp(code)
			time.Sleep(60 * time.Millisecond)
		}
		c.KeyDown(0x1C)
		time.Sleep(60 * time.Millisecond)
		c.KeyUp(0x1C)
		setStatusText("Unlock sequence sent")
	}()
	return "unlock sequence started"
}

func jsConnect(_ js.Value, args []js.Value) any {
	if len(args) < 8 {
		return fmt.Sprintf("usage: rdpConnect(proxyWsURL, host, port, domain, user, password, width, height[, swapAltMeta[, noGfx]])")
	}
	lastConnectArgs = args
	lastPassword = args[5].String()
	touchFrame()
	proxyWsURL := args[0].String()
	host := args[1].String()
	port := args[2].String()
	domain := args[3].String()
	user := args[4].String()
	password := args[5].String()
	width := args[6].Int()
	height := args[7].Int()
	if len(args) >= 9 {
		swapAltMeta = args[8].Bool()
	} else {
		swapAltMeta = false
	}
	noGfx := len(args) >= 10 && args[9].Bool()

	go func() {
		// Auto-negotiation: connect with EGFX first. If the codec the server
		// actually sends is not decodable by this client (xrdp only sends
		// progressive/planar — no AVC420 frames within 3s), disconnect and
		// reconnect once in compat mode (EGFX disabled, legacy bitmaps).
		// GDS/Windows serve H.264 and are unaffected.
		noGfxTry := false
		for attempt := 0; attempt < 2; attempt++ {
			incompat := make(chan struct{}, 1)
			err := connect(proxyWsURL, host, port, domain, user, password, width, height, noGfx || noGfxTry, incompat)
			if err == errIncompatibleGfx {
				slog.Info("server gfx incompatible — retrying in compat (no-EGFX) mode")
				setStatusText("xrdp detected: reconnecting in compat mode…")
				noGfxTry = true
				time.Sleep(300 * time.Millisecond)
				continue
			}
			if err != nil {
				slog.Error("connect", "err", err)
				js.Global().Call("rdpOnError", err.Error())
				return
			}
			// Connected. In auto mode keep listening for a late incompatibility
			// signal (the verdict point is 3s after the first surface frame,
			// later than the built-in wait window in connect).
			if !noGfxTry {
				select {
				case <-incompat:
					slog.Info("server gfx incompatible (late) — retrying in compat (no-EGFX) mode")
					setStatusText("xrdp detected: reconnecting in compat mode…")
					noGfxTry = true
					time.Sleep(300 * time.Millisecond)
					continue
				case <-time.After(10 * time.Second):
					return
				}
			}
			return
		}
	}()
	return nil
}

// errIncompatibleGfx means the negotiated server GFX codec is not decodable
// by this client (e.g. xrdp's RFX-progressive); callers should retry in
// compat (no-EGFX) mode.
var errIncompatibleGfx = errors.New("incompatible gfx codec negotiated")

func setStatusText(msg string) {
	js.Global().Get("document").Call("getElementById", "status").Set("textContent", msg)
}

func connect(proxyWsURL, host, port, domain, user, password string, width, height int, noGfx bool, incompatCh chan struct{}) error {
	clientMu.Lock()
	if rdpClient != nil {
		rdpClient.Close()
		rdpClient = nil
	}
	connectEpoch++
	myEpoch := connectEpoch
	clientMu.Unlock()
	stale := func() bool { return connectEpoch != myEpoch }

	hostPort := host + ":" + port

	// Build WebSocket URL for the proxy: ws://host:port/ws
	// The target is never put into the URL: the instance id in the path locates
	// the bridge, which knows the target from its own config (env); the page HTML
	// gets window.__rdpTarget injected by the bridge for form pre-fill.
	wsURL := proxyWsURL + "/ws"

	g := grdp.NewRdpClient(hostPort, width, height, func(hp string) (net.Conn, error) {
		return dialWebSocket(wsURL)
	})
	// The WASM page decodes H.264 with a single-stream WebCodecs decoder
	// (rdpOnH264 → one VideoDecoder). AVC444/LC=2 is a dual-stream codec —
	// drawing only the main stream garbles the chroma. Advertise AVC420
	// exclusively so the server never sends LC=2 data.
	g.DisableAVC444()
	if noGfx {
		// Servers like xrdp send EGFX streams this client cannot decode
		// (progressive garbage); disabling EGFX makes them fall back to
		// the legacy bitmap path
		g.DisableGFX()
	} else if incompatCh != nil {
		g.OnIncompatibleGfx(func() {
			select {
			case incompatCh <- struct{}{}:
			default:
			}
		})
	}

	// Get canvas from DOM
	canvas = js.Global().Get("document").Call("getElementById", "rdpCanvas")
	ctx2d = canvas.Call("getContext", "2d")
	canvas.Set("width", width)
	canvas.Set("height", height)

	g.OnAudio(func(af rdpsnd.AudioFormat, data []byte) {
		cp := make([]byte, len(data))
		copy(cp, data)
		playAudio(int(af.SamplesPerSec), int(af.Channels), int(af.BitsPerSample), cp)
	})

	g.OnH264Raw(func(destX, destY, w, h int, isKey bool, data []byte) {
		touchFrame()
		jsArr := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(jsArr, data)
		js.Global().Call("rdpOnH264", destX, destY, w, h, isKey, jsArr)
	})

	uint8Ctor := js.Global().Get("Uint8Array")
	g.OnPointerHide(func() {
		js.Global().Call("rdpOnPointerHide")
	}).OnPointerCached(func(idx uint16) {
		js.Global().Call("rdpOnPointerCached", int(idx))
	}).OnPointerUpdate(func(idx, xorBpp, hotX, hotY, w, h uint16, andMask, xorData []byte) {
		andArr := uint8Ctor.New(len(andMask))
		if len(andMask) > 0 {
			js.CopyBytesToJS(andArr, andMask)
		}
		xorArr := uint8Ctor.New(len(xorData))
		if len(xorData) > 0 {
			js.CopyBytesToJS(xorArr, xorData)
		}
		js.Global().Call("rdpOnPointerUpdate",
			int(idx), int(xorBpp), int(hotX), int(hotY), int(w), int(h), andArr, xorArr)
	})

	g.OnError(func(e error) {
		slog.Debug("rdp error", "err", e)
		if stale() {
			return // stale event from an old connection, ignore
		}
		js.Global().Call("rdpOnError", e.Error())
	}).OnClose(func() {
		slog.Debug("rdp close")
		if stale() {
			return
		}
		js.Global().Call("rdpOnClose")
	}).OnSuccess(func() {
		slog.Debug("rdp success")
	}).OnReady(func() {
		slog.Debug("rdp ready")
		if stale() {
			return
		}
		js.Global().Call("rdpOnReady")
	}).OnBitmap(func(bs []grdp.Bitmap) {
		// Copy bitmap data before rendering (data is borrowed from pool)
		for i := range bs {
			d := make([]byte, len(bs[i].Data))
			copy(d, bs[i].Data)
			bs[i].Data = d
		}
		renderBitmaps(bs)
	})

	g.OnClipboard(
		func(text string) {
			// Server → client: write text to browser clipboard.
			js.Global().Call("rdpOnClipboard", text)
		},
		func() string {
			// Client → server: return current local clipboard text.
			clipMu.Lock()
			defer clipMu.Unlock()
			return localClipboard
		},
	)

	if err := g.Login(domain, user, password); err != nil {
		return err
	}

	// Auto-negotiation: the GFX caps confirm arrives during/after Login; wait a
	// short window to classify.
	//  - incompatibility signal received -> close and let the caller reconnect in compat mode
	//  - timeout without signal -> server either uses H.264 (continue), or refused
	//    EGFX and serves legacy bitmaps (continue)
	if incompatCh != nil {
		select {
		case <-incompatCh:
			g.Close()
			return errIncompatibleGfx
		case <-time.After(2 * time.Second):
		}
	}

	clientMu.Lock()
	rdpClient = g
	clientMu.Unlock()

	// NOTE: the RDPEDISP full-desktop redraw (RedrawDesktop) is currently
	// DISABLED for auto-fire.  Fixed-time or settle-triggered wobbles both
	// correlated with the pipeline stalling at zero frames (reproducible:
	// fast connect → wobble mid-setup → server render never arrives), and
	// sessions went input-dead shortly after.  Re-enable only behind a
	// verified trigger; the reconnect tile-desync it was meant to heal is
	// rarer than the stall it caused.

	// Auto-unlock: if the server shows an interactive logon UI after NLA
	// (Windows Server 2022 dockurr container does this even with
	// INFO_AUTOLOGON), send the password as keyboard input to complete
	// interactive logon.  If the session is LOCKED instead ("Press
	// Ctrl+Alt+Delete"), the plain password keystrokes are swallowed — the
	// SAS must go first — so later attempts prepend Ctrl+Alt+Del.  A session
	// already sitting at the desktop gets stray keystrokes; that is the
	// accepted cost of a blind unlock (kept for the container boot flow).
	go func() {
		// Two PLAIN password attempts (4s: interactive-logon password field;
		// 12s: slow logon screen).  No blind CAD retries — on an unlocked
		// desktop CAD locks the session / opens the security screen and the
		// typed password lands in whatever has focus.  Locked sessions are
		// handled by the one-click 🔓 unlock (rdpUnlock) or ⚡ → Ctrl+Alt+Del.
		password := []int{0x14, 0x12, 0x1F, 0x14, 0x02, 0x03, 0x04, 0x1C}
		for _, delay := range []time.Duration{4 * time.Second, 12 * time.Second} {
			time.Sleep(delay)
			if stale() {
				return
			}
			for _, sc := range password {
				if stale() {
					return
				}
				g.KeyDown(sc)
				time.Sleep(60 * time.Millisecond)
				g.KeyUp(sc)
				time.Sleep(60 * time.Millisecond)
			}
			setStatusText("Connected (auto-unlock sent)")
			time.Sleep(800 * time.Millisecond)
			if stale() {
				return
			}
			g.MouseMove(640, 400)
		}
	}()
	return nil
}

var renderedBitmaps int64

// lastFrameAtMs tracks the most recent visual frame (bitmap or H.264) — the
// stall watchdog uses it to detect connections the server has gone silent on.
var lastFrameAtMs atomic.Int64

func touchFrame() { lastFrameAtMs.Store(time.Now().UnixMilli()) }

var renderedIconRegion int64

func renderBitmaps(bs []grdp.Bitmap) {
	touchFrame()
	renderedBitmaps += int64(len(bs))
	for _, bm := range bs {
		if bm.DestLeft < 300 && bm.DestTop < 200 {
			renderedIconRegion++
		}
	}
	uint8ClampedCtor := js.Global().Get("Uint8ClampedArray")
	imageDataCtor := js.Global().Get("ImageData")

	for _, bm := range bs {
		w := bm.DestRight - bm.DestLeft + 1
		if w > bm.Width {
			w = bm.Width
		}
		h := bm.DestBottom - bm.DestTop + 1
		if h > bm.Height {
			h = bm.Height
		}
		if w <= 0 || h <= 0 {
			continue
		}

		rgba := make([]byte, w*h*4)
		if bm.BitsPerPixel == 4 {
			// Fast path: bm.Data is BGRA32; swap R↔B to produce RGBA.
			// Only extract the visible w×h region (bm.Width may be padded wider).
			srcStride := bm.Width * 4
			dstStride := w * 4
			for row := 0; row < h; row++ {
				src := bm.Data[row*srcStride:]
				dst := rgba[row*dstStride:]
				for col := 0; col < w; col++ {
					dst[col*4+0] = src[col*4+2] // R ← BGRA[2]
					dst[col*4+1] = src[col*4+1] // G
					dst[col*4+2] = src[col*4+0] // B ← BGRA[0]
					dst[col*4+3] = 0xFF        // A (forced opaque: NSCodec/AVC paths leave alpha=0 → transparent holes)
				}
			}
		} else {
			// Slow path: bm.RGBA() converts any legacy bit-depth to RGBA.
			// Only copy the visible w×h region.
			m := bm.RGBA()
			for row := 0; row < h; row++ {
				src := m.Pix[row*m.Stride : row*m.Stride+w*4]
				copy(rgba[row*w*4:], src)
			}
		}

		jsArr := uint8ClampedCtor.New(len(rgba))
		js.CopyBytesToJS(jsArr, rgba)
		imageData := imageDataCtor.New(jsArr, w, h)
		ctx2d.Call("putImageData", imageData, bm.DestLeft, bm.DestTop)
		}
}

func jsDisconnect(_ js.Value, _ []js.Value) any {
	clientMu.Lock()
	defer clientMu.Unlock()
	if rdpClient != nil {
		rdpClient.Close()
		rdpClient = nil
	}
	return nil
}

func jsMouseMove(_ js.Value, args []js.Value) any {
	if len(args) < 2 {
		return nil
	}
	clientMu.Lock()
	c := rdpClient
	clientMu.Unlock()
	if c != nil {
		c.MouseMove(args[0].Int(), args[1].Int())
	}
	return nil
}

func jsMouseDown(_ js.Value, args []js.Value) any {
	if len(args) < 3 {
		return nil
	}
	clientMu.Lock()
	c := rdpClient
	clientMu.Unlock()
	if c != nil {
		c.MouseDown(args[0].Int(), args[1].Int(), args[2].Int())
	}
	return nil
}

func jsMouseUp(_ js.Value, args []js.Value) any {
	if len(args) < 3 {
		return nil
	}
	clientMu.Lock()
	c := rdpClient
	clientMu.Unlock()
	if c != nil {
		c.MouseUp(args[0].Int(), args[1].Int(), args[2].Int())
	}
	return nil
}

func jsMouseWheel(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return nil
	}
	clientMu.Lock()
	c := rdpClient
	clientMu.Unlock()
	if c != nil {
		c.MouseWheel(args[0].Float())
	}
	return nil
}

func jsKeyDown(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return nil
	}
	clientMu.Lock()
	c := rdpClient
	clientMu.Unlock()
	if c != nil {
		code := jsCodeToRDP(args[0].String(), swapAltMeta)
		if code != 0 {
			c.KeyDown(code)
		}
	}
	return nil
}

func jsKeyUp(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return nil
	}
	clientMu.Lock()
	c := rdpClient
	clientMu.Unlock()
	if c != nil {
		code := jsCodeToRDP(args[0].String(), swapAltMeta)
		if code != 0 {
			c.KeyUp(code)
		}
	}
	return nil
}

// jsClipboardChanged is called from JS when the local clipboard text changes.
// It stores the text and notifies the RDP server.
func jsClipboardChanged(_ js.Value, args []js.Value) any {
	text := ""
	if len(args) >= 1 {
		text = args[0].String()
	}
	clipMu.Lock()
	localClipboard = text
	clipMu.Unlock()

	clientMu.Lock()
	c := rdpClient
	clientMu.Unlock()
	if c != nil {
		c.NotifyClipboardChanged()
	}
	return nil
}
