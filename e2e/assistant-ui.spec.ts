import { test, expect, type Page } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
// A separate, explicitly mocked browser scenario. It does not start the Go
// server and is not counted as a live Codex model / ASR test.
test("assistant UI with mocked local Codex and browser recognition: tools, context, images and cleanup", async ({
  page,
}) => {
  const source = (
    await readFile(resolve("scripts/assistant-ui-browser.js"), "utf8")
  ).replaceAll("__WORKSPACE_ROOT__", resolve("."));
  const scenario = new Function(`return (${source});`)() as (
    page: Page,
  ) => Promise<{ ok: boolean }>;
  const result = await scenario(page);
  expect(result.ok).toBe(true);
});
