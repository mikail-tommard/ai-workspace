import { test, expect, type Page } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
test("media UI: uploads, gallery, playback, new widgets and voice pause settings", async ({
  page,
}) => {
  const source = (
    await readFile(resolve("scripts/media-ui-browser.js"), "utf8")
  ).replaceAll("__WORKSPACE_ROOT__", resolve("."));
  const scenario = new Function(`return (${source});`)() as (
    page: Page,
  ) => Promise<{ ok: boolean }>;
  expect((await scenario(page)).ok).toBe(true);
});
