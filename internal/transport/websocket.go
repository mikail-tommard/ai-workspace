package transport

import (
	"aiworkspace/internal/workspace"
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// wsConn implements bounded RFC6455 frames, control frames and fragmentation.
// Only the read loop reads; writes are serialized with independent deadlines.
type wsConn struct {
	conn net.Conn
	r    *bufio.Reader
	mu   sync.Mutex
}

func (c *wsConn) write(op byte, p []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	header := []byte{0x80 | op}
	n := len(p)
	switch {
	case n < 126:
		header = append(header, byte(n))
	case n < 65536:
		header = append(header, 126, byte(n>>8), byte(n))
	default:
		header = append(header, 127, 0, 0, 0, 0, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	if _, e := c.conn.Write(header); e != nil {
		return e
	}
	_, e := c.conn.Write(p)
	return e
}
func (c *wsConn) json(v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return c.write(1, b)
}
func (c *wsConn) read() ([]byte, error) {
	var msg []byte
	started := false
	for {
		var h [2]byte
		if _, e := io.ReadFull(c.r, h[:]); e != nil {
			return nil, e
		}
		fin := h[0]&0x80 != 0
		op := h[0] & 15
		if h[0]&0x70 != 0 || h[1]&0x80 == 0 {
			return nil, fmt.Errorf("invalid websocket frame")
		}
		n := uint64(h[1] & 127)
		if n == 126 {
			var b [2]byte
			if _, e := io.ReadFull(c.r, b[:]); e != nil {
				return nil, e
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		} else if n == 127 {
			var b [8]byte
			if _, e := io.ReadFull(c.r, b[:]); e != nil {
				return nil, e
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if n > MaxBody || uint64(len(msg))+n > MaxBody {
			return nil, fmt.Errorf("websocket message too large")
		}
		if op >= 8 && (!fin || n > 125) {
			return nil, fmt.Errorf("invalid control frame")
		}
		var mask [4]byte
		if _, e := io.ReadFull(c.r, mask[:]); e != nil {
			return nil, e
		}
		p := make([]byte, int(n))
		if _, e := io.ReadFull(c.r, p); e != nil {
			return nil, e
		}
		for i := range p {
			p[i] ^= mask[i%4]
		}
		switch op {
		case 8:
			_ = c.write(8, []byte{3, 232})
			return nil, io.EOF
		case 9:
			if e := c.write(10, p); e != nil {
				return nil, e
			}
			continue
		case 10:
			_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			continue
		case 1:
			if started {
				return nil, fmt.Errorf("unexpected data frame")
			}
			started = true
		case 0:
			if !started {
				return nil, fmt.Errorf("unexpected continuation")
			}
		default:
			return nil, fmt.Errorf("unsupported websocket opcode")
		}
		msg = append(msg, p...)
		if fin {
			if !utf8.Valid(msg) {
				return nil, fmt.Errorf("invalid UTF-8")
			}
			return msg, nil
		}
	}
}
func upgrade(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	key := r.Header.Get("Sec-WebSocket-Key")
	decoded, e := base64.StdEncoding.DecodeString(key)
	if e != nil || len(decoded) != 16 || r.Header.Get("Sec-WebSocket-Version") != "13" || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || !strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		http.Error(w, "invalid websocket upgrade", 400)
		return nil, fmt.Errorf("invalid upgrade")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("hijacking unavailable")
	}
	conn, rw, e := hj.Hijack()
	if e != nil {
		return nil, e
	}
	hash := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	_, e = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(hash[:]))
	if e == nil {
		e = rw.Flush()
	}
	if e != nil {
		conn.Close()
		return nil, e
	}
	return &wsConn{conn: conn, r: rw.Reader}, nil
}
func (a *App) websocket(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("workspace_id")
	if id == "" {
		id = "default"
	}
	state, events, cancel, e := a.Service.Subscribe(id)
	if e != nil {
		http.Error(w, "workspace not found", 404)
		return
	}
	defer cancel()
	a.wsMu.Lock()
	if len(a.conns) >= 128 {
		a.wsMu.Unlock()
		http.Error(w, "connection limit", 503)
		return
	}
	c, e := upgrade(w, r)
	if e != nil {
		a.wsMu.Unlock()
		return
	}
	a.conns[c] = true
	a.wsMu.Unlock()
	defer func() { c.conn.Close(); a.wsMu.Lock(); delete(a.conns, c); a.wsMu.Unlock() }()
	ctx, stop := context.WithCancel(r.Context())
	defer stop()
	done := make(chan struct{})
	defer func() { c.conn.Close(); <-done }()
	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	go func() {
		defer close(done)
		defer stop()
		for {
			raw, e := c.read()
			if e != nil {
				return
			}
			var cmd struct {
				ID        string         `json:"id"`
				Tool      string         `json:"tool"`
				Arguments map[string]any `json:"arguments"`
			}
			if json.Unmarshal(raw, &cmd) != nil || cmd.Tool == "" {
				_ = c.json(map[string]any{"type": "command.error", "error": "invalid command"})
				continue
			}
			if cmd.Arguments == nil {
				cmd.Arguments = map[string]any{}
			}
			if v, ok := cmd.Arguments["workspace_id"]; ok && v != id {
				_ = c.json(map[string]any{"type": "command.error", "id": cmd.ID, "error": "workspace mismatch"})
				continue
			}
			if cmd.Tool == "workspace_create" || cmd.Tool == "workspace_list" {
				_ = c.json(map[string]any{"type": "command.error", "id": cmd.ID, "error": "use REST for workspace discovery"})
				continue
			}
			cmd.Arguments["workspace_id"] = id
			args, _ := json.Marshal(cmd.Arguments)
			commandCtx, cc := context.WithTimeout(ctx, 15*time.Second)
			res, err := a.Service.Execute(commandCtx, cmd.Tool, args)
			cc()
			reply := map[string]any{"type": "command.result", "id": cmd.ID, "result": res}
			if err != nil {
				reply["type"] = "command.error"
				reply["error"] = err.Error()
			}
			if c.json(reply) != nil {
				return
			}
		}
	}()
	if c.json(workspace.Event{Type: "state.snapshot", WorkspaceID: id, Version: state.Version, Payload: state}) != nil {
		return
	}
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if c.json(event) != nil {
				return
			}
		case <-ticker.C:
			if c.write(9, []byte("heartbeat")) != nil {
				return
			}
		}
	}
}
