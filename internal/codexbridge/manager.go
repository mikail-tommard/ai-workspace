package codexbridge

import (
	"aiworkspace/internal/assistant"
	"aiworkspace/internal/desktop"
	"aiworkspace/internal/workspace"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrBusy = errors.New("Codex уже выполняет команду в этом пространстве. Дождитесь ответа или прервите её.")

type Pending struct {
	ID    string `json:"call_id"`
	Label string `json:"label"`
}
type State struct {
	RequestID         string   `json:"request_id"`
	Status            string   `json:"status"`
	Activity          string   `json:"activity"`
	Text              string   `json:"text"`
	Error             string   `json:"error,omitempty"`
	Pending           *Pending `json:"pending"`
	ThreadID          string   `json:"thread_id,omitempty"`
	NotificationError string   `json:"notification_error,omitempty"`
}
type Session struct {
	catalog        string
	requests       sync.Mutex
	state          State
	rpc            *client
	thread         string
	session        string
	cancel         context.CancelFunc
	decision       chan bool
	history        bool
	requestHash    string
	turnDone       chan struct{}
	turnID         string
	lastToolStatus string
	lastToolError  string
	currentItem    string
	runCtx         context.Context
}
type Manager struct {
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closed    bool
	binary    string
	runtime   string
	cwd       string
	assistant *assistant.Manager
	store     assistant.RecordStore
	desktop   *desktop.Manager
	sessions  map[string]*Session
}

func New(a *assistant.Manager, store assistant.RecordStore, d *desktop.Manager, binary, runtimeDir, cwd string) *Manager {
	if binary == "" {
		binary = "codex"
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{ctx: ctx, cancel: cancel, binary: binary, runtime: runtimeDir, cwd: cwd, assistant: a, store: store, desktop: d, sessions: map[string]*Session{}}
}
func (m *Manager) Available() bool { _, e := exec.LookPath(m.binary); return e == nil }
func (m *Manager) Snapshot(ws string) State {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[ws]; s != nil {
		return s.state
	}
	return State{Status: "idle"}
}
func (m *Manager) patch(s *Session, update func(*State)) {
	m.mu.Lock()
	update(&s.state)
	m.mu.Unlock()
}
func active(status string) bool {
	return status == "thinking" || status == "responding" || status == "confirmation"
}
func receiptKey(ws, id string) string {
	sum := sha256.Sum256([]byte(ws + ":" + id))
	return "codex-request:" + hex.EncodeToString(sum[:])
}
func (m *Manager) Start(ctx context.Context, ws, id, text string) (State, error) {
	if id == "" || len(id) > 120 || strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > 8000 {
		return State{}, errors.New("Некорректная команда Codex.")
	}
	if !m.Available() {
		return State{}, errors.New("Codex CLI не найден. Установите Codex на этом Mac и выполните codex login.")
	}
	if _, e := m.assistant.Conversation(ctx, ws); e != nil {
		return State{}, e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return State{}, errors.New("Codex отключён")
	}
	hashBytes := sha256.Sum256([]byte(text))
	hash := hex.EncodeToString(hashBytes[:])
	s := m.sessions[ws]
	if s != nil && s.state.RequestID == id {
		if s.requestHash != hash {
			return State{}, errors.New("ID уже связан с другой командой")
		}
		return s.state, nil
	}
	if s != nil && (active(s.state.Status) || s.cancel != nil) {
		return State{}, ErrBusy
	}
	if len(m.sessions) >= 4 && s == nil {
		evicted := false
		for key, old := range m.sessions {
			if old.cancel == nil && !active(old.state.Status) {
				delete(m.sessions, key)
				m.wg.Add(1)
				go func() {
					defer m.wg.Done()
					if old.rpc != nil {
						old.rpc.close()
					}
					if old.session != "" {
						_ = m.assistant.End(context.Background(), "codex", old.session)
					}
				}()
				evicted = true
				break
			}
		}
		if !evicted {
			return State{}, errors.New("Лимит: четыре активных пространства Codex")
		}
	}
	key := receiptKey(ws, id)
	raw, found, e := m.store.GetRecord(ctx, key)
	if e != nil {
		return State{}, e
	}
	if found {
		var old struct {
			Hash  string `json:"hash"`
			State State  `json:"state"`
		}
		if e = json.Unmarshal(raw, &old); e != nil {
			return State{}, e
		}
		if old.Hash != hash {
			return State{}, errors.New("ID уже использован")
		}
		if active(old.State.Status) {
			return State{}, errors.New("Исход прежней команды неизвестен после перезапуска. Проверьте панель перед повтором.")
		}
		return old.State, nil
	}
	if s == nil {
		s = &Session{}
		m.sessions[ws] = s
	}
	s.state = State{RequestID: id, Status: "thinking", Activity: "Подключаю локальный Codex…"}
	s.requestHash = hash
	s.lastToolStatus = ""
	s.lastToolError = ""
	raw, _ = json.Marshal(map[string]any{"hash": hash, "state": s.state})
	claimed, e := m.store.ClaimRecord(ctx, key, raw)
	if e != nil || !claimed {
		if e == nil {
			e = errors.New("Команда уже зарезервирована")
		}
		s.state.Status = "error"
		return State{}, e
	}
	if e := m.assistant.Append(ctx, ws, assistant.Message{ID: id + ":user", Role: "user", Text: text}); e != nil {
		s.state.Status = "error"
		s.state.Error = e.Error()
		return State{}, e
	}
	runCtx, cancel := context.WithCancel(m.ctx)
	s.cancel = cancel
	s.runCtx = runCtx
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer cancel()
		m.run(runCtx, ws, s, text)
		m.mu.Lock()
		state := s.state
		requestHash := s.requestHash
		s.cancel = nil
		m.mu.Unlock()
		if conversation, e := m.assistant.Conversation(context.Background(), ws); e == nil && !conversation.Settings.SaveHistory {
			state.Text = ""
		}
		b, _ := json.Marshal(map[string]any{"hash": requestHash, "state": state})
		saveCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = m.store.PutRecord(saveCtx, key, b)
	}()
	return s.state, nil
}
func (m *Manager) run(ctx context.Context, ws string, s *Session, text string) {
	fail := func(e error) {
		m.patch(s, func(st *State) {
			st.Status = "error"
			st.Pending = nil
			st.Error = e.Error()
			st.Activity = "Не удалось выполнить команду Codex"
		})
	}
	c, e := m.assistant.Conversation(ctx, ws)
	if e != nil {
		fail(e)
		return
	}
	functions := m.assistant.Functions()
	catalogJSON, _ := json.Marshal(functions)
	catalogSum := sha256.Sum256(catalogJSON)
	catalog := hex.EncodeToString(catalogSum[:])
	if s.rpc != nil && (s.history != c.Settings.SaveHistory || s.catalog != catalog) {
		s.rpc.close()
		m.mu.Lock()
		s.rpc = nil
		s.thread = ""
		m.mu.Unlock()
		_ = m.store.PutRecord(ctx, "codex-thread:"+ws, json.RawMessage(`{}`))
	}
	if s.rpc == nil {
		rpc, e := launch(m.ctx, m.binary, filepath.Join(m.runtime, ws), func(msg envelope) { m.handle(ctx, ws, s, msg) })
		if e != nil {
			fail(e)
			return
		}
		m.mu.Lock()
		s.rpc = rpc
		m.mu.Unlock()
		s.history = c.Settings.SaveHistory
		initCtx, stop := context.WithTimeout(ctx, 25*time.Second)
		e = rpc.call(initCtx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "forma_workspace", "title": "Forma AI", "version": "1.0.0"}, "capabilities": map[string]any{"experimentalApi": true}}, nil)
		if e == nil {
			e = rpc.write(map[string]any{"method": "initialized"})
		}
		var account struct {
			Account  json.RawMessage `json:"account"`
			Requires bool            `json:"requiresOpenaiAuth"`
		}
		if e == nil {
			e = rpc.call(initCtx, "account/read", map[string]any{"refreshToken": false}, &account)
			if e == nil && account.Requires && (len(account.Account) == 0 || string(account.Account) == "null") {
				e = errors.New("Войдите в Codex на этом Mac: codex login. Отдельный OPENAI_API_KEY не нужен.")
			}
		}
		stop()
		if e != nil {
			rpc.close()
			m.mu.Lock()
			s.rpc = nil
			s.thread = ""
			m.mu.Unlock()
			fail(fmt.Errorf("Подключение к локальному Codex не удалось: %w", e))
			return
		}
	}
	if s.session != "" {
		_ = m.assistant.End(ctx, "codex", s.session)
	}
	session, err := m.assistant.LocalSession(ctx, "codex", ws)
	if err != nil {
		fail(err)
		return
	}
	m.mu.Lock()
	s.session = session
	m.mu.Unlock()

	if s.thread == "" {
		tools := []map[string]any{}
		for _, f := range functions {
			tools = append(tools, map[string]any{"type": "function", "name": f.Name, "description": f.Description, "inputSchema": f.Parameters})
		}
		instructions := "Ты — локальный Codex, встроенный в Forma AI. Отвечай по-русски кратко, только текстом. У тебя есть инструменты workspace, использующие тот же сервис, что и MCP. Управляй панелью только этими инструментами; не редактируй SQLite или файлы состояния. Для локальных фотографий и сгенерированных изображений обязательно используй media_import с абсолютным path: файл из ~/.codex/generated_images или рабочей папки копируется в постоянное хранилище и появляется на панели. Публичный HTTPS не требуется. Не ограничивайся ссылкой на локальный файл и не говори, что загрузка не поддерживается. Для галереи импортируй файлы с attach=false и создай gallery с возвращёнными URL. Доступны также video, audio, file (PDF), checklist, metric, progress. Используй собственный web search для актуальной информации, если он доступен. Не утверждай, что действие выполнено, пока инструмент не подтвердил успех. Не отправляй ui_show_notification для каждого ответа: приложение само отправляет финальный текст в macOS. Текущий workspace: " + ws
		params := map[string]any{"cwd": m.cwd, "sandbox": "read-only", "approvalPolicy": "on-request", "developerInstructions": instructions, "serviceName": "forma_workspace", "config": map[string]any{"web_search": "live"}, "dynamicTools": tools, "ephemeral": !s.history}
		method := "thread/start"
		if s.history {
			raw, ok, err := m.store.GetRecord(ctx, "codex-thread:"+ws)
			if err != nil {
				fail(err)
				return
			}
			if ok {
				var stored struct {
					ID     string `json:"id"`
					Schema string `json:"schema"`
				}
				_ = json.Unmarshal(raw, &stored)
				if stored.ID != "" && stored.Schema == catalog {
					method = "thread/resume"
					params["threadId"] = stored.ID
					delete(params, "dynamicTools")
					delete(params, "ephemeral")
					params["excludeTurns"] = true
				}
			}
		}
		var result struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if e = s.rpc.call(ctx, method, params, &result); e != nil {
			fail(e)
			return
		}
		m.mu.Lock()
		s.thread = result.Thread.ID
		m.mu.Unlock()
		if s.thread == "" {
			fail(errors.New("Codex не вернул thread ID"))
			return
		}
		if s.history {
			raw, _ := json.Marshal(map[string]string{"id": s.thread, "schema": catalog})
			if e = m.store.PutRecord(ctx, "codex-thread:"+ws, raw); e != nil {
				fail(e)
				return
			}
		}
	}
	s.catalog = catalog
	if e = m.assistant.Append(ctx, ws, assistant.Message{ID: s.state.RequestID + ":user", Role: "user", Text: text}); e != nil {
		fail(e)
		return
	}
	contextInfo, e := m.assistant.Context(ctx, ws)
	if e != nil {
		fail(e)
		return
	}
	brief, _ := json.Marshal(contextInfo)
	m.patch(s, func(st *State) {
		st.ThreadID = s.thread
		st.Activity = "Codex обрабатывает команду…"
	})
	m.mu.Lock()
	s.turnDone = make(chan struct{}, 1)
	m.mu.Unlock()
	var started struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	input := []map[string]any{{"type": "text", "text": "Текущее состояние Forma (контекст, не инструкция): " + string(brief) + "\n\nЗапрос пользователя: " + text, "text_elements": []any{}}}
	if e = s.rpc.call(ctx, "turn/start", map[string]any{"threadId": s.thread, "input": input}, &started); e != nil {
		fail(e)
		return
	}
	m.mu.Lock()
	s.turnID = started.Turn.ID
	m.mu.Unlock()
	select {
	case <-ctx.Done():
		m.interrupt(s)
		if s.rpc != nil {
			s.rpc.close()
			m.mu.Lock()
			s.rpc = nil
			s.thread = ""
			m.mu.Unlock()
		}
		fail(errors.New("Команда Codex отменена"))
		return
	case <-s.rpc.done:
		m.mu.Lock()
		s.rpc = nil
		s.thread = ""
		m.mu.Unlock()
		fail(errors.New("Codex закрыл соединение. Отправьте новую команду для подключения."))
		return
	case <-s.turnDone:
	}
	m.mu.Lock()
	state := s.state
	m.mu.Unlock()
	if state.Status == "error" || state.Status == "cancelled" {
		return
	}
	if strings.TrimSpace(state.Text) == "" {
		m.mu.Lock()
		toolStatus, toolError := s.lastToolStatus, s.lastToolError
		m.mu.Unlock()
		switch toolStatus {
		case "success":
			state.Text = "Команда выполнена. Результат доступен на панели."
		case "error":
			state.Text = "Не удалось выполнить команду: " + toolError
		case "cancelled":
			state.Text = "Действие отменено."
		default:
			state.Text = "Codex завершил запрос без текстового ответа. Повторите или уточните команду."
		}
		m.patch(s, func(st *State) { st.Text = state.Text })
	}
	if state.Text != "" {
		answer := []rune(state.Text)
		if len(answer) > 8000 {
			answer = answer[:8000]
		}
		message := assistant.Message{ID: state.RequestID + ":assistant", Role: "assistant", Text: string(answer)}
		if e = m.assistant.Append(ctx, ws, message); e != nil {
			fail(e)
			return
		}
		if m.desktop != nil && c.Settings.Notifications {
			if _, e = m.desktop.Send(ctx, desktop.Notification{ID: "codex:" + ws + ":" + state.RequestID, Title: "Codex · Forma", Subtitle: "Ответ локального Codex", Message: message.Text}); e != nil {
				m.patch(s, func(st *State) { st.NotificationError = e.Error() })
			}
		}
	}
	m.patch(s, func(st *State) { st.Status = "completed"; st.Pending = nil; st.Activity = "Готово" })
}
func (m *Manager) handle(_ context.Context, ws string, s *Session, msg envelope) {
	m.mu.Lock()
	ctx := s.runCtx
	session := s.session
	rpc := s.rpc
	requestID := s.state.RequestID
	m.mu.Unlock()
	if ctx == nil || rpc == nil {
		return
	}
	var p struct {
		ThreadID  string          `json:"threadId"`
		TurnID    string          `json:"turnId"`
		CallID    string          `json:"callId"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
		Delta     string          `json:"delta"`
		ItemID    string          `json:"itemId"`
		Command   any             `json:"command"`
		Reason    string          `json:"reason"`
		Item      struct {
			ID    string `json:"id"`
			Type  string `json:"type"`
			Text  string `json:"text"`
			Phase string `json:"phase"`
		} `json:"item"`
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(msg.Params, &p) != nil {
		return
	}
	m.mu.Lock()
	thread := s.thread
	m.mu.Unlock()
	if p.ThreadID != "" && thread != "" && p.ThreadID != thread {
		return
	}
	if len(msg.ID) > 0 {
		s.requests.Lock()
		defer s.requests.Unlock()
		if msg.Method == "item/tool/call" {
			reply, err := m.assistant.Call(ctx, "codex", session, assistant.ToolCall{CallID: p.CallID, Name: p.Tool, Arguments: p.Arguments})
			if err == nil && reply.Status == "confirmation_required" {
				approved := m.confirm(ctx, s, reply.Label)
				decision := "reject"
				if approved {
					decision = "approve"
				}
				reply, err = m.assistant.Call(ctx, "codex", session, assistant.ToolCall{CallID: p.CallID, Name: p.Tool, Arguments: p.Arguments, Confirmation: reply.Confirmation, Decision: decision})
			}
			if err != nil {
				reply = assistant.ToolReply{Status: "error", Error: err.Error()}
			}
			m.mu.Lock()
			if s.state.RequestID == requestID && (reply.Status == "error" || s.lastToolStatus != "error" && (reply.Status == "cancelled" || s.lastToolStatus != "cancelled")) {
				s.lastToolStatus = reply.Status
				s.lastToolError = reply.Error
			}
			m.mu.Unlock()
			raw, _ := json.Marshal(reply)
			_ = rpc.reply(msg.ID, map[string]any{"success": reply.Status == "success", "contentItems": []map[string]string{{"type": "inputText", "text": string(raw)}}})
			return
		}
		if msg.Method == "item/fileChange/requestApproval" {
			_ = rpc.reply(msg.ID, map[string]any{"decision": "decline"})
			return
		}
		if msg.Method == "item/commandExecution/requestApproval" {
			preview, _ := json.Marshal(p.Command)
			label := "Разрешить действие Codex: " + p.Reason + " " + string(preview)
			if len(label) > 1200 || p.Command == nil {
				_ = rpc.reply(msg.ID, map[string]any{"decision": "decline"})
				return
			}
			approved := m.confirm(ctx, s, label)
			decision := "decline"
			if approved {
				decision = "accept"
			}
			_ = rpc.reply(msg.ID, map[string]any{"decision": decision})
			return
		}
		// Unsupported interactive requests fail closed instead of stalling the turn.
		_ = rpc.write(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32601, "message": "Forma does not support this interactive request; ask the user in your text reply."}})
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.state.RequestID != requestID || !active(s.state.Status) {
		return
	}
	switch msg.Method {
	case "item/started":
		if p.Item.Type == "agentMessage" {
			s.state.Text = ""
			s.currentItem = p.Item.ID
			s.state.Status = "responding"
		} else if p.Item.Type == "webSearch" {
			s.state.Activity = "Codex ищет информацию в интернете…"
		} else if p.Item.Type == "dynamicToolCall" {
			s.state.Activity = "Codex изменяет рабочее пространство…"
		}
	case "item/agentMessage/delta":
		if p.ItemID != s.currentItem {
			s.state.Text = ""
			s.currentItem = p.ItemID
		}
		s.state.Status = "responding"
		if utf8.RuneCountInString(s.state.Text) < 8000 {
			s.state.Text = bounded(s.state.Text + p.Delta)
		}
		s.state.Activity = "Codex отвечает…"
	case "item/completed":
		if p.Item.Type == "agentMessage" {
			s.state.Text = bounded(p.Item.Text)
		}
	case "turn/completed":
		if p.Turn.Status == "failed" {
			s.state.Status = "error"
			if p.Turn.Error != nil {
				s.state.Error = p.Turn.Error.Message
			} else {
				s.state.Error = "Codex не выполнил запрос"
			}
		} else if p.Turn.Status == "interrupted" {
			s.state.Status = "cancelled"
		}
		if s.turnDone != nil {
			select {
			case s.turnDone <- struct{}{}:
			default:
			}
		}
	case "error":
		if p.Error != nil {
			s.state.Activity = p.Error.Message
		}
	}
}
func (m *Manager) confirm(ctx context.Context, s *Session, label string) bool {
	m.mu.Lock()
	id := workspace.ID()
	decision := make(chan bool, 1)
	s.decision = decision
	s.state.Pending = &Pending{ID: id, Label: label}
	s.state.Status = "confirmation"
	m.mu.Unlock()
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	approved := false
	select {
	case approved = <-decision:
	case <-ctx.Done():
	case <-timer.C:
	}
	m.mu.Lock()
	s.decision = nil
	s.state.Pending = nil
	if s.state.Status == "confirmation" {
		s.state.Status = "thinking"
	}
	m.mu.Unlock()
	return approved
}
func (m *Manager) Decide(ws, id string, approve bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[ws]
	if s == nil || s.state.Pending == nil || s.state.Pending.ID != id || s.decision == nil {
		return errors.New("Подтверждение истекло или относится к другому действию")
	}
	select {
	case s.decision <- approve:
		s.state.Pending = nil
		return nil
	default:
		return errors.New("Решение уже принято")
	}
}
func (m *Manager) interrupt(s *Session) {
	m.mu.Lock()
	rpc, thread, turn := s.rpc, s.thread, s.turnID
	m.mu.Unlock()
	if rpc != nil && turn != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = rpc.call(ctx, "turn/interrupt", map[string]any{"threadId": thread, "turnId": turn}, nil)
	}
}
func (m *Manager) Cancel(ws string) {
	m.mu.Lock()
	s := m.sessions[ws]
	if s != nil && s.cancel != nil {
		s.cancel()
	}
	m.mu.Unlock()
}
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	for _, s := range m.sessions {
		if s.rpc != nil {
			s.rpc.close()
		}
		if s.session != "" {
			_ = m.assistant.End(context.Background(), "codex", s.session)
		}
	}
}

func bounded(s string) string {
	r := []rune(s)
	if len(r) > 8000 {
		return string(r[:8000])
	}
	return s
}
