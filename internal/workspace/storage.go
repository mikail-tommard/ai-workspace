package workspace

/*
#cgo LDFLAGS: -lsqlite3
#include <sqlite3.h>
#include <stdlib.h>
static int bind_text(sqlite3_stmt *s, int i, const char *v) { return sqlite3_bind_text(s,i,v,-1,SQLITE_TRANSIENT); }
*/
import "C"
import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// Store is a small SQLite adapter. All values are bound, never interpolated.
// Its mutex serializes a single FULLMUTEX connection and transaction boundaries.
type Store struct {
	mu   sync.Mutex
	db   *C.sqlite3
	lock *os.File
}

func Open(path string) (*Store, error) {
	s := &Store{}
	if path != ":memory:" {
		lock, e := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
		if e != nil {
			return nil, e
		}
		if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
			_ = lock.Close()
			return nil, fmt.Errorf("database already in use by another process: %w", e)
		}
		s.lock = lock
	}
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	if rc := C.sqlite3_open_v2(p, &s.db, C.SQLITE_OPEN_READWRITE|C.SQLITE_OPEN_CREATE|C.SQLITE_OPEN_FULLMUTEX, nil); rc != C.SQLITE_OK {
		err := s.err()
		s.Close()
		return nil, err
	}
	C.sqlite3_busy_timeout(s.db, 5000)
	for _, sql := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL", "PRAGMA foreign_keys=ON", "CREATE TABLE IF NOT EXISTS migrations(version INTEGER PRIMARY KEY)"} {
		if e := s.exec(sql); e != nil {
			s.Close()
			return nil, e
		}
	}
	if e := s.migrate(); e != nil {
		s.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) err() error { return fmt.Errorf("sqlite: %s", C.GoString(C.sqlite3_errmsg(s.db))) }
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		if C.sqlite3_close(s.db) != C.SQLITE_OK {
			return s.err()
		}
		s.db = nil
	}
	if s.lock != nil {
		_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
		if e := s.lock.Close(); e != nil {
			return e
		}
		s.lock = nil
	}
	return nil
}

func (s *Store) exec(query string, values ...string) error {
	q := C.CString(query)
	defer C.free(unsafe.Pointer(q))
	var st *C.sqlite3_stmt
	if C.sqlite3_prepare_v2(s.db, q, -1, &st, nil) != C.SQLITE_OK {
		return s.err()
	}
	defer C.sqlite3_finalize(st)
	for i, v := range values {
		p := C.CString(v)
		rc := C.bind_text(st, C.int(i+1), p)
		C.free(unsafe.Pointer(p))
		if rc != C.SQLITE_OK {
			return s.err()
		}
	}
	rc := C.sqlite3_step(st)
	if rc != C.SQLITE_DONE && rc != C.SQLITE_ROW {
		return s.err()
	}
	return nil
}
func (s *Store) Load(ctx context.Context) (map[string]State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	q := C.CString("SELECT state FROM workspaces ORDER BY id")
	defer C.free(unsafe.Pointer(q))
	var st *C.sqlite3_stmt
	if C.sqlite3_prepare_v2(s.db, q, -1, &st, nil) != C.SQLITE_OK {
		return nil, s.err()
	}
	defer C.sqlite3_finalize(st)
	out := map[string]State{}
	for {
		rc := C.sqlite3_step(st)
		if rc == C.SQLITE_DONE {
			break
		}
		if rc != C.SQLITE_ROW {
			return nil, s.err()
		}
		var state State
		if e := json.Unmarshal([]byte(C.GoString((*C.char)(unsafe.Pointer(C.sqlite3_column_text(st, 0))))), &state); e != nil {
			return nil, e
		}
		out[state.ID] = state
	}
	return out, nil
}
func (s *Store) Save(ctx context.Context, state State, event Event, deleted bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	e, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if err = s.exec("BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.exec("ROLLBACK")
		}
	}()
	if deleted {
		err = s.exec("DELETE FROM workspaces WHERE id=?", state.ID)
	} else {
		err = s.exec("INSERT INTO workspaces(id,state) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state", state.ID, string(b))
	}
	if err != nil {
		return err
	}
	if err = s.exec("INSERT INTO events(workspace_id,version,event) VALUES(?,?,?)", state.ID, fmt.Sprint(state.Version), string(e)); err != nil {
		return err
	}
	if err = s.exec("DELETE FROM events WHERE workspace_id=? AND id NOT IN (SELECT id FROM events WHERE workspace_id=? ORDER BY version DESC LIMIT 100)", state.ID, state.ID); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = s.exec("COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}
func (s *Store) History(ctx context.Context, id string) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	q := C.CString("SELECT event FROM events WHERE workspace_id=? ORDER BY version DESC LIMIT 100")
	defer C.free(unsafe.Pointer(q))
	var st *C.sqlite3_stmt
	if C.sqlite3_prepare_v2(s.db, q, -1, &st, nil) != C.SQLITE_OK {
		return nil, s.err()
	}
	defer C.sqlite3_finalize(st)
	p := C.CString(id)
	defer C.free(unsafe.Pointer(p))
	C.bind_text(st, 1, p)
	out := []Event{}
	for {
		rc := C.sqlite3_step(st)
		if rc == C.SQLITE_DONE {
			break
		}
		if rc != C.SQLITE_ROW {
			return nil, s.err()
		}
		var e Event
		if err := json.Unmarshal([]byte(C.GoString((*C.char)(unsafe.Pointer(C.sqlite3_column_text(st, 0))))), &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

//go:embed migrations/*.sql
var migrations embed.FS

func (s *Store) migrate() error {
	q := C.CString("SELECT COALESCE(MAX(version),0) FROM migrations")
	defer C.free(unsafe.Pointer(q))
	var st *C.sqlite3_stmt
	if C.sqlite3_prepare_v2(s.db, q, -1, &st, nil) != C.SQLITE_OK {
		return s.err()
	}
	if C.sqlite3_step(st) != C.SQLITE_ROW {
		C.sqlite3_finalize(st)
		return s.err()
	}
	current := int(C.sqlite3_column_int(st, 0))
	C.sqlite3_finalize(st)
	files, e := fs.Glob(migrations, "migrations/*.sql")
	if e != nil {
		return e
	}
	for _, name := range files {
		version, e := strconv.Atoi(strings.Split(strings.TrimPrefix(name, "migrations/"), "_")[0])
		if e != nil {
			return e
		}
		if version <= current {
			continue
		}
		sql, e := migrations.ReadFile(name)
		if e != nil {
			return e
		}
		if e = s.exec("BEGIN IMMEDIATE"); e != nil {
			return e
		}
		script := C.CString(string(sql))
		rc := C.sqlite3_exec(s.db, script, nil, nil, nil)
		C.free(unsafe.Pointer(script))
		if rc != C.SQLITE_OK {
			e = s.err()
			_ = s.exec("ROLLBACK")
			return e
		}
		if e = s.exec("INSERT INTO migrations(version) VALUES(?)", strconv.Itoa(version)); e != nil {
			_ = s.exec("ROLLBACK")
			return e
		}
		if e = s.exec("COMMIT"); e != nil {
			_ = s.exec("ROLLBACK")
			return e
		}
	}
	return nil
}

// Record methods let optional modules share the existing durable storage without
// importing CGO or creating independent databases. Values must be bounded JSON.
func (s *Store) GetRecord(ctx context.Context, key string) (json.RawMessage, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return nil, false, e
	}
	q := C.CString("SELECT document FROM assistant_records WHERE key=?")
	defer C.free(unsafe.Pointer(q))
	var st *C.sqlite3_stmt
	if C.sqlite3_prepare_v2(s.db, q, -1, &st, nil) != C.SQLITE_OK {
		return nil, false, s.err()
	}
	defer C.sqlite3_finalize(st)
	p := C.CString(key)
	defer C.free(unsafe.Pointer(p))
	C.bind_text(st, 1, p)
	rc := C.sqlite3_step(st)
	if rc == C.SQLITE_DONE {
		return nil, false, nil
	}
	if rc != C.SQLITE_ROW {
		return nil, false, s.err()
	}
	return json.RawMessage(C.GoString((*C.char)(unsafe.Pointer(C.sqlite3_column_text(st, 0))))), true, nil
}
func (s *Store) PutRecord(ctx context.Context, key string, value json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return e
	}
	if len(key) > 200 || len(value) > 512<<10 || !json.Valid(value) {
		return fmt.Errorf("invalid assistant record")
	}
	return s.exec("INSERT INTO assistant_records(key,document) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET document=excluded.document,updated_at=CAST(strftime('%s','now') AS INTEGER)", key, string(value))
}

// ClaimRecord reserves an execution ID before side effects. A crash leaves an
// unresolved reservation, deliberately preventing an automatic duplicate mutation.
func (s *Store) ClaimRecord(ctx context.Context, key string, value json.RawMessage) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return false, e
	}
	if len(key) > 200 || len(value) > 512<<10 || !json.Valid(value) {
		return false, fmt.Errorf("invalid assistant record")
	}
	if e := s.exec("INSERT OR IGNORE INTO assistant_records(key,document) VALUES(?,?)", key, string(value)); e != nil {
		return false, e
	}
	claimed := C.sqlite3_changes(s.db) > 0
	if claimed {
		for _, prefix := range []string{"desktop:%", "call:%", "codex-request:%"} {
			if e := s.exec("DELETE FROM assistant_records WHERE key LIKE ? AND rowid NOT IN (SELECT rowid FROM assistant_records WHERE key LIKE ? ORDER BY rowid DESC LIMIT 2000)", prefix, prefix); e != nil {
				return false, e
			}
		}
	}

	return claimed, nil
}
