// An independent official MCP TypeScript SDK client; no server internals used.
import assert from "node:assert/strict";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
const data = await mkdtemp(join(tmpdir(), "ai-workspace-mcp-"));
const binary = resolve(process.env.WORKSPACE_BINARY || "./ai-workspace");
async function connect() {
  const client = new Client({
    name: "workspace-independent-smoke",
    version: "1.0.0",
  });
  const transport = new StdioClientTransport({
    command: binary,
    args: ["--transport", "stdio", "--dashboard=false", "--data-dir", data],
    stderr: "pipe",
  });
  await client.connect(transport);
  return client;
}
async function tool(client, name, args = {}) {
  const res = await client.callTool({ name, arguments: args });
  assert.equal(res.isError, false, JSON.stringify(res));
  assert.ok(res.structuredContent);
  return res.structuredContent;
}
let client = await connect();
try {
  const { tools } = await client.listTools();
  assert.equal(tools.length, 28);
  assert.equal(new Set(tools.map((t) => t.name)).size, 28);
  for (const t of tools) {
    assert.equal(t.inputSchema.type, "object");
    assert.equal(t.inputSchema.additionalProperties, false);
  }
  const math = await tool(client, "math_create_problem", {
    kind: "linear",
    difficulty: "medium",
    title: "MCP smoke problem",
  });
  const problem = math.widget.problem;
  assert.equal(problem.expected, undefined);
  assert.equal(problem.steps, undefined);
  const coefficients = problem.latex.match(/^(\d+)x \+ (\d+) = (-?\d+)$/);
  assert.ok(coefficients);
  const answer = String(
    (Number(coefficients[3]) - Number(coefficients[2])) /
      Number(coefficients[1]),
  );
  const checked = await tool(client, "math_check_answer", {
    widget_id: math.widget.id,
    answer,
  });
  assert.equal(checked.correct, true);
  const solution = await tool(client, "math_show_solution", {
    widget_id: math.widget.id,
  });
  assert.ok(solution.steps.length >= 2);
  const updated = await tool(client, "widget_update", {
    widget_id: math.widget.id,
    math: { difficulty: "hard" },
  });
  assert.equal(updated.widget.id, math.widget.id);
  assert.equal(updated.widget.problem.difficulty, "hard");
  assert.equal(updated.widget.problem.kind, "linear");
  assert.equal(updated.widget.problem.attempts.length, 0);
  assert.equal(updated.widget.problem.steps, undefined);
  for (const kind of ["line", "bar", "pie", "scatter"])
    await tool(client, "chart_create", {
      title: `Chart ${kind}`,
      config: {
        kind,
        points: [
          { x: 0, y: 1 },
          { x: 1, y: 3 },
        ],
      },
    });
  await tool(client, "widget_create", {
    type: "table",
    title: "Data",
    config: { columns: ["x", "y"], rows: [["1", "2"]] },
  });
  const image = await tool(client, "widget_create", {
    type: "image",
    title: "Image from MCP",
    config: {
      url: "https://images.example.com/dirt.png",
      alt: "Minecraft dirt",
    },
  });
  assert.equal(image.widget.type, "image");
  const unsafe = await client.callTool({
    name: "widget_create",
    arguments: {
      type: "image",
      title: "Unsafe",
      config: { url: "javascript:alert(1)", alt: "Unsafe" },
    },
  });
  assert.equal(unsafe.isError, true);
  const local = await tool(client, "media_import", {
    path: resolve("e2e/fixtures/photo.png"),
    title: "Local photo from MCP",
  });
  assert.equal(local.widget.type, "image");
  assert.ok(local.asset.url.startsWith("/api/media/default/"));
  const invalid = await client.callTool({
    name: "math_create_problem",
    arguments: { difficulty: "invalid" },
  });
  assert.equal(invalid.isError, true);
  const before = await tool(client, "workspace_get_state");
  await client.close();
  client = await connect();
  const after = await tool(client, "workspace_get_state");
  assert.deepEqual(after, before);
  console.log(
    JSON.stringify(
      {
        ok: true,
        client: "official @modelcontextprotocol/sdk",
        tools: tools.length,
        checks: [
          "initialize",
          "tools/list",
          "real tool calls",
          "all chart types",
          "table",
          "local media import and permanent asset URL",
          "HTTPS image widget / unsafe image rejection",
          "answer checking",
          "solution",
          "in-place math update",
          "validation",
          "SQLite restart persistence",
        ],
        data,
      },
      null,
      2,
    ),
  );
} finally {
  await client.close();
}
