package workspace

import (
	"aiworkspace/internal/mathengine"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var ErrNotFound = errors.New("not found")

const MaxWidgets = 200

// Service owns all business mutations and publishes only committed events.
type Service struct {
	media       *MediaStore
	mu          sync.Mutex
	store       *Store
	states      map[string]State
	registry    Registry
	tools       map[string]Tool
	subscribers map[string]map[chan Event]bool
	observers   map[chan Event]bool
}

func ID() string { return rand.Text() }

func New(ctx context.Context, store *Store) (*Service, error) {
	states, e := store.Load(ctx)
	if e != nil {
		return nil, e
	}
	r := NewRegistry()
	s := &Service{store: store, states: states, registry: r, tools: Tools(r), subscribers: map[string]map[chan Event]bool{}}
	if _, ok := states["default"]; !ok {
		st := State{ID: "default", Name: "Моё пространство", Tabs: []Tab{{"main", "Рабочая область"}}, ActiveTab: "main", Layout: "grid", Theme: "system", Widgets: []Widget{}, Version: 1}
		ev := Event{Type: "workspace.created", WorkspaceID: st.ID, Version: st.Version, Payload: public(st)}
		if e := store.Save(ctx, st, ev, false); e != nil {
			return nil, e
		}
		states[st.ID] = st
	}
	return s, nil
}
func (s *Service) Tools() []Tool      { return SortedTools(s.tools) }
func (s *Service) Registry() Registry { return s.registry }
func clone(st State) State {
	b, _ := json.Marshal(st)
	var v State
	_ = json.Unmarshal(b, &v)
	return v
}
func public(st State) State {
	v := clone(st)
	hide := func(p *mathengine.Problem) {
		p.Expected = nil
		if !p.Revealed {
			p.Steps = nil
		}
	}
	for i := range v.Widgets {
		w := &v.Widgets[i]
		if w.Problem != nil {
			hide(w.Problem)
		}
		for j := range w.Questions {
			hide(&w.Questions[j])
		}
	}
	return v
}
func (s *Service) Snapshot(id string) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.states[id]
	if !ok {
		return State{}, ErrNotFound
	}
	return public(st), nil
}

// Subscribe and initial snapshot occur under one lock, closing the read/subscribe gap.
func (s *Service) Subscribe(id string) (State, <-chan Event, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.states[id]
	if !ok {
		return State{}, nil, nil, ErrNotFound
	}
	ch := make(chan Event, 32)
	if s.subscribers[id] == nil {
		s.subscribers[id] = map[chan Event]bool{}
	}
	s.subscribers[id][ch] = true
	cancel := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.subscribers[id][ch] {
			delete(s.subscribers[id], ch)
			close(ch)
		}
	}
	return public(st), ch, cancel, nil
}

// SubscribeEvents observes committed events from every workspace for local adapters.
func (s *Service) SubscribeEvents() (<-chan Event, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.observers == nil {
		s.observers = map[chan Event]bool{}
	}
	ch := make(chan Event, 128)
	s.observers[ch] = true
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.observers[ch] {
			delete(s.observers, ch)
			close(ch)
		}
	}
}
func (s *Service) broadcast(e Event) {
	for ch := range s.observers {
		select {
		case ch <- e:
		default:
			delete(s.observers, ch)
			close(ch)
		}
	}
	for ch := range s.subscribers[e.WorkspaceID] {
		select {
		case ch <- e:
		default:
			delete(s.subscribers[e.WorkspaceID], ch)
			close(ch)
		}
	}
}
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.observers {
		close(ch)
		delete(s.observers, ch)
	}
	for _, subs := range s.subscribers {
		for ch := range subs {
			close(ch)
			delete(subs, ch)
		}
	}
}
func (s *Service) History(ctx context.Context, id string) ([]Event, error) {
	if _, e := s.Snapshot(id); e != nil {
		return nil, e
	}
	return s.store.History(ctx, id)
}
func (s *Service) Execute(ctx context.Context, name string, raw []byte) (Result, error) {
	if name == "media_import" {
		return s.importMedia(ctx, raw)
	}
	return s.execute(ctx, name, raw, nil)
}

// ExecuteAtVersion prevents applying a user confirmation to changed workspace state.
func (s *Service) ExecuteAtVersion(ctx context.Context, name string, raw []byte, version int64) (Result, error) {
	return s.execute(ctx, name, raw, &version)
}
func (s *Service) execute(ctx context.Context, name string, raw []byte, expectedVersion *int64) (Result, error) {
	tool, ok := s.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	if len(raw) > 1<<20 {
		return nil, fmt.Errorf("arguments exceed 1 MiB")
	}
	a, e := Decode(raw, tool.InputSchema)
	if e != nil {
		return nil, e
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	str := func(k string) string { v, _ := a[k].(string); return v }
	id := str("workspace_id")
	if id == "" {
		id = "default"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "workspace_list" {
		list := []map[string]any{}
		for _, st := range s.states {
			list = append(list, map[string]any{"id": st.ID, "name": st.Name, "version": st.Version, "widget_count": len(st.Widgets)})
		}
		sort.Slice(list, func(i, j int) bool { return list[i]["id"].(string) < list[j]["id"].(string) })
		return Result{"workspaces": list}, nil
	}
	state, exists := s.states[id]
	if !exists && name != "workspace_create" {
		return nil, ErrNotFound
	}
	if expectedVersion != nil && state.Version != *expectedVersion {
		return nil, fmt.Errorf("workspace changed; request a new confirmation")
	}
	st := clone(state)
	eventType := "workspace.updated"
	deleted := false
	widgetID := str("widget_id")
	result := Result{}
	if name == "workspace_create" {
		if len(s.states) >= 50 {
			return nil, fmt.Errorf("workspace limit is 50")
		}
		id = ID()
		st = State{ID: id, Name: str("name"), Tabs: []Tab{{"main", "Рабочая область"}}, ActiveTab: "main", Layout: "grid", Theme: "system", Widgets: []Widget{}}
		eventType = "workspace.created"
	}
	if name == "workspace_get" || name == "workspace_get_state" {
		return Result{"state": public(st)}, nil
	}
	if name == "widget_list" {
		return Result{"widgets": public(st).Widgets}, nil
	}
	idx := -1
	for i := range st.Widgets {
		if st.Widgets[i].ID == widgetID {
			idx = i
			break
		}
	}
	needsWidget := strings.HasPrefix(name, "widget_") && name != "widget_create" || name == "math_check_answer" || name == "math_show_solution" || name == "chart_update" || name == "chart_delete" || name == "text_update"
	if needsWidget && idx < 0 {
		return nil, ErrNotFound
	}
	if name == "widget_get" {
		return Result{"widget": public(st).Widgets[idx]}, nil
	}
	tabExists := func(id string) bool {
		for _, t := range st.Tabs {
			if t.ID == id {
				return true
			}
		}
		return false
	}
	create := func(kind string, config map[string]any) error {
		if len(st.Widgets) >= MaxWidgets {
			return fmt.Errorf("widget limit is %d", MaxWidgets)
		}
		if e := s.registry.Validate(kind, config); e != nil {
			return e
		}
		tab := str("tab_id")
		if tab == "" {
			tab = st.ActiveTab
		}
		if !tabExists(tab) {
			return fmt.Errorf("unknown tab")
		}
		title := str("title")
		if title == "" {
			title = "Математическая практика"
			if kind == "notification" {
				title = "Сообщение от AI"
			}
		}
		widgetID = ID()
		st.Widgets = append(st.Widgets, Widget{ID: widgetID, Type: kind, Title: title, TabID: tab, Position: len(st.Widgets), Config: config})
		idx = len(st.Widgets) - 1
		eventType = "widget.created"
		return nil
	}
	switch name {
	case "workspace_create":
	case "workspace_clear":
		st.Widgets = []Widget{}
		eventType = "workspace.cleared"
	case "workspace_delete":
		if id == "default" {
			return nil, fmt.Errorf("cannot delete default workspace")
		}
		deleted = true
		eventType = "workspace.deleted"
	case "widget_create", "chart_create", "text_create", "markdown_render", "code_create", "ui_show_notification", "math_create_problem", "math_generate_quiz":
		kind := str("type")
		config, _ := a["config"].(map[string]any)
		switch name {
		case "chart_create":
			kind = "chart"
		case "text_create", "markdown_render":
			kind = "markdown"
			config = map[string]any{"content": str("content")}
		case "code_create":
			kind = "code"
			config = map[string]any{"code": str("code"), "language": str("language")}
		case "ui_show_notification":
			kind = "notification"
			level := str("level")
			if level == "" {
				level = "info"
			}
			config = map[string]any{"message": str("message"), "level": level}
		case "math_create_problem":
			kind = "math_problem"
			config = map[string]any{}
		case "math_generate_quiz":
			kind = "quiz"
			config = map[string]any{}
		}
		if e = create(kind, config); e != nil {
			return nil, e
		}
		if kind == "math_problem" || kind == "quiz" {
			count := 1
			if kind == "quiz" {
				count = 5
				if n, ok := a["count"].(float64); ok {
					count = int(n)
				}
			}
			for i := 0; i < count; i++ {
				p, err := mathengine.Generate(str("kind"), str("difficulty"))
				if err != nil {
					return nil, err
				}
				if kind == "math_problem" {
					st.Widgets[idx].Problem = &p
				} else {
					st.Widgets[idx].Questions = append(st.Widgets[idx].Questions, p)
				}
			}
		}
	case "widget_update", "chart_update", "text_update":
		w := &st.Widgets[idx]
		if name == "chart_update" && w.Type != "chart" || name == "text_update" && w.Type != "markdown" {
			return nil, fmt.Errorf("wrong widget type")
		}
		if v := str("title"); v != "" {
			w.Title = v
		}
		config, has := a["config"].(map[string]any)
		if name == "text_update" {
			config = map[string]any{"content": str("content")}
			has = true
		}
		if has {
			if w.Type == "math_problem" || w.Type == "quiz" {
				return nil, fmt.Errorf("math config is immutable")
			}
			if err := s.registry.Validate(w.Type, config); err != nil {
				return nil, err
			}
			w.Config = config
		}
		mathChanged := false
		if params, ok := a["math"].(map[string]any); ok {
			if w.Type != "math_problem" || w.Problem == nil {
				return nil, fmt.Errorf("math updates require a math_problem widget")
			}
			if len(params) == 0 {
				return nil, fmt.Errorf("provide math.kind or math.difficulty")
			}
			kind, difficulty := w.Problem.Kind, w.Problem.Difficulty
			if v, ok := params["kind"].(string); ok {
				kind = v
			}
			if v, ok := params["difficulty"].(string); ok {
				difficulty = v
			}
			problem, err := mathengine.Generate(kind, difficulty)
			if err != nil {
				return nil, err
			}
			w.Problem = &problem
			mathChanged = true
		}
		if str("title") == "" && !has && !mathChanged {
			return nil, fmt.Errorf("provide title, config or math")
		}
		eventType = "widget.updated"
	case "widget_delete", "chart_delete":
		if name == "chart_delete" && st.Widgets[idx].Type != "chart" {
			return nil, fmt.Errorf("not a chart")
		}
		st.Widgets = append(st.Widgets[:idx], st.Widgets[idx+1:]...)
		eventType = "widget.deleted"
		idx = -1
	case "widget_move":
		w := &st.Widgets[idx]
		if tab := str("tab_id"); tab != "" {
			if !tabExists(tab) {
				return nil, fmt.Errorf("unknown tab")
			}
			w.TabID = tab
		}
		position := int(a["position"].(float64))
		old := w.Position
		for i := range st.Widgets {
			other := &st.Widgets[i]
			if other.ID == w.ID || other.TabID != w.TabID {
				continue
			}
			if old > position && other.Position >= position && other.Position < old {
				other.Position++
			}
			if old < position && other.Position > old && other.Position <= position {
				other.Position--
			}
		}
		w.Position = position
		eventType = "widget.moved"
	case "math_check_answer", "math_show_solution":
		w := &st.Widgets[idx]
		p := w.Problem
		if w.Type == "quiz" {
			q, ok := a["question_index"].(float64)
			if !ok || int(q) >= len(w.Questions) {
				return nil, fmt.Errorf("valid question_index required for quiz")
			}
			p = &w.Questions[int(q)]
		}
		if p == nil {
			return nil, fmt.Errorf("widget is not a math problem or quiz")
		}
		if name == "math_show_solution" {
			p.Revealed = true
			result["steps"] = p.Steps
		} else {
			correct, err := mathengine.Check(*p, str("answer"))
			if err != nil {
				return nil, err
			}
			if len(p.Attempts) >= 100 {
				return nil, fmt.Errorf("attempt limit reached")
			}
			p.Attempts = append(p.Attempts, mathengine.Attempt{Answer: str("answer"), Correct: correct})
			result["correct"] = correct
		}
		eventType = "math.updated"
	case "ui_create_tab":
		if len(st.Tabs) >= 20 {
			return nil, fmt.Errorf("tab limit is 20")
		}
		tab := Tab{ID: ID(), Title: str("title")}
		st.Tabs = append(st.Tabs, tab)
		st.ActiveTab = tab.ID
		result["tab"] = tab
		eventType = "ui.tab_created"
	case "ui_switch_tab":
		if !tabExists(str("tab_id")) {
			return nil, fmt.Errorf("unknown tab")
		}
		st.ActiveTab = str("tab_id")
		eventType = "ui.tab_switched"
	case "ui_set_layout":
		if str("layout") == "" && str("theme") == "" {
			return nil, fmt.Errorf("provide layout or theme")
		}
		if str("layout") != "" {
			st.Layout = str("layout")
		}
		if str("theme") != "" {
			st.Theme = str("theme")
		}
		eventType = "ui.layout_changed"
	default:
		return nil, fmt.Errorf("unknown command")
	}
	rawState, marshalErr := json.Marshal(st)
	if marshalErr != nil {
		return nil, marshalErr
	}
	if len(rawState) > 8<<20 {
		return nil, fmt.Errorf("workspace size exceeds 8 MiB")
	}
	st.Version++
	safe := public(st)
	event := Event{Type: eventType, WorkspaceID: id, WidgetID: widgetID, Version: st.Version, Payload: safe}
	if e = s.store.Save(ctx, st, event, deleted); e != nil {
		return nil, fmt.Errorf("persist command: %w", e)
	}
	if deleted {
		delete(s.states, id)
	} else {
		s.states[id] = st
	}
	s.broadcast(event)
	result["workspace_id"] = id
	result["version"] = st.Version
	if !deleted {
		result["state"] = safe
	}
	if idx >= 0 && idx < len(safe.Widgets) {
		result["widget"] = safe.Widgets[idx]
	}
	return result, nil
}
