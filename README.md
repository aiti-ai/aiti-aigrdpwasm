# grdpwasm

[English](README.md) | [中文说明](README.zh-CN.md)

A web-based RDP client built with Go WebAssembly and [grdp](https://github.com/nakagami/grdp).
Connect to a Windows Remote Desktop server directly from your browser — no plugins required.

> **This is the `aiti-aigrdpwasm` fork** of [nakagami/grdpwasm](https://github.com/nakagami/grdpwasm).
> It carries a patched `grdp` vendor fork and several feature/fix layers on top of upstream —
> see [Updates in this fork](#updates-in-this-fork) below.
> Published to npm as [`@aiti-ai/grdpwasm`](https://www.npmjs.com/package/@aiti-ai/grdpwasm).

## Updates in this fork

### xrdp / gnome-remote-desktop compatibility

- Vendored patched `grdp` fork (`third_party/grdp`): planar codec constant fix, capability
  advertisement trimmed to what this client can honor (no SURFACE_COMMANDS, no frame
  marker, no bitmap-cache REV2), NSCodec-capture server-type detection, fullscreen
  refresh recovery, ECDSA CredSSP key echo, fast-path input PDU padding, clear NLA errors.
- Automatic EGFX negotiation: connects with GFX first; if the server's actual codec is
  undecodable (xrdp sends progressive/planar only), it reconnects once in compat mode.
  GDS/Windows (H.264) connect normally.
- Lossless WASM transport: inbound messages queue without blocking the JS thread or
  dropping data (dropping caused stream desync → artifacting/disconnects); bursts are
  bounded by the bridge's backpressure.

### Frontend

- **fixedTarget mode**: the target host/port is provided by the server (injected
  `__rdpTarget`) or `?fixedTarget=true`, and can no longer be tampered with from the
  frontend; standalone mode keeps free-form connection fields.
- Virtual keyboard (programmer layout), shortcut bar, native system-keyboard proxy,
  X11/RFB keysym helpers — shared, host-agnostic components.
- xrdp color-cursor mitigation: cursors whose AND mask makes them fully opaque render
  as black blocks; they are detected (≥95% opaque ratio) and discarded.

### Packaging & delivery (@aiti-ai/grdpwasm)

- Published as an npm package. The tarball ships `main.wasm` (that's what jsDelivr/unpkg
  serve from), but `postinstall` removes the local copy after install — at runtime the
  page race-downloads the wasm from CDNs via [justget](https://github.com/justdb/justget)
  with same-origin fallback (override: `?wasm=<url>` or `localStorage['grdpwasm.wasmUrl']`).
- Server-side fallback for embedders: a backend helper (`ensureRdpWasm` pattern) can
  pre-fetch the wasm into a cache dir so the static server always has one to serve.

## Architecture

```
Browser (WASM) ──WebSocket──► proxy (Go) ──TCP──► RDP Server
```

Because browsers cannot open raw TCP sockets, a lightweight Go proxy server bridges
WebSocket connections from the browser to the RDP server's TCP port.

## Requirements

- Go 1.24 or later
- A reachable RDP server (Windows or any RDP-compatible host)

## Build

```bash
git clone https://github.com/nakagami/grdpwasm.git
cd grdpwasm
make all
```

`make all` produces:

| Output | Description |
|---|---|
| `static/main.wasm` | Go WASM binary (runs in the browser) |
| `static/wasm_exec.js` | Go runtime JS support file |
| `proxy/proxy` | WebSocket-to-TCP proxy + static file server |

## Run

```bash
make serve
# or equivalently:
./proxy/proxy -listen :8080 -static static
```

Then open **http://localhost:8080** in your browser.

### Proxy options

| Flag | Default | Description |
|---|---|---|
| `-listen` | `:8080` | Address and port to listen on |
| `-static` | `static` | Directory to serve static files from |

## Usage

1. Open `http://localhost:8080` in a browser.
2. Fill in the connection form:
   - **Host** — hostname or IP address of the RDP server
   - **Port** — RDP port (default `3389`)
   - **Domain** — Windows domain (leave blank for local accounts)
   - **User** — username
   - **Password** — password
   - **Width / Height** — initial desktop resolution
3. Click **Connect**.
4. The remote desktop appears in the canvas. Click the canvas to capture keyboard focus.
5. Click **Disconnect** to end the session.

## Keyboard & Mouse

All standard keyboard input is forwarded to the remote desktop via RDP scan codes.
Mouse move, button clicks, and scroll wheel are fully supported.

> **Note:** The browser tab must have focus for keyboard events to be forwarded.
> Click inside the canvas area if keys stop responding.

## Audio

Remote audio is streamed via RDPSND and played through the browser's Web Audio API
(PCM 44100 Hz, stereo, 16-bit signed little-endian).

## Security notes

- The proxy accepts connections from any origin. Run it only on a trusted network
  or add authentication before exposing it to the internet.
- Credentials are transmitted from the browser to the proxy over WebSocket.
  Use HTTPS/WSS (put the proxy behind a TLS-terminating reverse proxy such as
  nginx or Caddy) when accessing it over an untrusted network.

## Development

```
npm install     # install JS dependencies (justget, esbuild)
make wasm       # rebuild only the WASM binary
make proxy      # rebuild only the proxy server
make wasm_exec  # refresh wasm_exec.js from the local Go toolchain
npm run build:vendor  # rebuild static/vendor/justget.browser.js from the installed justget
make clean      # remove all build artifacts
```

## npm package

Publish the browser assets as `@aiti-ai/grdpwasm`. Consumers get everything except
`main.wasm` — the page fetches it from CDN at runtime.

```bash
npm run build:vendor   # bundle justget/browser into static/vendor/
make all               # build main.wasm + wasm_exec.js + proxy
npm publish            # prepack syncs the version into index.html; tarball ships the wasm
```

Consumers get `static/` (html/js/vendor) without `main.wasm` — the page fetches it
from CDN at runtime. Set `GRDPWASM_KEEP_WASM=1` before install to keep the local copy.

## License

GPLv3 — see [grdp LICENSE](https://github.com/nakagami/grdp/blob/master/LICENSE).
