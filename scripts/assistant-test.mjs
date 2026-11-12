import test, { afterEach } from "node:test";
import assert from "node:assert/strict";
import { AssistantController } from "../web/src/assistant/controller.ts";
import { BrowserSpeech } from "../web/src/assistant/speech.ts";
const controllers = new Set();
afterEach(async () => {
  for (const c of controllers) {
    await c.disconnect();
    controllers.delete(c);
  }
});
const defaults = {
  mode: "notifications",
  notifications: true,
  language: "ru",
  save_history: true,
  speech_pause_ms: 4000,
};
const json = (body, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
const tick = () => new Promise((r) => setTimeout(r, 560));
function setup() {
  const requests = [],
    recognizers = [];
  let count = 0;
  const messages = [];
  let state = {
    request_id: "",
    status: "idle",
    activity: "",
    text: "",
    pending: null,
  };
  const c = new AssistantController({
    uuid: () => `request-${++count}`,
    speech: (h) => {
      const r = {
        handlers: h,
        started: false,
        aborted: false,
        start(language, pauseMs) {
          this.started = true;
          this.language = language;
          this.pauseMs = pauseMs;
          h.listening(true);
        },
        stop() {
          this.aborted = true;
          h.listening(false);
        },
      };
      recognizers.push(r);
      return r;
    },
    fetch: async (path, init = {}) => {
      const body = init.body ? JSON.parse(init.body) : null;
      requests.push({ path, method: init.method, body });
      if (path === "/api/assistant/status")
        return json({ configured: true, provider: "codex", message: "" });
      if (path === "/api/desktop/status")
        return json({
          supported: true,
          last_status: "submitted",
          engine: "mock",
          note: "",
        });
      if (path.startsWith("/api/desktop/notifications"))
        return json({ status: "queued" }, 202);
      if (path.endsWith("/settings")) return json(body);
      if (path === "/api/assistant/workspaces/default")
        return json({ settings: defaults, messages });
      if (path.endsWith("/state")) return json(state);
      if (path.endsWith("/turns")) {
        state = {
          request_id: body.request_id,
          status: "thinking",
          activity: "Codex обрабатывает команду",
          text: "",
          pending: null,
        };
        messages.push({
          id: body.request_id + ":user",
          role: "user",
          text: body.text,
          at: new Date().toISOString(),
        });
        return json(state, 202);
      }
      if (path.endsWith("/decision")) {
        state.pending = null;
        state.status = "thinking";
        return json({ ok: true });
      }
      if (path.endsWith("/cancel")) return json({ ok: true });
      throw Error("Unexpected route " + path);
    },
  });
  controllers.add(c);
  return {
    c,
    requests,
    recognizers,
    messages,
    state: () => state,
    patch: (p) => Object.assign(state, p),
  };
}
test("text command uses local Codex endpoint, never Realtime and no microphone", async () => {
  const h = setup();
  await h.c.load("default");
  await h.c.textCommand("Создай задачу");
  assert.equal(h.recognizers.length, 0);
  assert.equal(h.requests.filter((r) => r.path.endsWith("/turns")).length, 1);
  assert.equal(
    h.requests.some((r) => r.path === "/api/assistant/sessions"),
    false,
  );
  assert.equal(h.c.snapshot().status, "thinking");
  h.patch({ status: "completed", text: "Готово" });
  h.messages.push({
    id: "answer",
    role: "assistant",
    text: "Готово",
    at: new Date().toISOString(),
  });
  await tick();
  assert.equal(h.c.snapshot().messages.at(-1).text, "Готово");
  assert.equal("overlays" in h.c.snapshot(), false);
});
test("microphone is explicit; recognized final text reaches the same Codex route", async () => {
  const h = setup();
  await h.c.load("default");
  assert.equal(h.recognizers.length, 0);
  await h.c.setMicrophone(true);
  assert.equal(h.c.snapshot().microphone, true);
  h.recognizers[0].handlers.interim("Создай");
  assert.equal(h.c.snapshot().interim, "Создай");
  h.recognizers[0].handlers.final("Создай задачу");
  await new Promise((r) => setTimeout(r, 5));
  assert.equal(
    h.requests.find((r) => r.path.endsWith("/turns")).body.text,
    "Создай задачу",
  );
  await h.c.setMicrophone(false);
  assert.equal(h.recognizers[0].aborted, true);
  assert.equal(h.c.snapshot().microphone, false);
});
test("permission or recognition failure is visible, never pretends to listen", async () => {
  const h = setup();
  await h.c.load("default");
  await h.c.setMicrophone(true);
  h.recognizers[0].handlers.failure("Разрешите микрофон");
  assert.equal(h.c.snapshot().microphone, false);
  assert.equal(h.c.snapshot().status, "error");
  assert.match(h.c.snapshot().error, /микрофон/);
});
test("confirmation stops microphone and approval is bound to pending ID", async () => {
  const h = setup();
  await h.c.load("default");
  await h.c.setMicrophone(true);
  await h.c.textCommand("Очисти");
  h.patch({
    status: "confirmation",
    pending: { call_id: "approval", label: "Очистить?" },
  });
  await tick();
  assert.equal(h.c.snapshot().microphone, false);
  assert.equal(h.c.snapshot().pending.call_id, "approval");
  h.c.decide(false);
  await new Promise((r) => setTimeout(r, 5));
  assert.deepEqual(h.requests.find((r) => r.path.endsWith("/decision")).body, {
    call_id: "approval",
    approve: false,
  });
});
test("concurrent commands do not start a second turn and interrupt requests cancellation", async () => {
  const h = setup();
  await h.c.load("default");
  await h.c.textCommand("one");
  await h.c.textCommand("two");
  assert.equal(h.requests.filter((r) => r.path.endsWith("/turns")).length, 1);
  h.c.interrupt();
  await new Promise((r) => setTimeout(r, 5));
  assert.ok(h.requests.some((r) => r.path.endsWith("/cancel")));
});
test("settings persist notifications-only and microphone language restarts safely", async () => {
  const h = setup();
  await h.c.load("default");
  await h.c.setMicrophone(true);
  assert.equal(await h.c.settings({ ...defaults, language: "en" }), true);
  assert.equal(h.recognizers[0].aborted, true);
  assert.equal(h.recognizers.at(-1).language, "en");
  assert.equal(h.c.snapshot().settings.mode, "notifications");
});
test("old server is diagnosed rather than silently using OpenAI", async () => {
  const c = new AssistantController({
    fetch: async (path) =>
      path === "/api/assistant/status"
        ? json({ configured: true })
        : path === "/api/desktop/status"
          ? json({ supported: true })
          : json({ settings: defaults, messages: [] }),
  });
  controllers.add(c);
  await c.load("default");
  await c.setMicrophone(true);
  assert.equal(c.snapshot().configured, false);
  assert.match(c.snapshot().error, /старая версия/);
});
test("Codex failures remain visible, no native success is fabricated", async () => {
  const h = setup();
  await h.c.load("default");
  await h.c.textCommand("create");
  h.patch({ status: "error", error: "Войдите в Codex" });
  await tick();
  assert.match(h.c.snapshot().error, /Войдите/);
  assert.equal(
    h.requests.some((r) => r.path === "/api/desktop/notifications"),
    false,
  );
});

function speechHarness(t) {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const saved = globalThis.SpeechRecognition;
  let recognition;
  class Recognition {
    constructor() {
      recognition = this;
      this.aborts = 0;
    }
    start() {
      this.onstart?.();
    }
    abort() {
      this.aborts++;
      this.onend?.();
    }
  }
  globalThis.SpeechRecognition = Recognition;
  const finals = [],
    interim = [];
  const speech = new BrowserSpeech({
    listening() {},
    interim: (text) => interim.push(text),
    final: (text) => finals.push(text),
    failure: (message) => assert.fail(message),
  });
  t.after(() => {
    speech.stop();
    globalThis.SpeechRecognition = saved;
  });
  speech.start("ru");
  return {
    speech,
    finals,
    interim,
    r: () => recognition,
    result: (text, final = false) =>
      recognition.onresult({
        resultIndex: 0,
        results: [{ isFinal: final, 0: { transcript: text } }],
      }),
  };
}
test("a browser final waits for the full four-second pause", (t) => {
  const h = speechHarness(t);
  h.result("Покажи картинку", true);
  t.mock.timers.tick(3999);
  assert.deepEqual(h.finals, []);
  t.mock.timers.tick(1);
  assert.deepEqual(h.finals, ["Покажи картинку"]);
});
test("active speech is never dispatched just because words stopped updating", (t) => {
  const h = speechHarness(t);
  h.r().onspeechstart();
  h.result("Сначала создай");
  t.mock.timers.tick(10000);
  assert.equal(h.finals.length, 0);
  h.result("Сначала создай задачу");
  h.r().onspeechend();
  t.mock.timers.tick(4000);
  assert.deepEqual(h.finals, ["Сначала создай задачу"]);
});
test("short recognizer sessions are combined rather than cutting the user off", (t) => {
  const h = speechHarness(t);
  h.result("Создай задачу", true);
  h.r().onend();
  t.mock.timers.tick(250);
  h.result("средней сложности", true);
  h.r().onend();
  t.mock.timers.tick(3999);
  assert.equal(h.finals.length, 0);
  t.mock.timers.tick(1);
  assert.deepEqual(h.finals, ["Создай задачу средней сложности"]);
});
test("the configured six-second pause is respected", (t) => {
  const h = speechHarness(t);
  h.speech.start("ru", 6000);
  h.result("Сделай сложнее", true);
  t.mock.timers.tick(4000);
  assert.equal(h.finals.length, 0);
  t.mock.timers.tick(2000);
  assert.deepEqual(h.finals, ["Сделай сложнее"]);
});
test("interim without final/end is still sent after silence", (t) => {
  const h = speechHarness(t);
  h.result("Покажи решение");
  t.mock.timers.tick(4000);
  assert.deepEqual(h.finals, ["Покажи решение"]);
});
test("explicit send submits immediately and late final cannot duplicate it", (t) => {
  const h = speechHarness(t);
  h.result("Готовая фраза");
  h.speech.flush();
  h.result("Готовая фраза", true);
  assert.deepEqual(h.finals, ["Готовая фраза"]);
});
test("manual microphone off submits a draft while disposal cancels it", (t) => {
  const h = speechHarness(t);
  h.result("Отправить");
  h.speech.stop(true);
  assert.deepEqual(h.finals, ["Отправить"]);
  h.speech.start("ru");
  h.result("Отменить");
  h.speech.stop();
  t.mock.timers.tick(10000);
  assert.equal(h.finals.length, 1);
  assert.equal(h.r().onresult, null);
});
test("permission/network errors remain visible and never send a draft", (t) => {
  const h = speechHarness(t);
  let error;
  h.speech.stop();
  const speech = new BrowserSpeech({
    listening() {},
    interim() {},
    final: () => assert.fail("sent after error"),
    failure: (m) => (error = m),
  });
  speech.start("ru");
  h.r().onerror({ error: "network" });
  assert.match(error, /диктовку macOS/);
  speech.stop();
});
test("voice commands received while Codex answers are queued, not discarded", async () => {
  const h = setup();
  await h.c.load("default");
  await h.c.setMicrophone(true);
  h.recognizers[0].handlers.final("Создай задачу");
  await new Promise((r) => setTimeout(r, 5));
  h.recognizers[0].handlers.final("Сделай сложнее");
  assert.equal(h.requests.filter((r) => r.path.endsWith("/turns")).length, 1);
  h.patch({ status: "completed", text: "Создано" });
  await tick();
  const requests = h.requests.filter((r) => r.path.endsWith("/turns"));
  assert.equal(requests.length, 2);
  assert.equal(requests[1].body.text, "Сделай сложнее");
});
test("no-speech restarts do not postpone an already accumulated phrase forever", (t) => {
  const h = speechHarness(t);
  h.result("Законченная фраза", true);
  h.r().onend();
  t.mock.timers.tick(250);
  h.r().onerror({ error: "no-speech" });
  h.r().onend();
  t.mock.timers.tick(3750);
  assert.deepEqual(h.finals, ["Законченная фраза"]);
});
