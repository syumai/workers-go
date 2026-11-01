//go:build e2e

package e2e

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"
)

// closeReplyTimeout is how long the client waits for a Close frame after
// initiating (or provoking) one. A missing reply surfaces as 1006 only
// after the runtime's ~30s half-open timeout, so a few seconds is enough
// to tell a real handshake from a dropped connection.
const closeReplyTimeout = 5 * time.Second

// TestWebSocket checks the server-side WebSocket close handshake under
// workerd. Text echo is only there to prove the socket is open before the
// close; the assertion that matters is that a Close frame comes back
// promptly.
func TestWebSocket(t *testing.T) {
	w := startWrangler(t, "websocket")

	t.Run("client close", func(t *testing.T) {
		ws := dialWebSocket(t, w.BaseURL, "/ws")
		defer ws.Close()

		if err := ws.writeFrame(0x1, []byte("hello")); err != nil {
			t.Fatalf("write text frame: %v", err)
		}
		opcode, payload, err := ws.readFrame()
		if err != nil {
			t.Fatalf("read echo: %v", err)
		}
		if opcode != 0x1 || string(payload) != "hello" {
			t.Fatalf("echo frame opcode=%d payload=%q, want text \"hello\"", opcode, payload)
		}

		if err := ws.writeClose(1000, "bye"); err != nil {
			t.Fatalf("write close frame: %v", err)
		}
		start := time.Now()
		if err := ws.conn.SetReadDeadline(time.Now().Add(closeReplyTimeout)); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		code, reason, err := ws.readClose()
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("client close(1000, \"bye\") got no Close frame within %s (elapsed %s): %v", closeReplyTimeout, elapsed, err)
		}
		if code != 1000 || reason != "bye" {
			t.Fatalf("close frame = (%d, %q), want (1000, \"bye\")", code, reason)
		}
		if elapsed > closeReplyTimeout {
			t.Fatalf("close frame arrived after %s, want within %s", elapsed, closeReplyTimeout)
		}
	})

	t.Run("server close", func(t *testing.T) {
		ws := dialWebSocket(t, w.BaseURL, "/ws?mode=close-after-echo")
		defer ws.Close()

		if err := ws.writeFrame(0x1, []byte("hello")); err != nil {
			t.Fatalf("write text frame: %v", err)
		}
		opcode, payload, err := ws.readFrame()
		if err != nil {
			t.Fatalf("read echo: %v", err)
		}
		if opcode != 0x1 || string(payload) != "hello" {
			t.Fatalf("echo frame opcode=%d payload=%q, want text \"hello\"", opcode, payload)
		}

		start := time.Now()
		if err := ws.conn.SetReadDeadline(time.Now().Add(closeReplyTimeout)); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		code, reason, err := ws.readClose()
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("server Close(1000, \"server\") produced no Close frame within %s (elapsed %s): %v", closeReplyTimeout, elapsed, err)
		}
		if code != 1000 || reason != "server" {
			t.Fatalf("close frame = (%d, %q), want (1000, \"server\")", code, reason)
		}
	})
}

// wsClient is a minimal RFC 6455 client: one upgrade, masked client frames,
// unmasked server frames. It exists so the e2e module needs no WebSocket
// dependency.
type wsClient struct {
	conn net.Conn
	r    *bufio.Reader
}

func dialWebSocket(t *testing.T, baseURL, path string) *wsClient {
	t.Helper()
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse base URL: %v", err)
	}
	conn, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", u.Host, err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}

	keyRaw := make([]byte, 16)
	if _, err := rand.Read(keyRaw); err != nil {
		t.Fatalf("generate websocket key: %v", err)
	}
	key := base64.StdEncoding.EncodeToString(keyRaw)
	req := fmt.Sprintf(
		"GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		path, u.Host, key,
	)
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatalf("write upgrade request: %v", err)
	}

	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read upgrade status: %v", err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("upgrade status = %q, want 101", strings.TrimSpace(status))
	}
	wantAccept := wsAccept(key)
	gotAccept := ""
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read upgrade header: %v", err)
		}
		if line == "\r\n" || line == "\n" {
			break
		}
		name, value, ok := strings.Cut(strings.TrimRight(line, "\r\n"), ":")
		if ok && strings.EqualFold(name, "Sec-WebSocket-Accept") {
			gotAccept = strings.TrimSpace(value)
		}
	}
	if gotAccept != wantAccept {
		t.Fatalf("Sec-WebSocket-Accept = %q, want %q", gotAccept, wantAccept)
	}
	return &wsClient{conn: conn, r: br}
}

func (c *wsClient) Close() error { return c.conn.Close() }

func wsAccept(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func (c *wsClient) writeFrame(opcode byte, payload []byte) error {
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	header := []byte{0x80 | opcode}
	n := len(payload)
	switch {
	case n < 126:
		header = append(header, byte(n)|0x80)
	case n < 65536:
		header = append(header, 126|0x80, byte(n>>8), byte(n))
	default:
		return fmt.Errorf("payload too large: %d", n)
	}
	header = append(header, mask[:]...)
	masked := make([]byte, n)
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(masked)
	return err
}

func (c *wsClient) writeClose(code int, reason string) error {
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload[:2], uint16(code))
	copy(payload[2:], reason)
	return c.writeFrame(0x8, payload)
}

// readClose reads frames until a Close frame, replying to Ping so an
// intervening control frame cannot stall the handshake.
func (c *wsClient) readClose() (int, string, error) {
	for {
		opcode, payload, err := c.readFrame()
		if err != nil {
			return 0, "", err
		}
		switch opcode {
		case 0x8:
			if len(payload) < 2 {
				return 1005, "", nil
			}
			code := int(binary.BigEndian.Uint16(payload[:2]))
			return code, string(payload[2:]), nil
		case 0x9:
			if err := c.writeFrame(0xA, payload); err != nil {
				return 0, "", err
			}
		default:
			return 0, "", fmt.Errorf("unexpected opcode %d while waiting for close", opcode)
		}
	}
}

func (c *wsClient) readFrame() (byte, []byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(c.r, hdr[:]); err != nil {
		return 0, nil, err
	}
	if hdr[0]&0x80 == 0 {
		return 0, nil, fmt.Errorf("fragmented frame")
	}
	opcode := hdr[0] & 0x0f
	masked := hdr[1]&0x80 != 0
	ln := int(hdr[1] & 0x7f)
	switch ln {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.r, ext[:]); err != nil {
			return 0, nil, err
		}
		ln = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		return 0, nil, fmt.Errorf("frame too large")
	}
	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(c.r, maskKey[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, ln)
	if _, err := io.ReadFull(c.r, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	return opcode, payload, nil
}
