package assistant

import (
	"aiworkspace/internal/workspace"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

var callPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,128}$`)

type receipt struct {
	Hash      string          `json:"hash"`
	Phase     string          `json:"phase"`
	Reply     ToolReply       `json:"reply"`
	Version   int64           `json:"version"`
	Expires   time.Time       `json:"expires"`
	Arguments json.RawMessage `json:"arguments"`
}

func (m *Manager) saveReceipt(ctx context.Context, key string, r receipt) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	return m.store.PutRecord(ctx, key, b)
}
func (m *Manager) Call(ctx context.Context, owner, sessionID string, call ToolCall) (ToolReply, error) {
	m.toolMu.Lock()
	defer m.toolMu.Unlock()
	sess, e := m.session(owner, sessionID)
	if e != nil {
		return ToolReply{}, e
	}
	if e = ctx.Err(); e != nil {
		return ToolReply{}, e
	}
	if !callPattern.MatchString(call.CallID) || len(call.Arguments) > 1<<20 {
		return ToolReply{}, fmt.Errorf("Некорректный идентификатор или размер вызова.")
	}
	var args map[string]any
	if len(call.Arguments) == 0 {
		call.Arguments = json.RawMessage("{}")
	}
	if e = json.Unmarshal(call.Arguments, &args); e != nil || args == nil {
		return ToolReply{Status: "error", Error: "Аргументы должны быть JSON-объектом."}, nil
	}
	canonical, _ := json.Marshal(args)
	hash := fingerprint(call.Name, canonical)
	key := "call:" + sessionID + ":" + call.CallID
	existing, found, e := m.store.GetRecord(ctx, key)
	if e != nil {
		return ToolReply{}, e
	}
	var record receipt
	if found {
		if e = json.Unmarshal(existing, &record); e != nil {
			return ToolReply{}, e
		}
		if record.Hash != hash {
			return ToolReply{Status: "error", Error: "call_id уже связан с другой командой."}, nil
		}
		if record.Phase == "done" {
			return record.Reply, nil
		}
		if record.Phase == "running" {
			return ToolReply{Status: "error", Error: "Результат предыдущего выполнения не подтверждён. Прочитайте состояние; автоматический повтор запрещён."}, nil
		}
		if call.Decision == "" {
			return record.Reply, nil
		}
		if call.Confirmation != record.Reply.Confirmation || time.Now().After(record.Expires) {
			return ToolReply{Status: "error", Error: "Подтверждение недействительно или истекло."}, nil
		}
		if call.Decision == "reject" {
			record.Phase = "done"
			record.Reply = ToolReply{Status: "cancelled", Error: "Пользователь отменил операцию."}
			if e = m.saveReceipt(ctx, key, record); e != nil {
				return ToolReply{}, e
			}
			return record.Reply, nil
		}
		if call.Decision != "approve" {
			return ToolReply{Status: "error", Error: "Неизвестное решение подтверждения."}, nil
		}
	} else {
		var tool *workspace.Tool
		for _, t := range m.service.Tools() {
			if t.Name == call.Name {
				copy := t
				tool = &copy
				break
			}
		}
		if tool == nil || call.Name == "workspace_create" || call.Name == "workspace_list" {
			return ToolReply{Status: "error", Error: "Инструмент недоступен в голосовой сессии."}, nil
		}
		if v, ok := args["workspace_id"]; ok && v != sess.WorkspaceID {
			return ToolReply{Status: "error", Error: "Нельзя изменять другое рабочее пространство."}, nil
		}
		args["workspace_id"] = sess.WorkspaceID
		needsWidget := false
		for _, required := range tool.InputSchema.Required {
			if required == "widget_id" {
				needsWidget = true
			}
		}
		if needsWidget {
			if _, ok := args["widget_id"]; !ok {
				c, e := m.Context(ctx, sess.WorkspaceID)
				if e != nil {
					return ToolReply{}, e
				}
				if id, _ := c["last_widget_id"].(string); id != "" {
					args["widget_id"] = id
				}
			}
		}
		encoded, _ := json.Marshal(args)
		if _, e = workspace.Decode(encoded, tool.InputSchema); e != nil {
			return ToolReply{Status: "error", Error: e.Error()}, nil
		}
		m.mu.Lock()
		if sess.calls >= 128 {
			m.mu.Unlock()
			return ToolReply{Status: "error", Error: "Достигнут лимит команд сессии. Подключитесь заново."}, nil
		}
		sess.calls++
		m.mu.Unlock()
		record = receipt{Hash: hash, Phase: "running", Arguments: encoded}
		if tool.Annotations["destructiveHint"] {
			st, e := m.service.Snapshot(sess.WorkspaceID)
			if e != nil {
				return ToolReply{}, e
			}
			record.Phase = "pending"
			record.Version = st.Version
			record.Expires = time.Now().Add(2 * time.Minute)
			record.Reply = ToolReply{Status: "confirmation_required", Confirmation: workspace.ID(), Label: confirmationLabel(call.Name)}
		}
		raw, _ := json.Marshal(record)
		claimed, e := m.store.ClaimRecord(ctx, key, raw)
		if e != nil {
			return ToolReply{}, e
		}
		if !claimed {
			return ToolReply{Status: "error", Error: "Вызов уже зарезервирован. Повторите чтение результата."}, nil
		}
		if record.Phase == "pending" {
			return record.Reply, nil
		}
	}
	requiresVersion := record.Phase == "pending"
	record.Phase = "running"
	if e = m.saveReceipt(ctx, key, record); e != nil {
		return ToolReply{}, e
	}
	commandCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	stop := context.AfterFunc(sess.ctx, cancel)
	defer cancel()
	defer stop()
	var result workspace.Result
	if requiresVersion {
		result, e = m.service.ExecuteAtVersion(commandCtx, call.Name, record.Arguments, record.Version)
	} else {
		result, e = m.service.Execute(commandCtx, call.Name, record.Arguments)
	}
	reply := ToolReply{Status: "success"}
	if e != nil {
		reply.Status = "error"
		reply.Error = e.Error()
	} else {
		if w, ok := result["widget"].(workspace.Widget); ok {
			m.mu.Lock()
			c, err := m.load(commandCtx, sess.WorkspaceID)
			if err == nil {
				next := *c
				next.LastWidget = w.ID
				if err = m.persist(commandCtx, sess.WorkspaceID, &next); err == nil {
					c.LastWidget = w.ID
				}
			}
			m.mu.Unlock()
			if err != nil {
				result["context_warning"] = "Команда выполнена; сохранение контекста недоступно."
			}
		}
		reply.Result = m.compact(commandCtx, sess.WorkspaceID, result)
	}
	record.Phase = "done"
	record.Reply = reply
	// Once execution has started, persist the receipt even if the browser disconnected.
	saveCtx, saveCancel := context.WithTimeout(m.ctx, 5*time.Second)
	defer saveCancel()
	if err := m.saveReceipt(saveCtx, key, record); err != nil {
		return ToolReply{}, fmt.Errorf("Команда могла выполниться; сохранение результата недоступно. Перечитайте панель перед новым действием.")
	}
	return reply, nil
}
func confirmationLabel(tool string) string {
	switch tool {
	case "workspace_clear":
		return "Очистить все виджеты рабочего пространства?"
	case "workspace_delete":
		return "Удалить рабочее пространство?"
	default:
		return "Удалить выбранную карточку?"
	}
}
func (m *Manager) compact(ctx context.Context, id string, result workspace.Result) workspace.Result {
	out := workspace.Result{}
	for _, key := range []string{"workspace_id", "version", "correct", "steps", "tab", "context_warning"} {
		if v, ok := result[key]; ok {
			out[key] = v
		}
	}
	if w, ok := result["widget"].(workspace.Widget); ok {
		b, _ := json.Marshal(w)
		var v map[string]any
		_ = json.Unmarshal(b, &v)
		if cfg, ok := v["config"].(map[string]any); ok {
			for _, k := range []string{"code", "content", "message"} {
				if text, ok := cfg[k].(string); ok {
					cfg[k] = excerpt(text, 4000)
				}
			}
			for _, k := range []string{"rows", "points"} {
				if a, ok := cfg[k].([]any); ok && len(a) > 30 {
					cfg[k] = a[:30]
					cfg[k+"_truncated"] = true
				}
			}
		}
		out["widget"] = v
	}
	if _, ok := result["state"]; ok {
		if context, e := m.Context(ctx, id); e == nil {
			out["state"] = context
		}
	}
	if widgets, ok := result["widgets"].([]workspace.Widget); ok {
		summary := []map[string]any{}
		for _, w := range widgets {
			summary = append(summary, map[string]any{"id": w.ID, "type": w.Type, "title": excerpt(w.Title, 120), "tab_id": w.TabID})
		}
		out["widgets"] = summary
	}
	return out
}
