// Playwright MCP entry point: production UI with explicitly simulated I/O.
// This is a UI/component test, NOT the full server E2E test.
async (page) => {
  page = await page.context().newPage();
  const root = "__WORKSPACE_ROOT__";
  const state = {
    id: "default",
    name: "Моё пространство",
    version: 1,
    tabs: [{ id: "main", title: "Рабочая область" }],
    active_tab: "main",
    layout: "grid",
    theme: "light",
    widgets: [],
  };
  let sockets = [];
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const broadcast = () =>
    sockets.forEach((ws) =>
      ws.send(
        JSON.stringify({
          type: "workspace.updated",
          workspace_id: "default",
          version: state.version,
          payload: state,
        }),
      ),
    );
  await page.routeWebSocket("ws://forma-ui.test/ws*", (ws) => {
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
  await page.route("http://forma-ui.test/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
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
          configured: false,
          provider:"codex",
          message: "Выполните codex login.",
        },
      });
    if (path === "/api/desktop/status")
      return route.fulfill({
        json: {
          supported: false,
          engine: "mock",
          last_status: "unavailable",
          note: "",
        },
      });
    if(path.endsWith("/state")&&path.startsWith("/api/assistant/codex/"))return route.fulfill({json:{request_id:"",status:"idle",activity:"",text:"",pending:null}});
    if (path === "/api/assistant/workspaces/default")
      return route.fulfill({
        json: {
          settings: {
            mode: "notifications",
            notifications: true,
            language: "ru",
            save_history: true,
          },
          messages: [],
        },
      });
    if (path === "/api/commands") {
      const { tool, arguments: a } = route.request().postDataJSON();
      let result = {};
      if (tool === "math_check_answer") {
        const w = state.widgets.find((w) => w.id === a.widget_id);
        const correct = a.answer === "4";
        const p = w.problem || w.questions[a.question_index];
        p.attempts.push({ answer: a.answer, correct });
        result.correct = correct;
      } else if (tool === "math_show_solution") {
        const w = state.widgets.find((w) => w.id === a.widget_id);
        const p = w.problem || w.questions[a.question_index];
        p.revealed = true;
        p.steps = ["2x = 11 − 3 = 8", "x = 8/2 = 4"];
      } else if (tool === "ui_set_layout") {
        if (a.theme) state.theme = a.theme;
        if (a.layout) state.layout = a.layout;
      } else if (tool === "math_create_problem") {
        state.widgets.push({
          id: "math-fixture",
          type: "math_problem",
          title: "Задача из MCP",
          tab_id: "main",
          position: 0,
          config: {},
          problem: {
            kind: "linear",
            difficulty: "medium",
            prompt: "Найдите x.",
            latex: "2x + 3 = 11",
            hints: ["Вычтите 3, затем разделите на 2."],
            revealed: false,
            attempts: [],
          },
        });
      } else if (tool === "chart_create") {
        state.widgets.push({
          id: "chart-fixture",
          type: "chart",
          title: a.title,
          tab_id: "main",
          position: 1,
          config: a.config,
        });
      } else if (tool === "widget_create") {
        state.widgets.push({
          id: "table-fixture",
          type: a.type,
          title: a.title,
          tab_id: "main",
          position: 2,
          config: a.config,
        });
      } else if (tool === "code_create") {
        state.widgets.push({
          id: "code-fixture",
          type: "code",
          title: a.title,
          tab_id: "main",
          position: 3,
          config: { code: a.code, language: a.language },
        });
      } else if (tool === "markdown_render") {
        state.widgets.push({
          id: "markdown-fixture",
          type: "markdown",
          title: a.title,
          tab_id: "main",
          position: 4,
          config: { content: a.content },
        });
      } else if (tool === "ui_show_notification") {
        state.widgets.push({
          id: "notification-fixture",
          type: "notification",
          title: a.title,
          tab_id: "main",
          position: 5,
          config: { message: a.message, level: a.level || "info" },
        });
      } else if (tool === "math_generate_quiz") {
        const problem = {
          kind: "linear",
          difficulty: "easy",
          prompt: "Найдите x.",
          latex: "2x + 3 = 11",
          hints: ["Вычтите 3."],
          revealed: false,
          attempts: [],
        };
        state.widgets.push({
          id: "quiz-fixture",
          type: "quiz",
          title: a.title,
          tab_id: "main",
          position: 6,
          config: {},
          questions: [structuredClone(problem), structuredClone(problem)],
        });
      }

      state.version++;
      broadcast();
      return route.fulfill({
        json: {
          ...result,
          state,
          version: state.version,
          workspace_id: "default",
        },
      });
    }
    try {
      const rel = path === "/" ? "index.html" : path.slice(1);
      if (rel.includes("..")) throw Error("bad path");
      const ext = rel.split(".").pop();
      return route.fulfill({
        path: `${root}/web/dist/${rel}`,
        contentType:
          {
            js: "text/javascript",
            css: "text/css",
            html: "text/html",
            svg: "image/svg+xml",
          }[ext] || "application/octet-stream",
      });
    } catch {
      return route.fulfill({ status: 404, body: "Not found" });
    }
  });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto("http://forma-ui.test/");
  await page
    .getByRole("heading", { name: "Большие идеи начинаются здесь" })
    .waitFor();
  await page.screenshot({
    path: `${root}/.cache/ui-empty.png`,
    fullPage: true,
  });
  await page
    .getByRole("button", {
      name: "Создать математическую задачу",
      exact: false,
    })
    .click();
  const card = page.locator('[data-widget-id="math-fixture"]');
  await card.waitFor();
  await card.getByRole("textbox", { name: "Ваш ответ" }).fill("4");
  await card.getByRole("button", { name: "Проверить" }).click();
  await card.getByRole("status").filter({ hasText: "Верно!" }).waitFor();
  await card.getByRole("button", { name: "Подсказка", exact: true }).click();
  await card.getByRole("button", { name: "Показать решение" }).click();
  await card.locator(".solution").waitFor();
  await page.evaluate(async () => {
    await fetch("/api/commands", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        tool: "chart_create",
        arguments: {
          title: "Функция y = x²",
          config: {
            kind: "line",
            points: Array.from({ length: 21 }, (_, i) => ({
              x: i - 10,
              y: (i - 10) ** 2,
            })),
            x_label: "x",
            y_label: "y",
          },
        },
      }),
    });
  });
  await page
    .locator('[data-widget-id="chart-fixture"] svg[role="img"]')
    .waitFor();
  await page.evaluate(async () => {
    await fetch("/api/commands", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        tool: "widget_create",
        arguments: {
          type: "table",
          title: "Данные исследования",
          config: {
            columns: ["Этап", "Статус"],
            rows: [
              ["Гипотеза", "Готово"],
              ["Данные", "В работе"],
            ],
          },
        },
      }),
    });
  });
  await page.getByRole("textbox", { name: "Поиск в таблице" }).fill("Гипотеза");
  if ((await page.locator(".table-widget tbody tr").count()) !== 1)
    throw Error("Table search failed");
  await page.getByRole("textbox", { name: "Поиск в таблице" }).fill("");
  await page.evaluate(() => {
    document.activeElement?.blur();
    scrollTo(0, 0);
  });
  await page.screenshot({
    path: `${root}/.cache/ui-desktop.png`,
    fullPage: true,
  });
  await page.getByRole("button", { name: "Переключить тему" }).click();
  await page.waitForFunction(
    () => document.documentElement.dataset.theme === "dark",
  );
  await page.screenshot({ path: `${root}/.cache/ui-dark.png`, fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "Открыть навигацию" }).click();
  await page
    .getByRole("navigation", { name: "Рабочие пространства" })
    .waitFor();
  await page.getByRole("button", { name: "Закрыть навигацию" }).click();
  await page.screenshot({
    path: `${root}/.cache/ui-mobile.png`,
    fullPage: true,
  });

  await page.setViewportSize({ width: 1440, height: 1000 });
  for (const [tool, args] of [
    [
      "code_create",
      {
        title: "Код с подсветкой",
        language: "go",
        code: "package main\nfunc main() { println(42) }",
      },
    ],
    [
      "markdown_render",
      {
        title: "Безопасный Markdown",
        content:
          "# Заголовок\n**Жирный текст**\n<script>window.fixtureXSS=true</script>\n[unsafe](javascript:alert(1))",
      },
    ],
    [
      "ui_show_notification",
      {
        title: "Сообщение AI",
        message: "Проверка уведомления",
        level: "success",
      },
    ],
    ["math_generate_quiz", { title: "Проверка теста" }],
  ])
    await page.evaluate(
      async ({ tool, args }) => {
        await fetch("/api/commands", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ tool, arguments: args }),
        });
      },
      { tool, args },
    );
  await page
    .locator('[data-widget-id="code-fixture"] pre code .syntax-keyword')
    .first()
    .waitFor();
  await page.getByText("Проверка уведомления", { exact: true }).waitFor();
  if (await page.evaluate(() => window.fixtureXSS)) throw Error("Markdown XSS");
  if (
    (await page.locator('[data-widget-id="markdown-fixture"] a').count()) !== 0
  )
    throw Error("Unsafe URL rendered");
  const quiz = page.locator('[data-widget-id="quiz-fixture"]');
  await quiz.getByRole("textbox", { name: "Ваш ответ" }).fill("4");
  await quiz.getByRole("button", { name: "Проверить" }).click();
  await quiz.getByRole("status").filter({ hasText: "Верно!" }).waitFor();
  await quiz.getByRole("button", { name: "Следующий" }).click();
  await quiz.getByText("Вопрос 2 из 2", { exact: true }).waitFor();
  sockets.forEach((ws) =>
    ws.close({ code: 1001, reason: "simulated restart" }),
  );
  await page.waitForFunction(
    () =>
      !document
        .querySelector(".connection")
        ?.textContent.includes("Подключено"),
  );
  await page.waitForFunction(() =>
    document.querySelector(".connection")?.textContent.includes("Подключено"),
  );
  await page.locator('[data-widget-id="math-fixture"]').waitFor();
  await page.setViewportSize({ width: 390, height: 844 });
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > innerWidth,
  );
  if (overflow) throw Error("Mobile horizontal overflow");
  if (errors.length) throw Error(errors.join("\n"));
  return {
    ok: true,
    scope: "production UI with simulated HTTP/WebSocket; not full E2E",
    checks: [
      "empty state",
      "math form",
      "feedback",
      "hint",
      "solution",
      "WebSocket rendering",
      "chart",
      "table search",
      "theme",
      "mobile navigation",
      "no horizontal overflow",
      "no JS errors",
      "code highlighting",
      "safe Markdown",
      "quiz checking and pagination",
      "notification",
      "reconnect snapshot",
    ],
    screenshots: [
      "ui-empty.png",
      "ui-desktop.png",
      "ui-dark.png",
      "ui-mobile.png",
    ],
  };
}
