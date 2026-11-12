// Package codexbridge speaks the installed Codex App Server JSON-RPC protocol.
// Authentication is owned by Codex. No OpenAI credential is read or forwarded.
package codexbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type envelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("Codex: %s (%d)", e.Message, e.Code) }

type client struct {
	mu       sync.Mutex
	writeMu  sync.Mutex
	in       io.WriteCloser
	cmd      *exec.Cmd
	pending  map[string]chan envelope
	sequence int
	done     chan struct{}
	closed   sync.Once
	handle   func(envelope)
	exitErr  error
	handlers sync.WaitGroup
}

func launch(ctx context.Context, binary, runtimeDir string, handle func(envelope)) (*client, error) {
	absolute, e := filepath.Abs(runtimeDir)
	if e != nil {
		return nil, e
	}
	runtimeDir = absolute
	if e := os.MkdirAll(runtimeDir, 0700); e != nil {
		return nil, e
	}
	cmd := exec.CommandContext(ctx, binary, "app-server", "--stdio", "-c", "log_dir="+strconv.Quote(filepath.Join(runtimeDir, "logs")), "-c", "sqlite_home="+strconv.Quote(filepath.Join(runtimeDir, "state")))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error { return signalGroup(cmd, syscall.SIGTERM) }
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "OPENAI_API_KEY=") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	// Codex owns auth/config. Never log subprocess stderr: it can contain private context.
	cmd.Stderr = io.Discard
	stdin, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	c := &client{in: stdin, cmd: cmd, pending: map[string]chan envelope{}, done: make(chan struct{}), handle: handle}
	if e = cmd.Start(); e != nil {
		return nil, fmt.Errorf("Не удалось запустить Codex: %w", e)
	}
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 8192), 4<<20)
		for scanner.Scan() {
			var msg envelope
			if json.Unmarshal(scanner.Bytes(), &msg) != nil {
				continue
			}
			if msg.Method != "" {
				if len(msg.ID) > 0 {
					c.handlers.Add(1)
					go func() { defer c.handlers.Done(); c.handle(msg) }()
				} else {
					c.handle(msg)
				}
				continue
			}
			c.mu.Lock()
			ch := c.pending[string(msg.ID)]
			delete(c.pending, string(msg.ID))
			c.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
		}
		if scanner.Err() != nil && cmd.Process != nil {
			_ = signalGroup(cmd, syscall.SIGKILL)
		}
		err := cmd.Wait()
		if err == nil {
			err = errors.New("Codex завершил соединение")
		}
		c.mu.Lock()
		c.exitErr = err
		c.mu.Unlock()
		close(c.done)
	}()
	return c, nil
}
func (c *client) write(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	b = append(b, '\n')
	_, e = c.in.Write(b)
	return e
}
func (c *client) call(ctx context.Context, method string, params any, dest any) error {
	c.mu.Lock()
	c.sequence++
	id := c.sequence
	ch := make(chan envelope, 1)
	key := strconv.Itoa(id)
	c.pending[key] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, key); c.mu.Unlock() }()
	if e := c.write(map[string]any{"id": id, "method": method, "params": params}); e != nil {
		return e
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return errors.New("Codex App Server не запустился или закрылся. Проверьте codex login status и запустите сервер из обычного терминала.")
	case msg := <-ch:
		if msg.Error != nil {
			return msg.Error
		}
		if dest != nil {
			return json.Unmarshal(msg.Result, dest)
		}
		return nil
	}
}
func (c *client) reply(id json.RawMessage, result any) error {
	return c.write(map[string]any{"id": id, "result": result})
}
func signalGroup(cmd *exec.Cmd, signal syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	e := syscall.Kill(-cmd.Process.Pid, signal)
	if errors.Is(e, syscall.ESRCH) {
		return nil
	}
	if e != nil {
		return cmd.Process.Kill()
	}
	return nil
}
func (c *client) close() {
	c.closed.Do(func() {
		_ = c.in.Close()
		select {
		case <-c.done:
		case <-time.After(time.Second):
			_ = signalGroup(c.cmd, syscall.SIGTERM)
			select {
			case <-c.done:
			case <-time.After(time.Second):
				_ = signalGroup(c.cmd, syscall.SIGKILL)
				<-c.done
			}
		}
		c.handlers.Wait()
	})
}
