import { test, expect } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
const url = "http://127.0.0.1:18080";
let server: ChildProcess,
  logs = "",
  data: string,
  client: Client;
async function launch() {
  logs = "";
  server = spawn(
    resolve("./ai-workspace"),
    ["--addr", "127.0.0.1:18080", "--data-dir", data],
    { stdio: ["ignore", "ignore", "pipe"] },
  );
  server.stderr!.on("data", (chunk) => (logs += chunk));
  server.on("error", (e) => (logs += e.message));
  for (let i = 0; i < 80; i++) {
    if (server.exitCode !== null)
      throw new Error(`Server exited before listening: ${logs}`);
    try {
      if ((await fetch(`${url}/healthz`)).ok) return;
    } catch {
      /* Wait for startup, bounded below. */
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`Server startup timeout: ${logs}`);
}
async function shutdown() {
  if (!server || server.exitCode !== null) return;
  await new Promise<void>((resolve) => {
    server.once("exit", () => resolve());
    server.kill("SIGTERM");
  });
}
async function call(name: string, args: Record<string, unknown> = {}) {
  const r = await client.callTool({ name, arguments: args });
  expect(r.isError).toBe(false);
  return r.structuredContent as {
    widget: { id: string; problem: { latex: string } };
  };
}
test.beforeAll(async () => {
  data = await mkdtemp(join(tmpdir(), "forma-e2e-"));
  await launch();
  client = new Client({ name: "playwright-e2e", version: "1" });
  const token = (await readFile(join(data, "mcp-token"), "utf8")).trim();
  await client.connect(
    new StreamableHTTPClientTransport(new URL(`${url}/mcp`), {
      requestInit: { headers: { Authorization: `Bearer ${token}` } },
    }),
  );
});
test.afterAll(async () => {
  await client?.close();
  await shutdown();
});
test("MCP → WebSocket → interactive card → SQLite restart", async ({
  page,
  context,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Моё пространство", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("status")).toContainText("Подключено");
  const navigations: string[] = [];
  page.on("framenavigated", (frame) => {
    if (frame === page.mainFrame()) navigations.push(frame.url());
  });
  const math = await call("math_create_problem", {
    kind: "linear",
    difficulty: "medium",
    title: "Задача из MCP",
  });
  const card = page.locator(`[data-widget-id="${math.widget.id}"]`);
  await expect(card).toBeVisible();
  expect(navigations).toHaveLength(0);
  const m = math.widget.problem.latex.match(/^(\d+)x \+ (\d+) = (-?\d+)$/)!;
  const answer = String((Number(m[3]) - Number(m[2])) / Number(m[1]));
  await card.getByRole("textbox", { name: "Ваш ответ" }).fill(answer);
  await card.getByRole("button", { name: "Проверить" }).click();
  await expect(card.getByRole("status")).toContainText("Верно!");
  await card.getByRole("button", { name: "Подсказка", exact: true }).click();
  await expect(card.locator(".hint")).toBeVisible();
  await card.getByRole("button", { name: "Показать решение" }).click();
  await expect(card.locator(".solution")).toBeVisible();
  for (const kind of ["line", "bar", "pie", "scatter"]) {
    const chart = await call("chart_create", {
      title: `График ${kind}`,
      config: {
        kind,
        points: [
          { x: 0, y: 1 },
          { x: 1, y: 4 },
          { x: 2, y: 2 },
        ],
      },
    });
    await expect(
      page.locator(`[data-widget-id="${chart.widget.id}"] svg[role="img"]`),
    ).toBeVisible();
  }
  const tab2 = await context.newPage();
  await tab2.goto("/");
  await expect(
    tab2.getByRole("heading", { name: "Задача из MCP" }),
  ).toBeVisible();
  await call("text_create", {
    title: "Синхронный документ",
    content: "**Две вкладки**",
  });
  await expect(
    page.getByRole("heading", { name: "Синхронный документ" }),
  ).toBeVisible();
  await expect(
    tab2.getByRole("heading", { name: "Синхронный документ" }),
  ).toBeVisible();
  await call("markdown_render", {
    title: "Безопасный документ",
    content: "<script>window.xss=true</script>\n[unsafe](javascript:alert(1))",
  });
  expect(await page.evaluate(() => Object.hasOwn(window, "xss"))).toBe(false);
  await client.close();
  await shutdown();
  await expect(page.getByRole("status").first()).toContainText(
    "Переподключение",
  );
  await launch();
  await expect(card).toBeVisible();
  await expect(card.getByRole("status")).toContainText("Верно!");
  await expect(page.getByRole("status").first()).toContainText("Подключено", {
    timeout: 20000,
  });
  await expect(
    page.getByRole("heading", { name: "График line", exact: true }),
  ).toBeVisible();
  expect(errors).toEqual([]);
});
test("mobile navigation, theme, tab and table search", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await page.getByRole("button", { name: "Открыть навигацию" }).click();
  await expect(
    page.getByRole("navigation", { name: "Рабочие пространства" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Переключить тему" }).click();
  await page.getByRole("button", { name: "Закрыть навигацию" }).click();
  await page
    .getByRole("button", { name: "Добавить вкладку", exact: true })
    .click();
  await page.getByRole("textbox", { name: "Название" }).fill("Данные");
  await page.getByRole("button", { name: "Создать", exact: true }).click();
  await expect(page.getByRole("tab", { name: /Данные/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await page
    .getByRole("button", { name: "Добавить виджет", exact: true })
    .click();
  await page
    .getByRole("button", { name: /Таблица Поиск и сортировка/ })
    .click();
  await page.getByRole("textbox", { name: "Поиск в таблице" }).fill("гипотезу");
  await expect(page.locator("tbody tr")).toHaveCount(1);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});
