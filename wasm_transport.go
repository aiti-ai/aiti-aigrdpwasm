//go:build js && wasm

package main

import (
	"fmt"
	"net"
	"sync"
	"syscall/js"
	"time"
)

// wsConn implements net.Conn over a browser WebSocket.
// It is created by dialWebSocket and injected into grdp via RdpClient.Dialer.
//
// Inbound messages use an unbounded queue: the onMessage callback runs on the
// JS thread — blocking it deadlocks the whole Go runtime — while RDP messages
// must never be dropped (dropping = stream desync = artifacting/disconnects).
// So enqueueing never blocks or discards; the Read side consumes at its own
// pace. Memory growth during bursts stays bounded by the bridge's 32MB
// backpressure protection.
type wsConn struct {
	ws      js.Value
	mu      sync.Mutex
	queue   [][]byte
	notify  chan struct{}
	pending []byte
	closed  bool
}

func dialWebSocket(proxyURL string) (net.Conn, error) {
	c := &wsConn{
		notify: make(chan struct{}, 1),
	}

	ws := js.Global().Get("WebSocket").New(proxyURL)
	ws.Set("binaryType", "arraybuffer")
	c.ws = ws

	openCh := make(chan error, 1)

	var onOpen, onError, onMessage, onClose js.Func

	onOpen = js.FuncOf(func(this js.Value, args []js.Value) any {
		openCh <- nil
		return nil
	})
	onError = js.FuncOf(func(this js.Value, args []js.Value) any {
		select {
		case openCh <- fmt.Errorf("websocket error"):
		default:
		}
		return nil
	})
	onMessage = js.FuncOf(func(this js.Value, args []js.Value) any {
		data := args[0].Get("data")
		arr := js.Global().Get("Uint8Array").New(data)
		buf := make([]byte, arr.Length())
		js.CopyBytesToGo(buf, arr)
		c.mu.Lock()
		if !c.closed {
			c.queue = append(c.queue, buf)
		}
		c.mu.Unlock()
		select {
		case c.notify <- struct{}{}:
		default:
		}
		return nil
	})
	onClose = js.FuncOf(func(this js.Value, args []js.Value) any {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		close(c.notify)
		return nil
	})

	ws.Set("onopen", onOpen)
	ws.Set("onerror", onError)
	ws.Set("onmessage", onMessage)
	ws.Set("onclose", onClose)

	err := <-openCh
	// Release open/error handlers; keep message/close handlers alive via c.ws
	onOpen.Release()
	onError.Release()
	// Store message and close handlers so they are not GC'd
	c.ws.Set("_onmessage", onMessage)
	c.ws.Set("_onclose", onClose)

	if err != nil {
		return nil, err
	}
	return c, nil
}

func (c *wsConn) Read(b []byte) (int, error) {
	// Drain pending bytes from last read first.
	if len(c.pending) > 0 {
		n := copy(b, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}

	for {
		c.mu.Lock()
		if c.closed && len(c.queue) == 0 {
			c.mu.Unlock()
			return 0, fmt.Errorf("websocket closed")
		}
		var chunk []byte
		if len(c.queue) > 0 {
			chunk = c.queue[0]
			c.queue[0] = nil
			c.queue = c.queue[1:]
		}
		c.mu.Unlock()
		if chunk != nil {
			n := copy(b, chunk)
			if n < len(chunk) {
				c.pending = chunk[n:]
			}
			return n, nil
		}
		// queue empty: wait for onMessage notification (notify is closed on Close, so Read returns immediately)
		_, ok := <-c.notify
		if !ok {
			c.mu.Lock()
			empty := len(c.queue) == 0
			c.mu.Unlock()
			if empty {
				return 0, fmt.Errorf("websocket closed")
			}
			continue
		}
	}
}

func (c *wsConn) Write(b []byte) (int, error) {
	if c.closed {
		return 0, fmt.Errorf("websocket closed")
	}
	arr := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(arr, b)
	c.ws.Call("send", arr)
	return len(b), nil
}

func (c *wsConn) Close() error {
	c.ws.Call("close")
	return nil
}

func (c *wsConn) LocalAddr() net.Addr                { return dummyAddr{} }
func (c *wsConn) RemoteAddr() net.Addr               { return dummyAddr{} }
func (c *wsConn) SetDeadline(t time.Time) error      { return nil }
func (c *wsConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *wsConn) SetWriteDeadline(t time.Time) error { return nil }

type dummyAddr struct{}

func (dummyAddr) Network() string { return "ws" }
func (dummyAddr) String() string  { return "websocket" }
