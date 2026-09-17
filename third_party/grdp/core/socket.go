package core

import (
	"os"
	"bufio"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"log/slog"
	"math/big"
	"net"
	"time"
)

// readBufSize is the size of the buffered reader used for socket reads.
// RDP packets can be large (bitmap updates, channel data); a 64 KiB buffer
// keeps the number of read(2) syscalls low without wasting memory.
const readBufSize = 65536

// tcpRecvBufSize is the OS-level TCP receive socket buffer size.
// The default on most systems (~87 KiB on Linux, ~128 KiB on macOS) is too
// small for high-resolution RDP sessions where the server can burst several
// MiB of bitmap/H.264 data per frame.  512 KiB allows the kernel to buffer
// more in-flight data, reducing stalls when the application goroutine is
// briefly busy decoding a previous frame.
const tcpRecvBufSize = 512 * 1024

type SocketLayer struct {
	conn       net.Conn
	tlsConn    *tls.Conn
	reader     *bufio.Reader // buffers reads regardless of TLS state
	serverName string
}

func NewSocketLayer(conn net.Conn, serverName string) *SocketLayer {
	// Disable Nagle's algorithm so small DVC responses are sent immediately.
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
		// Increase the OS receive buffer so the kernel can absorb large bitmap
		// or H.264 bursts without dropping bytes while the decoder is busy.
		// SetReadBuffer is a best-effort hint; ignore errors (e.g. restricted
		// by the OS cap in /proc/sys/net/core/rmem_max on Linux).
		_ = tc.SetReadBuffer(tcpRecvBufSize)
	}
	l := &SocketLayer{
		conn:       conn,
		tlsConn:    nil,
		serverName: serverName,
	}
	l.reader = bufio.NewReaderSize(conn, readBufSize)
	return l
}

func (s *SocketLayer) SetDeadline(t time.Time) error {
	return s.conn.SetDeadline(t)
}

func (s *SocketLayer) Read(b []byte) (n int, err error) {
	return s.reader.Read(b)
}

func (s *SocketLayer) Write(b []byte) (n int, err error) {
	if s.tlsConn != nil {
		return s.tlsConn.Write(b)
	}
	return s.conn.Write(b)
}

func (s *SocketLayer) Close() error {
	if s.tlsConn != nil {
		s.tlsConn.Close() // best-effort; always close the underlying TCP socket
	}
	return s.conn.Close()
}

func (s *SocketLayer) StartTLS() error {
	config := &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         s.serverName,
		MinVersion:         tls.VersionTLS12,
		MaxVersion:         tls.VersionTLS12,
		//		MaxVersion:               tls.VersionTLS13,
	}
	// RDP_TLS_KEYLOG: dump TLS session keys for wireshark/tshark decryption of
	// server-side drops (native builds only — wasm has no filesystem).
	if p := os.Getenv("RDP_TLS_KEYLOG"); p != "" {
		if f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			config.KeyLogWriter = f
		}
	}
	tlsConn := tls.Client(s.conn, config)
	if err := tlsConn.Handshake(); err != nil {
		return err
	}
	s.tlsConn = tlsConn
	slog.Warn("RDP TLS established", "version", tlsConn.ConnectionState().Version,
		"cipher", tlsConn.ConnectionState().CipherSuite,
		"peerCerts", len(tlsConn.ConnectionState().PeerCertificates))
	// Reset the buffered reader to read from the TLS connection.
	// Reset discards any unconsumed buffered bytes from the plain-text phase,
	// which is correct because the TLS handshake has already consumed them.
	s.reader.Reset(tlsConn)
	return nil
}

type PublicKey struct {
	N *big.Int `asn1:"explicit,tag:0"` // modulus
	E int      `asn1:"explicit,tag:1"` // public exponent
}

func (s *SocketLayer) TlsPubKey() ([]byte, error) {
	if s.tlsConn == nil {
		return nil, errors.New("TLS conn does not exist")
	}
	pub := s.tlsConn.ConnectionState().PeerCertificates[0].PublicKey
	switch key := pub.(type) {
	case *rsa.PublicKey:
		// RSA: keep the original PKCS#1 encoding (verified working against xrdp NLA; equivalent to OpenSSL i2d_PublicKey(RSA))
		return asn1.Marshal(*key)
	case *ecdsa.PublicKey:
		// ECDSA (e.g. gnome-remote-desktop): FreeRDP servers use OpenSSL i2d_PublicKey,
		// which yields an uncompressed EC point (0x04||X||Y) for EC keys — not SPKI.
		// It must match byte-for-byte to pass the CredSSP public key echo check.
		size := (key.Curve.Params().BitSize + 7) / 8
		out := make([]byte, 1+2*size)
		out[0] = 0x04
		key.X.FillBytes(out[1 : 1+size])
		key.Y.FillBytes(out[1+size : 1+2*size])
		return out, nil
	default:
		// other key types (Ed25519 etc.): fall back to SPKI
		return x509.MarshalPKIXPublicKey(pub)
	}
}
