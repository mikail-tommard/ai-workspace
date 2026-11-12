// Browser recognition produces text for local Codex; its vendor may use a network service.
export type SpeechHandlers = {
  listening: (on: boolean) => void;
  interim: (text: string) => void;
  draft?: (text: string) => void;
  final: (text: string) => void;
  failure: (message: string) => void;
};
interface RecognitionResult {
  isFinal: boolean;
  [index: number]: { transcript: string };
}
interface RecognitionEvent {
  resultIndex: number;
  results: { length: number; [index: number]: RecognitionResult };
}
interface Recognition {
  lang: string;
  continuous: boolean;
  interimResults: boolean;
  onstart: (() => void) | null;
  onend: (() => void) | null;
  onspeechstart: (() => void) | null;
  onspeechend: (() => void) | null;
  onerror: ((e: { error: string }) => void) | null;
  onresult: ((e: RecognitionEvent) => void) | null;
  start(): void;
  stop?(): void;
  abort(): void;
}
export interface SpeechInput {
  start(language: string, pauseMs?: number): void;
  flush?(): void;
  stop(submit?: boolean): void;
}
export class BrowserSpeech implements SpeechInput {
  private recognition: Recognition | null = null;
  private restart: ReturnType<typeof setTimeout> | undefined;
  private idle: ReturnType<typeof setTimeout> | undefined;
  private active = false;
  private commitPending: (() => void) | null = null;
  private handlers: SpeechHandlers;
  constructor(handlers: SpeechHandlers) {
    this.handlers = handlers;
  }
  start(language: string, pauseMs = 4000) {
    this.stop();
    const api = globalThis as typeof globalThis & {
      SpeechRecognition?: new () => Recognition;
      webkitSpeechRecognition?: new () => Recognition;
    };
    const Constructor = api.SpeechRecognition || api.webkitSpeechRecognition;
    if (!Constructor)
      throw Error(
        "Этот браузер не поддерживает распознавание речи. Используйте Chrome или Safari. Также можно включить диктовку macOS в поле команды.",
      );
    const pause = Math.max(2000, Math.min(10000, pauseMs));
    const r = new Constructor();
    this.recognition = r;
    this.active = true;
    r.lang = language === "en" ? "en-US" : "ru-RU";
    r.continuous = true;
    r.interimResults = true;
    let segments: string[] = [],
      sessionText = "",
      ignoreSession = false,
      speaking = false;
    const candidate = () =>
      [...segments, sessionText].filter(Boolean).join(" ").trim();
    const show = () => {
      const text = candidate();
      this.handlers.draft?.(text);
      this.handlers.interim(text);
    };
    const commit = () => {
      clearTimeout(this.idle);
      const text = candidate();
      if (!this.active || !text) return;
      ignoreSession = true;
      segments = [];
      sessionText = "";
      this.handlers.draft?.("");
      this.handlers.final(text);
      if (this.active) r.abort();
    };
    const arm = (reset = true) => {
      if (!reset && this.idle !== undefined) return;
      clearTimeout(this.idle);
      this.idle = undefined;
      if (this.active && !speaking && candidate())
        this.idle = setTimeout(() => {
          this.idle = undefined;
          commit();
        }, pause);
    };
    this.commitPending = commit;
    r.onstart = () => {
      sessionText = "";
      ignoreSession = false;
      speaking = false;
      this.handlers.listening(true);
      if (segments.length) show();
    };
    r.onspeechstart = () => {
      speaking = true;
      clearTimeout(this.idle);
      this.idle = undefined;
    };
    r.onspeechend = () => {
      speaking = false;
      arm();
    };
    r.onresult = (e) => {
      if (!this.active || ignoreSession) return;
      const parts: string[] = [];
      for (let i = 0; i < e.results.length; i++) {
        const text = e.results[i][0]?.transcript.trim();
        if (text) parts.push(text);
      }
      const before = candidate();
      sessionText = parts.join(" ");
      show();
      arm(candidate() !== before);
    };
    r.onerror = (e) => {
      if (!this.active || e.error === "aborted") return;
      if (e.error === "no-speech") {
        speaking = false;
        if (!candidate())
          this.handlers.interim(
            "Речь пока не распознана. Проверьте выбранный микрофон.",
          );
        arm(false);
        return;
      }
      this.active = false;
      clearTimeout(this.idle);
      segments = [];
      sessionText = "";
      this.handlers.draft?.("");
      this.handlers.failure(
        e.error === "not-allowed" || e.error === "service-not-allowed"
          ? "Разрешите микрофон и распознавание речи для этого сайта в браузере и настройках macOS."
          : e.error === "audio-capture"
            ? "Микрофон недоступен. Проверьте устройство ввода в macOS."
            : e.error === "network"
              ? "Сервис распознавания браузера недоступен. Проверьте сеть или используйте диктовку macOS в поле команды."
              : "Не удалось распознать речь. Попробуйте снова или введите команду текстом.",
      );
    };
    r.onend = () => {
      if (!this.active) return;
      if (!ignoreSession && sessionText) segments.push(sessionText);
      sessionText = "";
      speaking = false;
      this.handlers.listening(false);
      if (candidate()) {
        show();
        arm(false);
      }
      this.restart = setTimeout(() => {
        if (this.active)
          try {
            r.start();
          } catch {
            this.active = false;
            this.handlers.failure(
              "Распознавание остановлено. Нажмите микрофон ещё раз.",
            );
          }
      }, 250);
    };
    try {
      r.start();
    } catch (e) {
      this.stop();
      throw e;
    }
  }
  flush() {
    this.commitPending?.();
  }
  stop(submit = false) {
    if (submit) this.commitPending?.();
    this.commitPending = null;
    this.active = false;
    clearTimeout(this.restart);
    clearTimeout(this.idle);
    const r = this.recognition;
    this.recognition = null;
    if (r) {
      r.onstart = null;
      r.onresult = null;
      r.onerror = null;
      r.onend = null;
      r.onspeechstart = null;
      r.onspeechend = null;
      r.abort();
    }
    this.handlers.draft?.("");
    this.handlers.listening(false);
  }
}
