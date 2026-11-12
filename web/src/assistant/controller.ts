import {
  BrowserSpeech,
  type SpeechInput,
  type SpeechHandlers,
} from "./speech.ts";
import {
  defaultSettings,
  normalizeSettings,
  type AssistantSettings,
  type AssistantState,
  type ChatMessage,
  type DesktopStatus,
  type PendingConfirmation,
} from "./types.ts";
type Fetcher = (
  input: RequestInfo | URL,
  init?: RequestInit,
) => Promise<Response>;
type CodexState = {
  request_id: string;
  status:
    | "idle"
    | "thinking"
    | "responding"
    | "confirmation"
    | "completed"
    | "cancelled"
    | "error";
  activity: string;
  text: string;
  error?: string;
  pending: PendingConfirmation | null;
  notification_error?: string;
  thread_id?: string;
};
type Options = {
  fetch?: Fetcher;
  speech?: (handlers: SpeechHandlers) => SpeechInput;
  uuid?: () => string;
};
export class AssistantController {
  private state: AssistantState = {
    workspaceId: "default",
    status: "disconnected",
    connected: false,
    microphone: false,
    configured: null,
    configurationMessage: "",
    settings: { ...defaultSettings },
    messages: [],
    activity: "",
    desktop: null,
    notificationError: null,
    pending: null,
    error: null,
    interim: "",
    voiceDraft: "",
  };
  private listeners = new Set<() => void>();
  private fetcher: Fetcher;
  private speechFactory: (handlers: SpeechHandlers) => SpeechInput;
  private speech: SpeechInput | null = null;
  private uuid: () => string;
  private generation = 0;
  private loadGeneration = 0;
  private requestId = "";
  private polling: ReturnType<typeof setTimeout> | undefined;
  private silence: ReturnType<typeof setTimeout> | undefined;
  private speechWanted = false;
  private queuedVoice: string[] = [];
  private closed = false;
  private busy = false;
  private abort: AbortController | null = null;
  constructor(options: Options = {}) {
    this.fetcher = options.fetch || ((input, init) => fetch(input, init));
    this.speechFactory = options.speech || ((h) => new BrowserSpeech(h));
    this.uuid = options.uuid || (() => crypto.randomUUID());
  }
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  snapshot = () => this.state;
  private set(patch: Partial<AssistantState>) {
    this.state = { ...this.state, ...patch };
    this.listeners.forEach((f) => f());
  }
  private base() {
    return `/api/assistant/codex/workspaces/${encodeURIComponent(this.state.workspaceId)}`;
  }
  private async api<T>(
    path: string,
    method = "GET",
    body?: unknown,
    signal?: AbortSignal,
  ): Promise<T> {
    const response = await this.fetcher(path, {
      method,
      credentials: "same-origin",
      headers:
        method === "GET"
          ? {}
          : { "Content-Type": "application/json", "X-Workspace-Request": "1" },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal,
    });
    if (!response.ok) {
      let message = "Не удалось связаться с локальным Codex.";
      try {
        message = (await response.json()).error || message;
      } catch {
        /* No upstream HTML. */
      }
      throw Error(message);
    }
    return response.json() as Promise<T>;
  }
  private message(e: unknown) {
    return e instanceof Error ? e.message : "Не удалось выполнить команду";
  }
  private fail(e: unknown) {
    this.set({
      status: "error",
      error: this.message(e),
      activity: "",
      pending: null,
    });
  }
  async load(workspaceId: string) {
    const loading = ++this.loadGeneration;
    this.closed = false;
    if (workspaceId !== this.state.workspaceId) {
      await this.disconnect();
      if (loading !== this.loadGeneration) return;
      this.set({ workspaceId, messages: [], error: null, interim: "" });
    }
    try {
      const [status, c, desktop] = await Promise.all([
        this.api<{ configured: boolean; message: string; provider?: string }>(
          "/api/assistant/status",
        ),
        this.api<{ settings: AssistantSettings; messages: ChatMessage[] }>(
          `/api/assistant/workspaces/${encodeURIComponent(workspaceId)}`,
        ),
        this.api<DesktopStatus>("/api/desktop/status"),
      ]);
      if (this.closed || loading !== this.loadGeneration) return;
      const current = status.provider === "codex";
      this.set({
        configured: current && status.configured,
        configurationMessage: current
          ? status.message
          : "Запущена старая версия сервера. Перезапустите обновлённый ai-workspace для подключения к локальному Codex.",
        settings: normalizeSettings(c.settings),
        messages: c.messages || [],
        desktop,
      });
      if (current) {
        const state = await this.api<CodexState>(this.base() + "/state");
        if (this.closed || loading !== this.loadGeneration) return;
        if (["thinking", "responding", "confirmation"].includes(state.status)) {
          this.requestId = state.request_id;
          this.busy = true;
          this.apply(state);
          this.schedule();
        }
      }
    } catch (e) {
      if (!this.closed && loading === this.loadGeneration)
        this.set({ configured: false, configurationMessage: this.message(e) });
    }
  }
  private schedule() {
    clearTimeout(this.polling);
    const gen = this.generation;
    this.polling = setTimeout(() => {
      void this.poll(gen);
    }, 500);
  }
  private async poll(gen: number) {
    try {
      const state = await this.api<CodexState>(
        this.base() + "/state",
        "GET",
        undefined,
        this.abort?.signal,
      );
      if (this.closed || gen !== this.generation) return;
      this.apply(state);
      if (this.busy) this.schedule();
      else {
        await this.refreshMessages();
        if (state.status === "completed" && this.queuedVoice.length) {
          const next = this.queuedVoice.shift()!;
          void this.textCommand(next);
        } else if (state.status !== "completed") this.queuedVoice = [];
        if (this.state.settings.notifications)
          void this.updateDesktop().catch((e) => {
            if (!this.closed) this.set({ notificationError: this.message(e) });
          });
      }
    } catch (e) {
      if (!this.closed && gen === this.generation) {
        this.busy = false;
        this.fail(e);
      }
    }
  }
  private apply(s: CodexState) {
    if (this.requestId && s.request_id !== this.requestId) {
      this.busy = false;
      this.fail(
        Error(
          "Другая вкладка начала новую команду. Перечитайте историю диалога.",
        ),
      );
      return;
    }
    this.requestId = s.request_id;
    this.busy = ["thinking", "responding", "confirmation"].includes(s.status);
    const status = this.busy
      ? s.status === "responding"
        ? "responding"
        : "thinking"
      : s.status === "error"
        ? "error"
        : this.speechWanted
          ? "listening"
          : "disconnected";
    this.set({
      status,
      connected: this.busy || this.speechWanted,
      activity: s.text || s.activity,
      pending: s.pending,
      error: s.error || null,
      notificationError: s.notification_error || null,
    });
    if (s.pending && this.speechWanted) {
      this.stopSpeech();
      this.set({ interim: "Микрофон остановлен на время подтверждения." });
    }
  }
  private async refreshMessages() {
    const ws = this.state.workspaceId;
    const c = await this.api<{ messages: ChatMessage[] }>(
      `/api/assistant/workspaces/${encodeURIComponent(ws)}`,
    );
    if (!this.closed && ws === this.state.workspaceId)
      this.set({ messages: c.messages || [] });
  }
  async textCommand(text: string) {
    text = text.trim();
    if (!text || text.length > 8000) return;
    if (this.state.pending) {
      this.set({ error: "Сначала подтвердите или отмените действие." });
      return;
    }
    if (this.busy) {
      this.set({
        error: "Codex ещё отвечает. Дождитесь ответа или нажмите «Прервать».",
      });
      return;
    }
    if (!this.state.configured) {
      this.fail(
        Error(
          this.state.configurationMessage ||
            "Проверяю локальный Codex. Попробуйте через секунду.",
        ),
      );
      return;
    }
    const gen = this.generation;
    this.busy = true;
    this.requestId = this.uuid();
    this.abort = new AbortController();
    this.set({
      status: "thinking",
      connected: true,
      error: null,
      interim: "",
      activity: "Передаю команду локальному Codex…",
    });
    try {
      const s = await this.api<CodexState>(
        this.base() + "/turns",
        "POST",
        { request_id: this.requestId, text },
        this.abort.signal,
      );
      if (this.closed || gen !== this.generation) return;
      this.apply(s);
      await this.refreshMessages();
      if (this.busy) this.schedule();
    } catch (e) {
      if (this.closed || gen !== this.generation) return;
      this.busy = false;
      this.fail(e);
    }
  }
  private armSilence() {
    clearTimeout(this.silence);
    this.silence = setTimeout(() => {
      if (this.speechWanted)
        this.set({
          interim:
            "Речь пока не распознана. Проверьте микрофон и доступ к распознаванию в браузере.",
        });
    }, 12000);
  }
  async setMicrophone(enabled: boolean) {
    if (!enabled) {
      this.stopSpeech(true);
      if (!this.busy) this.set({ status: "disconnected", connected: false });
      return;
    }
    if (this.state.pending) {
      this.set({ error: "Сначала подтвердите действие." });
      return;
    }
    if (this.state.configured !== true) {
      this.fail(
        Error(this.state.configurationMessage || "Подождите проверки Codex"),
      );
      return;
    }
    this.stopSpeech();
    this.speechWanted = true;
    this.set({
      error: null,
      interim: "Разрешите распознавание речи в браузере…",
    });
    const gen = this.generation;
    this.speech = this.speechFactory({
      listening: (on) => {
        if (this.closed || gen !== this.generation) return;
        this.set({
          microphone: on,
          connected: on || this.busy,
          status: this.busy
            ? this.state.status
            : on
              ? "listening"
              : "disconnected",
        });
        if (on) {
          this.set({
            interim: "Говорите — распознанная команда появится здесь.",
          });
          this.armSilence();
        }
      },
      interim: (text) => {
        if (this.closed || gen !== this.generation) return;
        clearTimeout(this.silence);
        this.set({ interim: text });
        if (!text) this.armSilence();
      },
      draft: (text) => {
        if (!this.closed && gen === this.generation)
          this.set({ voiceDraft: text });
      },
      final: (text) => {
        if (this.closed || gen !== this.generation) return;
        clearTimeout(this.silence);
        this.set({ interim: text });
        if (this.busy) {
          if (this.state.pending) {
            this.set({ error: "Сначала подтвердите действие кнопкой." });
            return;
          }
          if (this.queuedVoice.length >= 3) {
            this.set({
              error:
                "Очередь голосовых команд заполнена. Дождитесь ответа Codex.",
            });
            return;
          }
          this.queuedVoice.push(text);
          this.set({ interim: `Следующая команда после ответа: ${text}` });
        } else void this.textCommand(text);
      },
      failure: (message) => {
        if (this.closed || gen !== this.generation) return;
        this.stopSpeech();
        this.fail(Error(message));
      },
    });
    try {
      this.speech.start(
        this.state.settings.language,
        this.state.settings.speech_pause_ms,
      );
    } catch (e) {
      this.stopSpeech();
      this.fail(e);
    }
  }
  private stopSpeech(submit = false) {
    this.speechWanted = false;
    this.queuedVoice = [];
    clearTimeout(this.silence);
    this.speech?.stop(submit);
    this.speech = null;
    this.set({ microphone: false, interim: "" });
  }
  submitVoice() {
    this.speech?.flush?.();
  }
  interrupt() {
    if (!this.busy) return;
    void this.api(this.base() + "/cancel", "POST", {}).catch((e) =>
      this.fail(e),
    );
  }
  decide(approve: boolean) {
    const pending = this.state.pending;
    if (pending)
      void this.api(this.base() + "/decision", "POST", {
        call_id: pending.call_id,
        approve,
      })
        .then(() => this.set({ pending: null }))
        .catch((e) => this.fail(e));
  }
  async settings(next: AssistantSettings) {
    try {
      await this.api(
        `/api/assistant/workspaces/${encodeURIComponent(this.state.workspaceId)}/settings`,
        "PUT",
        next,
      );
      const restart =
        this.speechWanted &&
        (next.language !== this.state.settings.language ||
          next.speech_pause_ms !== this.state.settings.speech_pause_ms);
      this.set({ settings: normalizeSettings(next) });
      if (restart) await this.setMicrophone(true);
      return true;
    } catch (e) {
      this.fail(e);
      return false;
    }
  }
  private async updateDesktop() {
    const gen = this.generation;
    for (let i = 0; i < 9; i++) {
      await new Promise((r) => setTimeout(r, 500));
      if (this.closed || gen !== this.generation) return;
      const desktop = await this.api<DesktopStatus>("/api/desktop/status");
      if (this.closed || gen !== this.generation) return;
      this.set({ desktop, notificationError: desktop.last_error || null });
      if (desktop.last_status !== "queued") break;
    }
  }
  async testNotification() {
    try {
      await this.api("/api/desktop/notifications/test", "POST", {});
      await this.updateDesktop();
    } catch (e) {
      this.set({ notificationError: this.message(e) });
    }
  }
  async disconnect() {
    const running = this.busy;
    const base = this.base();
    ++this.generation;
    clearTimeout(this.polling);
    this.abort?.abort();
    this.abort = null;
    this.stopSpeech();
    this.busy = false;
    this.requestId = "";
    this.set({
      connected: false,
      status: "disconnected",
      pending: null,
      activity: "",
    });
    if (running)
      try {
        await this.api(base + "/cancel", "POST", {});
      } catch {
        /* Cancellation failed; the turn can continue until completion or server shutdown. */
      }
  }
  dispose() {
    this.closed = true;
    ++this.loadGeneration;
    void this.disconnect();
  }
}
