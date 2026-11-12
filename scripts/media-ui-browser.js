// UI integration on the production build. HTTP/WebSocket are explicitly mocked;
// real multipart encoding, browser file selection, images and media decoding run.
async (page) => {
  page = await page.context().newPage();
  const root = "__WORKSPACE_ROOT__";
  const state = {
    id: "default",
    name: "Студия материалов",
    version: 1,
    tabs: [{ id: "main", title: "Материалы" }],
    active_tab: "main",
    layout: "grid",
    theme: "light",
    widgets: [],
  };
  const settings = {
    mode: "notifications",
    notifications: true,
    language: "ru",
    save_history: true,
    speech_pause_ms: 4000,
  };
  let sockets = [],
    count = 0;
  const assets = new Map(),
    uploads = [],
    errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const broadcast = () =>
    sockets.forEach((ws) =>
      ws.send(
        JSON.stringify({
          type: "widget.updated",
          workspace_id: "default",
          version: state.version,
          payload: state,
        }),
      ),
    );
  function create(type, title, config) {
    const widget = {
      id: "widget-" + ++count,
      type,
      title,
      tab_id: "main",
      position: state.widgets.length,
      config,
    };
    state.widgets.push(widget);
    state.version++;
    broadcast();
    return widget;
  }
  await page.routeWebSocket("ws://localhost:18882/ws*", (ws) => {
    sockets.push(ws);
    ws.send(
      JSON.stringify({
        type: "state.snapshot",
        workspace_id: "default",
        version: state.version,
        payload: state,
      }),
    );
    ws.onClose(() => (sockets = sockets.filter((s) => s !== ws)));
  });
  await page.route("http://localhost:18882/**", async (route) => {
    const r = route.request(),
      path = new URL(r.url()).pathname;
    if (path === "/api/session") return route.fulfill({ json: { ok: true } });
    if (path === "/api/workspaces")
      return route.fulfill({
        json: {
          workspaces: [
            {
              id: "default",
              name: state.name,
              version: state.version,
              widget_count: state.widgets.length,
            },
          ],
        },
      });
    if (path === "/api/assistant/status")
      return route.fulfill({
        json: {
          provider: "codex",
          configured: false,
          message: "Codex подключается отдельно",
        },
      });
    if (path === "/api/assistant/workspaces/default")
      return route.fulfill({ json: { settings, messages: [] } });
    if (path.endsWith("/state"))
      return route.fulfill({
        json: {
          request_id: "",
          status: "idle",
          text: "",
          activity: "",
          pending: null,
        },
      });
    if (path === "/api/desktop/status")
      return route.fulfill({
        json: {
          supported: true,
          last_status: "idle",
          engine: "mock",
          note: "",
        },
      });
    if (path.endsWith("/settings")) {
      Object.assign(settings, r.postDataJSON());
      return route.fulfill({ json: settings });
    }
    if (path === "/api/media/default" && r.method() === "POST") {
      if (
        !r.headers()["content-type"].startsWith("multipart/form-data;") ||
        r.headers()["x-workspace-request"] !== "1"
      )
        throw Error("Incorrect multipart upload");
      const body = r.postData(),
        filename = /filename="([^"]+)"/.exec(body)?.[1];
      const attach = !/name="attach"\r\n\r\nfalse/.test(body);
      uploads.push({ filename, attach });
      if (filename?.endsWith(".html"))
        return route.fulfill({
          status: 400,
          json: { error: "Формат не поддерживается" },
        });
      const id = (assets.size + 1).toString(16).padStart(64, "0"),
        ext = filename.split(".").pop(),
        mime = {
          png: "image/png",
          wav: "audio/wav",
          mp4: "video/mp4",
          pdf: "application/pdf",
        }[ext];
      const url = "/api/media/default/" + id;
      assets.set(url, { file: filename, mime });
      const asset = { id, name: filename, mime, size: 1000, url };
      let widget;
      if (attach) {
        const type = mime.startsWith("image/")
          ? "image"
          : mime.startsWith("video/")
            ? "video"
            : mime.startsWith("audio/")
              ? "audio"
              : "file";
        widget = create(
          type,
          filename,
          type === "image"
            ? { url, alt: filename }
            : type === "file"
              ? { url, filename }
              : { url },
        );
      }
      return route.fulfill({
        status: 201,
        json: { asset, widget, state: attach ? state : undefined },
      });
    }
    if (assets.has(path)) {
      const a = assets.get(path);
      return route.fulfill({
        path: root + "/e2e/fixtures/" + a.file,
        contentType: a.mime,
      });
    }
    if (path === "/api/commands") {
      const { tool, arguments: a } = r.postDataJSON();
      let widget;
      if (tool === "widget_create") widget = create(a.type, a.title, a.config);
      else if (tool === "widget_update") {
        widget = state.widgets.find((w) => w.id === a.widget_id);
        widget.config = a.config;
        state.version++;
        broadcast();
      } else if (tool === "ui_set_layout") {
        state.theme = a.theme || state.theme;
        state.layout = a.layout || state.layout;
        state.version++;
        broadcast();
      } else throw Error("Unexpected command " + tool);
      return route.fulfill({ json: { state, widget } });
    }
    try {
      const file = path === "/" ? "index.html" : path.slice(1);
      if (file.includes("..")) throw Error();
      return await route.fulfill({
        path: root + "/web/dist/" + file,
        contentType:
          { html: "text/html", js: "text/javascript", css: "text/css" }[
            file.split(".").pop()
          ] || "application/octet-stream",
      });
    } catch {
      return route.fulfill({ status: 404, body: "Not found" });
    }
  });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto("http://localhost:18882/");
  await page
    .getByRole("button", { name: "Добавить виджет", exact: true })
    .click();
  await page
    .getByLabel("Выбрать локальные файлы")
    .setInputFiles(root + "/e2e/fixtures/photo.png");
  await page.getByText("Добавлено файлов: 1", { exact: true }).waitFor();
  await page.getByLabel("Собрать фотографии в галерею").check();
  await page
    .getByLabel("Выбрать локальные файлы")
    .setInputFiles([
      root + "/e2e/fixtures/photo.png",
      root + "/e2e/fixtures/photo2.png",
    ]);
  await page
    .getByText("Галерея добавлена на панель", { exact: true })
    .waitFor();
  await page.getByLabel("Собрать фотографии в галерею").uncheck();
  for (const name of ["video.mp4", "audio.wav", "document.pdf"]) {
    const before = uploads.length,
      expectedWidgets = state.widgets.length + 1;
    await page
      .getByLabel("Выбрать локальные файлы")
      .setInputFiles(root + "/e2e/fixtures/" + name);
    await page.waitForFunction(
      (count) => document.querySelectorAll("[data-widget-id]").length === count,
      expectedWidgets,
    );
    await page.waitForTimeout(150);
    if (uploads.length !== before + 1) throw Error("Upload missing");
  }
  await page.keyboard.press("Escape");
  await page.locator(".widget-gallery img").first().waitFor();
  await page.waitForFunction(() =>
    [...document.querySelectorAll(".image-widget img")].every(
      (img) => img.naturalWidth > 0,
    ),
  );
  if ((await page.locator(".widget-gallery img").count()) !== 2)
    throw Error("Gallery incomplete");
  await page.locator(".widget-image .image-open").click();
  await page.locator(".image-lightbox img").waitFor();
  await page
    .getByRole("button", { name: "Закрыть изображение", exact: true })
    .click();
  for (const label of ["Чек-лист", "Показатель", "Прогресс"]) {
    await page
      .getByRole("button", { name: "Добавить виджет", exact: true })
      .click();
    await page
      .locator(".widget-library button")
      .filter({ hasText: label })
      .click();
    await page.waitForTimeout(100);
  }
  await page.getByLabel("Сформулировать цель", { exact: true }).click();
  await page.waitForFunction(
    () => document.querySelector(".checklist-widget input")?.checked === true,
  );
  await page.waitForFunction(
    () =>
      document.querySelector("video")?.readyState >= 1 &&
      document.querySelector("audio")?.readyState >= 1,
  );
  const durations = await page.evaluate(async () => {
    const video = document.querySelector("video"),
      audio = document.querySelector("audio");
    await video.play();
    video.pause();
    await audio.play();
    audio.pause();
    return { video: video.duration, audio: audio.duration };
  });
  if (durations.video < 0.5 || durations.audio < 0.5)
    throw Error("Media decoding failed");
  await page.getByRole("button", { name: "Настройки ассистента" }).click();
  await page
    .getByLabel("Пауза перед отправкой", { exact: true })
    .selectOption("8000");
  await page.getByRole("button", { name: "Сохранить настройки" }).click();
  if (settings.speech_pause_ms !== 8000) throw Error("Pause not saved");
  await page.reload();
  await page.getByLabel("Сформулировать цель", { exact: true }).waitFor();
  if (
    !(await page.getByLabel("Сформулировать цель", { exact: true }).isChecked())
  )
    throw Error("Checklist not restored");
  await page.getByRole("button", { name: "Настройки ассистента" }).click();
  if (
    (await page
      .getByLabel("Пауза перед отправкой", { exact: true })
      .inputValue()) !== "8000"
  )
    throw Error("Pause lost");
  await page
    .getByRole("button", { name: "Закрыть настройки ассистента" })
    .click();
  await page.evaluate(() => {
    document.activeElement?.blur();
    scrollTo(0, 0);
  });
  await page.waitForTimeout(200);
  await page.screenshot({
    path: root + "/.cache/media-desktop.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForFunction(
    () => document.querySelector(".sidebar").getBoundingClientRect().right <= 0,
  );
  if (
    await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)
  )
    throw Error("Mobile overflow");
  await page.screenshot({
    path: root + "/.cache/media-mobile.png",
    fullPage: true,
  });
  if (errors.length) throw Error(errors.join("\n"));
  return {
    ok: true,
    scope:
      "Production UI with HTTP/WS fixtures; real file inputs, multipart and native media decoding",
    widgets: state.widgets.map((w) => w.type),
    uploads,
    durations,
    checks: [
      "local image upload",
      "multi-image gallery",
      "image lightbox",
      "video and audio playback",
      "PDF attachment",
      "checklist persistence",
      "metric/progress rendering",
      "pause setting reload",
      "mobile layout",
    ],
  };
}
