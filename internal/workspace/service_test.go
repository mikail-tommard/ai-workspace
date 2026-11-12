package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newTest(t *testing.T) (*Service, *Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	s, e := New(context.Background(), db)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close(); _ = db.Close() })
	return s, db, path
}
func execute(t *testing.T, s *Service, name string, args map[string]any) Result {
	t.Helper()
	b, e := json.Marshal(args)
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.Execute(context.Background(), name, b)
	if e != nil {
		t.Fatalf("%s: %v", name, e)
	}
	return v
}
func TestAllTools(t *testing.T) {
	s, _, _ := newTest(t)
	mediaPath := testPNG(t, t.TempDir())
	if e := s.EnableMedia(filepath.Join(t.TempDir(), "media"), []string{filepath.Dir(mediaPath)}); e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	call := func(n string, a map[string]any) Result { seen[n] = true; return execute(t, s, n, a) }
	call("media_import", map[string]any{"path": mediaPath, "title": "Local photo"})
	call("workspace_list", map[string]any{})
	ws := call("workspace_create", map[string]any{"name": "Исследование"})["workspace_id"].(string)
	call("workspace_get", map[string]any{"workspace_id": ws})
	call("workspace_get_state", map[string]any{"workspace_id": ws})
	tab := call("ui_create_tab", map[string]any{"title": "Тест"})["tab"].(Tab).ID
	call("ui_switch_tab", map[string]any{"tab_id": "main"})
	call("ui_set_layout", map[string]any{"layout": "list", "theme": "dark"})
	w := call("widget_create", map[string]any{"type": "table", "title": "Данные", "config": map[string]any{"columns": []string{"x"}, "rows": [][]string{{"1"}}}})["widget"].(Widget)
	call("widget_list", map[string]any{})
	call("widget_get", map[string]any{"widget_id": w.ID})
	call("widget_update", map[string]any{"widget_id": w.ID, "title": "Обновлено"})
	call("widget_move", map[string]any{"widget_id": w.ID, "tab_id": tab, "position": 0})
	call("widget_delete", map[string]any{"widget_id": w.ID})
	math := call("math_create_problem", map[string]any{"kind": "linear", "difficulty": "easy"})["widget"].(Widget)
	if len(math.Problem.Expected) != 0 || len(math.Problem.Steps) != 0 {
		t.Fatal("answer leak")
	}
	call("math_check_answer", map[string]any{"widget_id": math.ID, "answer": "999999"})
	call("math_show_solution", map[string]any{"widget_id": math.ID})
	quiz := call("math_generate_quiz", map[string]any{"count": 2})["widget"].(Widget)
	call("math_check_answer", map[string]any{"widget_id": quiz.ID, "question_index": 0, "answer": "9999;9998"})
	call("math_show_solution", map[string]any{"widget_id": quiz.ID, "question_index": 1})
	cfg := map[string]any{"kind": "line", "points": []map[string]any{{"x": 0, "y": 1}, {"x": 1, "y": 2}}}
	chart := call("chart_create", map[string]any{"title": "График", "config": cfg})["widget"].(Widget)
	cfg["kind"] = "bar"
	call("chart_update", map[string]any{"widget_id": chart.ID, "config": cfg})
	call("chart_delete", map[string]any{"widget_id": chart.ID})
	text := call("text_create", map[string]any{"title": "Текст", "content": "# Hello"})["widget"].(Widget)
	call("text_update", map[string]any{"widget_id": text.ID, "content": "**Updated**"})
	call("markdown_render", map[string]any{"title": "Документ", "content": "<script>alert(1)</script>"})
	call("code_create", map[string]any{"title": "Go", "code": "package main", "language": "go"})
	call("ui_show_notification", map[string]any{"message": "Ready", "level": "success"})
	call("workspace_clear", map[string]any{"workspace_id": ws})
	call("workspace_delete", map[string]any{"workspace_id": ws})
	for _, tool := range s.Tools() {
		if !seen[tool.Name] {
			t.Errorf("tool not exercised: %s", tool.Name)
		}
	}
}
func TestPersistenceAndEvents(t *testing.T) {
	s, db, path := newTest(t)
	state, ch, cancel, e := s.Subscribe("default")
	if e != nil {
		t.Fatal(e)
	}
	defer cancel()
	result := execute(t, s, "math_create_problem", map[string]any{"kind": "fraction"})
	id := result["widget"].(Widget).ID
	event := <-ch
	if event.Version != state.Version+1 || event.Type != "widget.created" || event.WidgetID != id {
		t.Fatalf("event %#v", event)
	}
	s.mu.Lock()
	answer := s.states["default"].Widgets[0].Problem.Expected[0]
	s.mu.Unlock()
	res := execute(t, s, "math_check_answer", map[string]any{"widget_id": id, "answer": answer})
	if res["correct"] != true {
		t.Fatal("answer rejected")
	}
	before, _ := s.Snapshot("default")
	if e := db.Close(); e != nil {
		t.Fatal(e)
	}
	db2, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer db2.Close()
	s2, e := New(context.Background(), db2)
	if e != nil {
		t.Fatal(e)
	}
	after, _ := s2.Snapshot("default")
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if string(a) != string(b) {
		t.Fatal("state lost on restart")
	}
	history, e := s2.History(context.Background(), "default")
	if e != nil || len(history) != 3 {
		t.Fatalf("history: %d %v", len(history), e)
	}
	for _, ev := range history {
		b, _ := json.Marshal(ev)
		if strings.Contains(string(b), "expected") {
			t.Fatal("history leaks answer")
		}
	}
}
func TestValidationNoMutation(t *testing.T) {
	s, _, _ := newTest(t)
	before, _ := s.Snapshot("default")
	cases := []struct{ tool, raw string }{{"workspace_create", `{"name":"x","unknown":1}`}, {"widget_create", `{"type":"html","title":"X","config":{}}`}, {"widget_create", `{"type":"table","title":"X","config":{"columns":["a"],"rows":[["a","b"]]}}`}, {"chart_create", `{"title":"X","config":{"kind":"pie","points":[{"x":1,"y":-1}]}}`}, {"math_create_problem", `{"difficulty":"impossible"}`}, {"widget_move", `{"widget_id":"missing","position":0}`}, {"workspace_delete", `{}`}, {"ui_switch_tab", `{"tab_id":"missing"}`}, {"ui_set_layout", `{}`}, {"widget_get", `null`}, {"workspace_list", `[]`}}
	for _, tc := range cases {
		if _, e := s.Execute(context.Background(), tc.tool, []byte(tc.raw)); e == nil {
			t.Errorf("accepted %s %s", tc.tool, tc.raw)
		}
	}
	after, _ := s.Snapshot("default")
	if before.Version != after.Version {
		t.Fatal("failed command modified state")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := s.Execute(ctx, "math_create_problem", []byte(`{}`)); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestConcurrentAndSlowSubscribers(t *testing.T) {
	s, _, _ := newTest(t)
	_, slow, cancel, e := s.Subscribe("default")
	if e != nil {
		t.Fatal(e)
	}
	defer cancel()
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := s.Execute(context.Background(), "text_create", []byte(fmt.Sprintf(`{"title":"%d","content":"parallel"}`, i)))
			if e != nil {
				errs <- e
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	st, _ := s.Snapshot("default")
	if len(st.Widgets) != 40 || st.Version != 41 {
		t.Fatal("lost updates")
	}
	for range slow {
	}
	_, _, cancel2, e := s.Subscribe("default")
	if e != nil {
		t.Fatal(e)
	}
	cancel2()
	cancel2()
}
func TestWorkspaceIsolation(t *testing.T) {
	s, _, _ := newTest(t)
	id := execute(t, s, "workspace_create", map[string]any{"name": "Private"})["workspace_id"].(string)
	_, events, cancel, e := s.Subscribe(id)
	if e != nil {
		t.Fatal(e)
	}
	defer cancel()
	execute(t, s, "math_create_problem", map[string]any{})
	select {
	case <-events:
		t.Fatal("event crossed workspace boundary")
	default:
	}
	st, _ := s.Snapshot(id)
	if len(st.Widgets) != 0 {
		t.Fatal("state crossed workspace boundary")
	}
}
func TestLimits(t *testing.T) {
	s, _, _ := newTest(t)
	s.mu.Lock()
	st := s.states["default"]
	st.Widgets = make([]Widget, MaxWidgets)
	s.states["default"] = st
	s.mu.Unlock()
	if _, e := s.Execute(context.Background(), "math_create_problem", []byte(`{}`)); e == nil {
		t.Fatal("widget cap missing")
	}
}
func TestSchemaRegistry(t *testing.T) {
	r := NewRegistry()
	for kind, s := range r {
		b, e := json.Marshal(s)
		if e != nil || len(b) == 0 {
			t.Fatal(kind, e)
		}
	}
	for _, tc := range []struct {
		schema *Schema
		v      any
	}{{Integer(0, 2), 1.5}, {Str("title", 3), "long"}, {Obj(map[string]*Schema{}), map[string]any{"unknown": true}}, {Array(Str("s", 3), 1, 2), []any{}}, {Enum("a"), "b"}} {
		if e := tc.schema.Validate(tc.v, "test"); e == nil {
			t.Fatal("schema accepted invalid value")
		}
	}
}
func TestStorageRollback(t *testing.T) {
	s, db, _ := newTest(t)
	st, _ := s.Snapshot("default")
	st.Name = "must rollback"
	ev := Event{Type: "invalid.duplicate", WorkspaceID: "default", Version: st.Version, Payload: st}
	if e := db.Save(context.Background(), st, ev, false); e == nil {
		t.Fatal("duplicate event accepted")
	}
	loaded, e := db.Load(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if loaded["default"].Name == st.Name {
		t.Fatal("transaction not rolled back")
	}
}

func TestExclusiveDatabaseAndMigrations(t *testing.T) {
	_, db, path := newTest(t)
	if other, e := Open(path); e == nil {
		other.Close()
		t.Fatal("two caches opened the same database")
	}
	if e := db.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	if states, e := reopened.Load(context.Background()); e != nil || len(states) != 1 {
		t.Fatal("migration/reopen lost state", e)
	}
}
