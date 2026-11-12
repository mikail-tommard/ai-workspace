import { useEffect, useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import {
  Bell,
  Check,
  ChevronDown,
  ChevronUp,
  History,
  LoaderCircle,
  Mic,
  MicOff,
  Send,
  Settings2,
  Sparkles,
  Square,
  X,
} from "lucide-react";
import { useWorkspace } from "../store";
import DesktopNotice from "./DesktopNotice";
import { assistant, useAssistant } from "./useAssistant";
import type { AssistantSettings } from "./types";
const labels = {
  disconnected: "Локальный Codex",
  connecting: "Подключаюсь…",
  listening: "Готов к команде",
  thinking: "Обрабатываю запрос…",
  responding: "Готовлю ответ…",
  error: "Требуется внимание",
};
export default function AssistantPanel() {
  const { workspace } = useWorkspace(),
    state = useAssistant();
  const [expanded, setExpanded] = useState(false),
    [text, setText] = useState(""),
    [settingsOpen, setSettingsOpen] = useState(false),
    [sending, setSending] = useState(false),
    [saving, setSaving] = useState(false),
    [draft, setDraft] = useState<AssistantSettings>(state.settings);
  const historyRef = useRef<HTMLDivElement>(null),
    follow = useRef(true);
  const workspaceId = workspace?.id;
  useEffect(() => {
    if (workspaceId) void assistant.load(workspaceId);
  }, [workspaceId]);
  useEffect(() => () => assistant.dispose(), []);
  useEffect(() => {
    if (settingsOpen) setDraft(state.settings);
  }, [settingsOpen, state.settings]);
  useEffect(() => {
    if (expanded) {
      follow.current = true;
      const el = historyRef.current;
      if (el) el.scrollTop = el.scrollHeight;
    }
  }, [expanded]);
  useEffect(() => {
    const el = historyRef.current;
    if (el && follow.current) el.scrollTop = el.scrollHeight;
  }, [state.messages]);
  const connecting = state.status === "connecting";
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!text.trim() || sending) return;
    const input = text;
    setText("");
    setSending(true);
    setExpanded(true);
    try {
      await assistant.textCommand(input);
    } finally {
      setSending(false);
    }
  }
  return (
    <>
      <section
        className={`assistant-dock ${expanded ? "assistant-dock-expanded" : ""}`}
        aria-label="AI-ассистент"
      >
        <header className="assistant-dock-header">
          <button
            className="assistant-expand"
            aria-expanded={expanded}
            onClick={() => setExpanded(!expanded)}
          >
            <span className="assistant-symbol">
              <Sparkles size={18} />
            </span>
            <span>
              <strong>
                Codex{" "}
                <span className="assistant-native-badge">
                  <Bell size={10} /> macOS
                </span>
              </strong>
              <small
                className={`assistant-state assistant-state-${state.status}`}
                role="status"
              >
                <span />
                {state.microphone && state.status === "listening"
                  ? "Распознаю речь"
                  : labels[state.status]}
              </small>
            </span>
            {expanded ? <ChevronDown size={16} /> : <ChevronUp size={16} />}
          </button>
          <button
            className="icon-button"
            aria-label="История диалога"
            onClick={() => setExpanded(true)}
          >
            <History size={16} />
          </button>
          <button
            className="icon-button"
            aria-label="Настройки ассистента"
            onClick={() => setSettingsOpen(true)}
          >
            <Settings2 size={17} />
          </button>
        </header>
        {expanded ? (
          <div className="assistant-expanded">
            <div className="assistant-delivery">
              <Bell size={13} />
              <span>Ответы в уведомлениях macOS</span>
              <span className="tiny-dot" />
            </div>
            {state.configured !== true ? (
              <div className="assistant-configuration" role="note">
                <p>
                  {state.configurationMessage ||
                    "Проверяю настройки ассистента…"}
                  <span>Инструменты панели и MCP доступны.</span>
                </p>
              </div>
            ) : null}
            <div
              className="assistant-history"
              ref={historyRef}
              onScroll={(e) => {
                const el = e.currentTarget;
                follow.current =
                  el.scrollHeight - el.scrollTop - el.clientHeight < 40;
              }}
              aria-label="История сообщений"
              aria-live="polite"
            >
              {state.messages.length ? (
                state.messages.map((message) => (
                  <div
                    key={message.id}
                    className={`assistant-message assistant-message-${message.role}`}
                  >
                    <span>{message.role === "user" ? "Вы" : "Forma AI"}</span>
                    <p>{message.text}</p>
                    <time dateTime={message.at}>
                      {new Date(message.at).toLocaleTimeString("ru", {
                        hour: "2-digit",
                        minute: "2-digit",
                      })}
                    </time>
                  </div>
                ))
              ) : (
                <div className="assistant-history-empty">
                  <Sparkles size={25} />
                  <strong>От идеи — к результату</strong>
                  <p>
                    «Создай задачу по математике»
                    <br />
                    или «Покажи график синуса».
                  </p>
                </div>
              )}
            </div>
            {state.interim ? (
              <p className="assistant-transcript" role="status">
                <Mic size={13} />
                {state.interim}
              </p>
            ) : null}
            {state.voiceDraft ? (
              <button
                className="quiet assistant-send-phrase"
                onClick={() => assistant.submitVoice()}
              >
                <Send size={14} />
                Отправить фразу сейчас
              </button>
            ) : null}
            {state.activity &&
            ["thinking", "responding"].includes(state.status) ? (
              <p className="assistant-activity" role="status">
                <LoaderCircle size={13} className="spin" />
                {state.activity}
              </p>
            ) : null}
            {state.pending ? (
              <div
                className="assistant-confirm"
                role="group"
                aria-label="Подтверждение действия"
              >
                <strong>{state.pending.label}</strong>
                <p>Микрофон выключен. Подтвердите действие кнопкой.</p>
                <div>
                  <button
                    className="quiet"
                    onClick={() => assistant.decide(false)}
                  >
                    Отмена
                  </button>
                  <button
                    className="primary destructive"
                    onClick={() => assistant.decide(true)}
                  >
                    Подтвердить
                  </button>
                </div>
              </div>
            ) : null}
            {state.error ? (
              <p className="assistant-inline-error" role="alert">
                {state.error}
              </p>
            ) : null}
            {state.notificationError ? (
              <p className="assistant-inline-error" role="alert">
                {state.notificationError}
              </p>
            ) : null}
            <div className="assistant-session-controls">
              <span>
                {state.microphone ? <Mic size={12} /> : <MicOff size={12} />}
                Микрофон {state.microphone ? "включён" : "выключен"}
              </span>
              {state.connected || connecting ? (
                <button
                  className="text-button"
                  onClick={() => void assistant.disconnect()}
                >
                  <Square size={12} />
                  Завершить сессию
                </button>
              ) : null}
            </div>
          </div>
        ) : null}
        <form className="assistant-input-row" onSubmit={submit}>
          <button
            type="button"
            className={`assistant-microphone ${state.microphone ? "is-recording" : ""}`}
            aria-label={
              state.microphone ? "Выключить микрофон" : "Включить микрофон"
            }
            aria-pressed={state.microphone}
            disabled={connecting || !!state.pending || !workspaceId}
            onClick={() => {
              setExpanded(true);
              void assistant.setMicrophone(!state.microphone).catch(() => {});
            }}
          >
            {connecting ? (
              <LoaderCircle size={19} className="spin" />
            ) : state.microphone ? (
              <MicOff size={19} />
            ) : (
              <Mic size={19} />
            )}
          </button>
          <label className="sr-only" htmlFor="assistant-text">
            Команда ассистенту
          </label>
          <input
            id="assistant-text"
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="Что создадим сегодня?"
            autoComplete="off"
            maxLength={8000}
            disabled={!!state.pending}
            onFocus={() => setExpanded(true)}
          />
          {["responding", "thinking"].includes(state.status) &&
          !state.pending ? (
            <button
              type="button"
              className="icon-button"
              aria-label="Прервать ответ ассистента"
              onClick={() => assistant.interrupt()}
            >
              <Square size={16} />
            </button>
          ) : null}
          <button
            type="submit"
            className="assistant-send"
            aria-label="Отправить команду ассистенту"
            disabled={
              !text.trim() ||
              sending ||
              connecting ||
              !!state.pending ||
              !workspaceId
            }
          >
            <Send size={16} />
          </button>
        </form>
        <div className="assistant-privacy">
          <span className="tiny-dot" />
          Ваш Codex на Mac · без озвучки
        </div>
      </section>
      <Dialog.Root open={settingsOpen} onOpenChange={setSettingsOpen}>
        <Dialog.Portal>
          <Dialog.Overlay className="dialog-overlay" />
          <Dialog.Content className="dialog-content assistant-settings">
            <div className="dialog-top">
              <span className="dialog-icon">
                <Sparkles size={20} />
              </span>
              <Dialog.Close
                className="icon-button"
                aria-label="Закрыть настройки ассистента"
              >
                <X size={20} />
              </Dialog.Close>
            </div>
            <Dialog.Title>Настройки ассистента</Dialog.Title>
            <Dialog.Description>
              Команды передаются вашему Codex на этом Mac. Отдельный OpenAI API
              key не нужен.
            </Dialog.Description>
            <DesktopNotice />
            <form
              onSubmit={(e) => {
                e.preventDefault();
                setSaving(true);
                void assistant
                  .settings(draft)
                  .then((saved) => {
                    if (saved) setSettingsOpen(false);
                  })
                  .finally(() => setSaving(false));
              }}
            >
              <label className="assistant-setting">
                Язык
                <select
                  aria-label="Язык"
                  value={draft.language}
                  onChange={(e) =>
                    setDraft({
                      ...draft,
                      language: e.target.value as "ru" | "en",
                    })
                  }
                >
                  <option value="ru">Русский</option>
                  <option value="en">English</option>
                </select>
              </label>
              <label className="assistant-setting">
                Пауза перед отправкой
                <select
                  aria-label="Пауза перед отправкой"
                  value={draft.speech_pause_ms}
                  onChange={(e) =>
                    setDraft({
                      ...draft,
                      speech_pause_ms: Number(e.target.value),
                    })
                  }
                >
                  {[2000, 3000, 4000, 5000, 6000, 8000, 10000].map((value) => (
                    <option key={value} value={value}>
                      {value / 1000} сек.
                    </option>
                  ))}
                </select>
                <small className="muted">
                  Дождётся этой паузы после речи. Несколько коротких фраз
                  объединяются; можно отправить сразу кнопкой.
                </small>
              </label>
              <label className="assistant-checkbox">
                <input
                  type="checkbox"
                  checked={draft.notifications}
                  onChange={(e) =>
                    setDraft({ ...draft, notifications: e.target.checked })
                  }
                />
                <span>Отправлять ответы в уведомления macOS</span>
              </label>
              <label className="assistant-checkbox">
                <input
                  type="checkbox"
                  checked={draft.save_history}
                  onChange={(e) =>
                    setDraft({ ...draft, save_history: e.target.checked })
                  }
                />
                <span>Сохранять историю диалога</span>
              </label>
              <p className="muted small">
                Аудиозаписи не сохраняются. Стиль и длительность системных
                баннеров настраиваются в macOS → Уведомления → Script Editor.
              </p>
              <button className="primary full-width" disabled={saving}>
                <Check size={16} />
                Сохранить настройки
              </button>
            </form>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </>
  );
}
